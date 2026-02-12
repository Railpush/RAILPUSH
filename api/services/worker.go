package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type DeployJob struct {
	Deploy      *models.Deploy
	Service     *models.Service
	GitHubToken string
}

type Worker struct {
	Config     *config.Config
	Builder    *Builder
	Deployer   *Deployer
	Router     *Router
	Logger     *Logger
	Jobs       chan DeployJob
	OnBuildLog func(deployID string, line string) // callback for WebSocket broadcasting
	wg         sync.WaitGroup
}

func NewWorker(cfg *config.Config) *Worker {
	return &Worker{
		Config:   cfg,
		Builder:  NewBuilder(cfg),
		Deployer: NewDeployer(cfg),
		Router:   NewRouter(cfg),
		Logger:   NewLogger(cfg),
		Jobs:     make(chan DeployJob, 100),
	}
}

func (w *Worker) Start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		w.wg.Add(1)
		go w.run(i)
	}
	log.Printf("Deploy worker started with %d workers", numWorkers)
}

func (w *Worker) Stop() {
	close(w.Jobs)
	w.wg.Wait()
	log.Println("Deploy worker stopped")
}

func (w *Worker) Enqueue(job DeployJob) {
	w.Jobs <- job
}

func (w *Worker) run(id int) {
	defer w.wg.Done()
	for job := range w.Jobs {
		log.Printf("[worker-%d] Processing deploy %s for service %s", id, job.Deploy.ID, job.Service.Name)
		w.processJob(job)
	}
}

func (w *Worker) processJob(job DeployJob) {
	deploy := job.Deploy
	svc := job.Service

	appendLog := func(line string) {
		log.Printf("[deploy:%s] %s", deploy.ID[:8], line)
		models.UpdateDeployBuildLog(deploy.ID, line)
		if w.OnBuildLog != nil {
			w.OnBuildLog(deploy.ID, line)
		}
	}

	// 1. Mark as building
	models.UpdateDeployStatus(deploy.ID, "building")
	models.UpdateServiceStatus(svc.ID, "building", svc.ContainerID, svc.HostPort)
	appendLog("==> Starting build...")

	// Check if this is a rollback with an existing image (skip build)
	imageTag := ""
	skipBuild := false
	if deploy.ImageTag != "" && deploy.Trigger == "rollback" {
		if err := w.Deployer.ExecCommandNoOutput("docker", "image", "inspect", deploy.ImageTag); err == nil {
			appendLog(fmt.Sprintf("==> Rollback: image %s already exists, skipping build", deploy.ImageTag))
			imageTag = deploy.ImageTag
			skipBuild = true
		}
	}

	buildDir := filepath.Join(w.Config.Docker.BuildPath, deploy.ID)
	defer os.RemoveAll(buildDir)

	// Check if this is an image-based deploy (no build needed)
	if !skipBuild && svc.ImageURL != "" {
		appendLog(fmt.Sprintf("==> Using prebuilt image: %s", svc.ImageURL))
		appendLog("==> Pulling image...")
		pullOut, err := w.Deployer.ExecCommand("docker", "pull", svc.ImageURL)
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Failed to pull image: %v - %s", err, pullOut))
			w.failDeploy(deploy, svc)
			return
		}
		imageTag = svc.ImageURL
		skipBuild = true
		appendLog("==> Image pulled successfully")
	}

	if !skipBuild {
		// 2. Clone repo
		if svc.RepoURL == "" {
			appendLog("ERROR: No repository URL configured")
			w.failDeploy(deploy, svc)
			return
		}

		appendLog(fmt.Sprintf("==> Cloning %s (branch: %s)...", svc.RepoURL, svc.Branch))
		if err := w.Builder.CloneRepo(svc.RepoURL, svc.Branch, buildDir, job.GitHubToken); err != nil {
			appendLog(fmt.Sprintf("ERROR: Clone failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Clone complete")

		// Determine effective source directory (rootDir / DockerContext)
		effectiveDir := buildDir
		if svc.DockerContext != "" {
			effectiveDir = filepath.Join(buildDir, svc.DockerContext)
		}

		// 3. Detect runtime
		runtime := svc.Runtime
		if runtime == "" {
			runtime = w.Builder.DetectRuntime(effectiveDir)
			appendLog(fmt.Sprintf("==> Detected runtime: %s", runtime))
		}
		// For static sites, override runtime
		if svc.Type == "static" {
			runtime = "static"
			appendLog("==> Service type is static, using static site build")
		}

		// 4. Generate Dockerfile if not present
		dockerfilePath := filepath.Join(effectiveDir, "Dockerfile")
		if svc.DockerfilePath != "" {
			dockerfilePath = filepath.Join(buildDir, svc.DockerfilePath)
		}

		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			appendLog("==> No Dockerfile found, generating one...")
			startCmd := svc.StartCommand
			if svc.Type == "static" && svc.StaticPublishPath != "" {
				startCmd = svc.StaticPublishPath
			}
			content := w.Builder.GenerateDockerfile(runtime, svc.BuildCommand, startCmd, svc.Port)
			if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
				appendLog(fmt.Sprintf("ERROR: Failed to write Dockerfile: %v", err))
				w.failDeploy(deploy, svc)
				return
			}
			appendLog("==> Dockerfile generated")
		} else {
			appendLog("==> Using existing Dockerfile")
		}

		// 5. Build image
		imageTag = fmt.Sprintf("railpush/%s:%s", utils.ServiceDomainLabel(svc.Name), deploy.ID[:8])
		appendLog(fmt.Sprintf("==> Building image %s...", imageTag))

		buildContext := effectiveDir

		buildOutput, err := w.Builder.BuildImageWithLogs(imageTag, buildContext, dockerfilePath)
		if buildOutput != "" {
			for _, line := range strings.Split(buildOutput, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					appendLog("    " + line)
				}
			}
		}
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Build failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Build complete")
	}

	// 6. Update deploy with image tag
	models.UpdateDeployStarted(deploy.ID, imageTag)
	models.UpdateDeployStatus(deploy.ID, "deploying")
	models.UpdateServiceStatus(svc.ID, "deploying", svc.ContainerID, svc.HostPort)
	appendLog("==> Deploying...")

	// 7. Stop old container
	if svc.ContainerID != "" {
		appendLog(fmt.Sprintf("==> Stopping old container %s...", svc.ContainerID))
		w.Deployer.RemoveContainer(svc.ContainerID)
	}
	// Stop and clear any replica containers from previous deploy.
	if prevInstances, err := models.ListServiceInstances(svc.ID); err == nil {
		for _, inst := range prevInstances {
			if inst.ContainerID != "" {
				_ = w.Deployer.RemoveContainer(inst.ContainerID)
			}
		}
	}
	_ = models.DeleteServiceInstancesByService(svc.ID)

	// 8. Run new container with env vars
	envVars, _ := models.ListEnvVars("service", svc.ID)
	cid, port, err := w.Deployer.RunContainerWithEnv(svc, imageTag, envVars, w.Config.Crypto.EncryptionKey)
	if err != nil {
		appendLog(fmt.Sprintf("ERROR: Failed to start container: %v", err))
		w.failDeploy(deploy, svc)
		return
	}
	appendLog(fmt.Sprintf("==> Container started: %s on port %d", cid, port))
	if svc.Instances < 1 {
		svc.Instances = 1
	}
	startedContainerIDs := []string{cid}
	upstreamPorts := []int{port}

	// 8b. Start replica instances if requested.
	publishReplicaPorts := svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv"
	for i := 2; i <= svc.Instances; i++ {
		replicaName := fmt.Sprintf("sr-%s-%02d", svc.ID[:8], i)
		rcid, rport, rerr := w.Deployer.RunContainerWithEnvNamed(
			svc,
			imageTag,
			envVars,
			w.Config.Crypto.EncryptionKey,
			replicaName,
			publishReplicaPorts,
		)
		if rerr != nil {
			appendLog(fmt.Sprintf("WARNING: failed to start replica #%d: %v", i, rerr))
			continue
		}
		_ = models.CreateServiceInstance(&models.ServiceInstance{
			ServiceID:   svc.ID,
			ContainerID: rcid,
			HostPort:    rport,
			Role:        "replica",
			Status:      "live",
		})
		if rport > 0 {
			upstreamPorts = append(upstreamPorts, rport)
		}
		startedContainerIDs = append(startedContainerIDs, rcid)
		appendLog(fmt.Sprintf("==> Replica #%d started: %s on port %d", i, rcid, rport))
	}

	// 9. Health check
	requiresHealthCheck := svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv"
	if requiresHealthCheck {
		appendLog("==> Running health check...")
		healthPath := svc.HealthCheckPath
		if healthPath == "" {
			healthPath = "/"
		}
		if ok := w.Deployer.HealthCheck("localhost", port, healthPath); !ok {
			appendLog("ERROR: Health check failed; rolling back deploy")
			for _, startedCID := range startedContainerIDs {
				if strings.TrimSpace(startedCID) != "" {
					_ = w.Deployer.RemoveContainer(startedCID)
				}
			}
			_ = models.DeleteServiceInstancesByService(svc.ID)
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Health check passed")
	} else {
		appendLog("==> Skipping health check for non-HTTP service type")
	}

	// 10. Update Caddy route if domain is configured
	if (svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv") && w.Config.Deploy.Domain != "" && w.Config.Deploy.Domain != "localhost" {
		domain := fmt.Sprintf("%s.%s", utils.ServiceDomainLabel(svc.Name), w.Config.Deploy.Domain)
		appendLog(fmt.Sprintf("==> Adding route: %s -> ports=%v", domain, upstreamPorts))
		if err := w.Router.AddRouteUpstreams(domain, upstreamPorts); err != nil {
			appendLog(fmt.Sprintf("WARNING: Failed to add Caddy route: %v", err))
		}
	}

	// 10b. Update Caddy routes for any custom domains and flag TLS provisioning state.
	customDomains, err := models.ListCustomDomains(svc.ID)
	if err != nil {
		appendLog(fmt.Sprintf("WARNING: Failed to load custom domains: %v", err))
	} else {
		for _, cd := range customDomains {
			host := strings.ToLower(strings.TrimSpace(cd.Domain))
			if host == "" {
				continue
			}

			appendLog(fmt.Sprintf("==> Adding custom domain route: %s -> ports=%v", host, upstreamPorts))
			if err := w.Router.AddRouteUpstreams(host, upstreamPorts); err != nil {
				appendLog(fmt.Sprintf("WARNING: Failed to add custom domain route for %s: %v", host, err))
				_ = models.SetCustomDomainTLSProvisioned(svc.ID, host, false)
				continue
			}

			if err := models.SetCustomDomainTLSProvisioned(svc.ID, host, true); err != nil {
				appendLog(fmt.Sprintf("WARNING: Route added but failed to mark TLS for %s: %v", host, err))
			}
		}
	}

	// 11. Mark as live
	models.UpdateServiceStatus(svc.ID, "live", cid, port)
	models.UpdateDeployStatus(deploy.ID, "live")
	appendLog(fmt.Sprintf("==> Deploy complete! Service is live on port %d", port))

	// 12. Start log tailing in background
	go w.Logger.TailContainer(cid)
}

func (w *Worker) failDeploy(deploy *models.Deploy, svc *models.Service) {
	models.UpdateDeployStatus(deploy.ID, "failed")
	models.UpdateServiceStatus(svc.ID, "deploy_failed", svc.ContainerID, svc.HostPort)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (w *Worker) ensurePostgresTLSCert(idPrefix string) (string, error) {
	if w == nil || w.Config == nil {
		return "", fmt.Errorf("missing config")
	}
	idPrefix = strings.TrimSpace(idPrefix)
	if idPrefix == "" {
		return "", fmt.Errorf("missing idPrefix")
	}

	baseDir := strings.TrimSpace(w.Config.Deploy.DataDir)
	if baseDir == "" {
		baseDir = "/var/lib/railpush"
	}
	certDir := filepath.Join(baseDir, "db-certs", idPrefix)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return "", err
	}

	keyPath := filepath.Join(certDir, "server.key")
	crtPath := filepath.Join(certDir, "server.crt")
	if fileExists(keyPath) && fileExists(crtPath) {
		return certDir, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "", err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: fmt.Sprintf("sr-db-%s", idPrefix),
		},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{fmt.Sprintf("sr-db-%s", idPrefix)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		keyFile.Close()
		return "", err
	}
	keyFile.Close()

	crtFile, err := os.OpenFile(crtPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	if err := pem.Encode(crtFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		crtFile.Close()
		return "", err
	}
	crtFile.Close()

	// Postgres alpine images run as uid/gid 70.
	_ = os.Chown(keyPath, 70, 70)
	_ = os.Chown(crtPath, 70, 70)
	_ = os.Chmod(keyPath, 0600)
	_ = os.Chmod(crtPath, 0644)

	return certDir, nil
}

// ProvisionDatabase spins up a real PostgreSQL container
func (w *Worker) ProvisionDatabase(db *models.ManagedDatabase, password string) {
	go func() {
		log.Printf("Provisioning database %s...", db.Name)
		models.UpdateManagedDatabaseStatus(db.ID, "creating", "")

		containerName := fmt.Sprintf("sr-db-%s", db.ID[:8])
		port := w.Deployer.findFreePort()
		certDir, certErr := w.ensurePostgresTLSCert(db.ID[:8])
		if certErr != nil {
			log.Printf("WARNING: Failed to create DB TLS certs for %s: %v (continuing without SSL)", db.Name, certErr)
			certDir = ""
		}

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-e", fmt.Sprintf("POSTGRES_DB=%s", db.DBName),
			"-e", fmt.Sprintf("POSTGRES_USER=%s", db.Username),
			"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
			"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
			"-v", fmt.Sprintf("sr-db-%s:/var/lib/postgresql/data", db.ID[:8]),
		}
		if certDir != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/etc/postgres-ssl:ro", certDir))
		}
		args = append(args, fmt.Sprintf("postgres:%d-alpine", db.PGVersion))
		if certDir != "" {
			args = append(args,
				"postgres",
				"-c", "ssl=on",
				"-c", "ssl_cert_file=/etc/postgres-ssl/server.crt",
				"-c", "ssl_key_file=/etc/postgres-ssl/server.key",
			)
		}

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision database %s: %v - %s", db.Name, err, out)
			models.UpdateManagedDatabaseStatus(db.ID, "failed", "")
			return
		}

		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		// Wait for PostgreSQL to be ready
		ready := false
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if err := w.Deployer.ExecCommandNoOutput("docker", "exec", containerName, "pg_isready", "-U", db.Username); err == nil {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Database %s did not become ready in time", db.Name)
			models.UpdateManagedDatabaseStatus(db.ID, "failed", cid)
			return
		}

		models.UpdateManagedDatabaseStatus(db.ID, "available", cid)
		models.UpdateManagedDatabaseConnection(db.ID, port, "localhost")
		log.Printf("Database %s provisioned successfully on port %d", db.Name, port)
	}()
}

// ProvisionDatabaseReplica spins up a read-only PostgreSQL replica container.
// This provides low-latency read endpoints and HA standby primitives.
func (w *Worker) ProvisionDatabaseReplica(primary *models.ManagedDatabase, replica *models.DatabaseReplica, password string) {
	go func() {
		log.Printf("Provisioning database replica %s for primary %s...", replica.Name, primary.Name)
		_ = models.UpdateDatabaseReplicaStatus(replica.ID, "creating", "", "", 0)

		containerName := fmt.Sprintf("sr-db-rep-%s", replica.ID[:8])
		port := w.Deployer.findFreePort()
		certDir, certErr := w.ensurePostgresTLSCert(replica.ID[:8])
		if certErr != nil {
			log.Printf("WARNING: Failed to create DB replica TLS certs for %s: %v (continuing without SSL)", replica.Name, certErr)
			certDir = ""
		}

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-e", fmt.Sprintf("POSTGRES_DB=%s", primary.DBName),
			"-e", fmt.Sprintf("POSTGRES_USER=%s", primary.Username),
			"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
			"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
			"-v", fmt.Sprintf("sr-db-rep-%s:/var/lib/postgresql/data", replica.ID[:8]),
		}
		if certDir != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/etc/postgres-ssl:ro", certDir))
		}
		args = append(args, fmt.Sprintf("postgres:%d-alpine", primary.PGVersion))
		args = append(args, "postgres")
		if certDir != "" {
			args = append(args,
				"-c", "ssl=on",
				"-c", "ssl_cert_file=/etc/postgres-ssl/server.crt",
				"-c", "ssl_key_file=/etc/postgres-ssl/server.key",
			)
		}
		args = append(args, "-c", "default_transaction_read_only=on")

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision database replica %s: %v - %s", replica.Name, err, out)
			_ = models.UpdateDatabaseReplicaStatus(replica.ID, "failed", "", "", 0)
			return
		}
		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		ready := false
		for i := 0; i < 40; i++ {
			time.Sleep(time.Second)
			if err := w.Deployer.ExecCommandNoOutput("docker", "exec", containerName, "pg_isready", "-U", primary.Username); err == nil {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Database replica %s did not become ready in time", replica.Name)
			_ = models.UpdateDatabaseReplicaStatus(replica.ID, "failed", cid, "localhost", port)
			return
		}

		// Best-effort snapshot seed from primary to replica.
		primaryContainerName := fmt.Sprintf("sr-db-%s", primary.ID[:8])
		dumpOut, dumpErr := w.Deployer.ExecCommand(
			"docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", password),
			primaryContainerName,
			"pg_dump", "-U", primary.Username, "-d", primary.DBName, "--clean", "--if-exists",
		)
		if dumpErr == nil && strings.TrimSpace(dumpOut) != "" {
			cmd := exec.Command("docker", "exec", "-i",
				"-e", fmt.Sprintf("PGPASSWORD=%s", password),
				containerName,
				"psql", "-U", primary.Username, "-d", primary.DBName,
			)
			cmd.Stdin = strings.NewReader(dumpOut)
			if seedOut, err := cmd.CombinedOutput(); err != nil {
				log.Printf("Replica seed warning for %s: %v - %s", replica.Name, err, string(seedOut))
			}
		}

		_ = models.UpdateDatabaseReplicaStatus(replica.ID, "available", cid, "localhost", port)
		log.Printf("Database replica %s available on port %d", replica.Name, port)
	}()
}

// ProvisionKeyValue spins up a real Redis container
func (w *Worker) ProvisionKeyValue(kv *models.ManagedKeyValue, password string) {
	go func() {
		log.Printf("Provisioning key-value store %s...", kv.Name)
		models.UpdateManagedKeyValueStatus(kv.ID, "creating", "")

		containerName := fmt.Sprintf("sr-kv-%s", kv.ID[:8])
		port := w.Deployer.findFreePort()

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-p", fmt.Sprintf("127.0.0.1:%d:6379", port),
			"-v", fmt.Sprintf("sr-kv-%s:/data", kv.ID[:8]),
			"redis:7-alpine",
			"redis-server",
			"--requirepass", password,
			"--maxmemory-policy", kv.MaxmemoryPolicy,
		}

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision key-value %s: %v - %s", kv.Name, err, out)
			models.UpdateManagedKeyValueStatus(kv.ID, "failed", "")
			return
		}

		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		// Wait for Redis to be ready
		ready := false
		for i := 0; i < 15; i++ {
			time.Sleep(time.Second)
			checkOut, _ := w.Deployer.ExecCommand("docker", "exec", containerName, "redis-cli", "-a", password, "ping")
			if strings.Contains(checkOut, "PONG") {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Key-value store %s did not become ready in time", kv.Name)
			models.UpdateManagedKeyValueStatus(kv.ID, "failed", cid)
			return
		}

		models.UpdateManagedKeyValueStatus(kv.ID, "available", cid)
		models.UpdateManagedKeyValueConnection(kv.ID, port, "localhost")
		log.Printf("Key-value store %s provisioned successfully on port %d", kv.Name, port)
	}()
}
