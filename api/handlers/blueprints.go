package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
	"gopkg.in/yaml.v3"
)

type BlueprintHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewBlueprintHandler(cfg *config.Config, worker *services.Worker) *BlueprintHandler {
	return &BlueprintHandler{Config: cfg, Worker: worker}
}

func (h *BlueprintHandler) ListBlueprints(w http.ResponseWriter, r *http.Request) {
	bps, err := models.ListBlueprints()
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list blueprints")
		return
	}
	if bps == nil {
		bps = []models.Blueprint{}
	}
	utils.RespondJSON(w, http.StatusOK, bps)
}

func (h *BlueprintHandler) CreateBlueprint(w http.ResponseWriter, r *http.Request) {
	var bp models.Blueprint
	if err := json.NewDecoder(r.Body).Decode(&bp); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bp.Name = strings.TrimSpace(bp.Name)
	bp.RepoURL = strings.TrimSpace(bp.RepoURL)
	bp.FilePath = strings.TrimSpace(bp.FilePath)
	if bp.Name == "" || bp.RepoURL == "" {
		utils.RespondError(w, http.StatusBadRequest, "name and repo_url are required")
		return
	}
	if bp.Branch == "" {
		bp.Branch = "main"
	}
	if bp.FilePath == "" {
		bp.FilePath = "render.yaml"
	}
	if bp.WorkspaceID == "" {
		userID := middleware.GetUserID(r)
		ws, err := models.GetWorkspaceByOwner(userID)
		if err != nil || ws == nil {
			utils.RespondError(w, http.StatusBadRequest, "no workspace found for user")
			return
		}
		bp.WorkspaceID = ws.ID
	}
	if err := models.CreateBlueprint(&bp); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create blueprint: "+err.Error())
		return
	}

	// Auto-sync in background after creation
	bp.LastSyncStatus = "syncing"
	models.UpdateBlueprintSync(bp.ID, "syncing")
	ghToken := h.resolveGitHubToken(bp.WorkspaceID)
	go h.doSync(&bp, ghToken)

	utils.RespondJSON(w, http.StatusCreated, bp)
}

type blueprintDetailResponse struct {
	models.Blueprint
	Resources []models.BlueprintResource `json:"resources"`
}

func (h *BlueprintHandler) GetBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	bp, err := models.GetBlueprint(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	resources, err := models.ListBlueprintResources(id)
	if err != nil {
		resources = []models.BlueprintResource{}
	}
	if resources == nil {
		resources = []models.BlueprintResource{}
	}
	utils.RespondJSON(w, http.StatusOK, blueprintDetailResponse{Blueprint: *bp, Resources: resources})
}

// SyncBlueprint clones the repo, parses render.yaml, and creates/updates services
func (h *BlueprintHandler) SyncBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	bp, err := models.GetBlueprint(id)
	if err != nil || bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	if err := models.UpdateBlueprintSync(id, "syncing"); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to start sync")
		return
	}

	ghToken := h.resolveGitHubToken(bp.WorkspaceID)
	go h.doSync(bp, ghToken)

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}

func (h *BlueprintHandler) DeleteBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := models.DeleteBlueprint(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete blueprint")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RenderYAML represents the render.yaml file format
type RenderYAML struct {
	Services     []RenderService     `yaml:"services"`
	Databases    []RenderDatabase    `yaml:"databases"`
	KeyValues    []RenderKeyValue    `yaml:"keyValues"`
	EnvVarGroups []RenderEnvVarGroup `yaml:"envVarGroups"`
}

type RenderEnvVarGroup struct {
	Name    string         `yaml:"name"`
	EnvVars []RenderEnvVar `yaml:"envVars"`
}

type RenderService struct {
	Name              string             `yaml:"name"`
	Type              string             `yaml:"type"`
	Runtime           string             `yaml:"runtime"`
	Repo              string             `yaml:"repo"`
	Branch            string             `yaml:"branch"`
	BuildCommand      string             `yaml:"buildCommand"`
	StartCommand      string             `yaml:"startCommand"`
	Port              int                `yaml:"port"`
	AutoDeploy        *bool              `yaml:"autoDeploy"`
	Plan              string             `yaml:"plan"`
	EnvVars           []RenderEnvVar     `yaml:"envVars"`
	HealthCheckPath   string             `yaml:"healthCheckPath"`
	PreDeployCmd      string             `yaml:"preDeployCommand"`
	DockerfilePath    string             `yaml:"dockerfilePath"`
	DockerContext     string             `yaml:"dockerContext"`
	DockerCommand     string             `yaml:"dockerCommand"`
	RootDir           string             `yaml:"rootDir"`
	StaticPublishPath string             `yaml:"staticPublishPath"`
	Schedule          string             `yaml:"schedule"`
	NumInstances      int                `yaml:"numInstances"`
	Domains           []string           `yaml:"domains"`
	Disk              *RenderDisk        `yaml:"disk"`
	BuildFilter       *RenderBuildFilter `yaml:"buildFilter"`
	Image             *RenderImage       `yaml:"image"`
}

type RenderDisk struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	SizeGB    int    `yaml:"sizeGB"`
}

type RenderBuildFilter struct {
	Paths        []string `yaml:"paths"`
	IgnoredPaths []string `yaml:"ignoredPaths"`
}

type RenderImage struct {
	URL string `yaml:"url"`
}

type RenderEnvVar struct {
	Key           string              `yaml:"key"`
	Value         string              `yaml:"value"`
	GenerateValue bool                `yaml:"generateValue"`
	FromDatabase  *RenderFromDatabase `yaml:"fromDatabase"`
	FromService   *RenderFromService  `yaml:"fromService"`
	FromGroup     string              `yaml:"fromGroup"`
}

type RenderFromDatabase struct {
	Name     string `yaml:"name"`
	Property string `yaml:"property"`
}

type RenderFromService struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	Property  string `yaml:"property"`
	EnvVarKey string `yaml:"envVarKey"`
}

type RenderDatabase struct {
	Name         string `yaml:"name"`
	Plan         string `yaml:"plan"`
	PGVersion    int    `yaml:"postgresMajorVersion"`
	DatabaseName string `yaml:"databaseName"`
	User         string `yaml:"user"`
}

type RenderKeyValue struct {
	Name            string `yaml:"name"`
	Plan            string `yaml:"plan"`
	MaxmemoryPolicy string `yaml:"maxmemoryPolicy"`
}

// resolveGitHubToken looks up the workspace owner's GitHub access token
func (h *BlueprintHandler) resolveGitHubToken(workspaceID string) string {
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		log.Printf("Blueprint: workspace %s not found: %v", workspaceID, err)
		return ""
	}
	encToken, err := models.GetUserGitHubToken(ws.OwnerID)
	if err != nil || encToken == "" {
		log.Printf("Blueprint: no GitHub token for user %s (workspace %s)", ws.OwnerID, workspaceID)
		return ""
	}
	token, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey)
	if err != nil {
		log.Printf("Blueprint: failed to decrypt GitHub token for user %s: %v", ws.OwnerID, err)
		return ""
	}
	return token
}

// dbConnInfo holds connection info for a provisioned database
type dbConnInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
}

func (h *BlueprintHandler) doSync(bp *models.Blueprint, ghToken string) {
	fail := func(msg string) {
		log.Printf("Blueprint sync failed for %s: %s", bp.Name, msg)
		models.UpdateBlueprintSync(bp.ID, "failed: "+msg)
	}

	// Clone repo to temp dir
	tmpDir := filepath.Join(os.TempDir(), "sr-bp-"+bp.ID[:8])
	defer os.RemoveAll(tmpDir)

	if err := h.Worker.Builder.CloneRepo(bp.RepoURL, bp.Branch, tmpDir, ghToken); err != nil {
		fail("clone failed — check repository URL and branch")
		return
	}

	// Read render.yaml
	yamlPath := filepath.Join(tmpDir, bp.FilePath)
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		fail(bp.FilePath + " not found in repository")
		return
	}

	var spec RenderYAML
	if err := yaml.Unmarshal(data, &spec); err != nil {
		fail("invalid YAML syntax in " + bp.FilePath)
		return
	}

	if len(spec.Services) == 0 && len(spec.Databases) == 0 && len(spec.KeyValues) == 0 {
		fail("no services, databases, or key-value stores defined in " + bp.FilePath)
		return
	}

	// --- Phase 1: Create all databases ---
	dbInfoMap := map[string]*dbConnInfo{}

	for _, ddef := range spec.Databases {
		existing, _ := models.GetBlueprintResourceByName(bp.ID, "database", ddef.Name)
		if existing != nil {
			dbModel, _ := models.GetManagedDatabase(existing.ResourceID)
			if dbModel != nil {
				pw, _ := utils.Decrypt(dbModel.EncryptedPassword, h.Config.Crypto.EncryptionKey)
				internalHost := fmt.Sprintf("sr-db-%s", dbModel.ID[:8])
				dbInfoMap[ddef.Name] = &dbConnInfo{
					Host: internalHost, Port: 5432,
					User: dbModel.Username, Password: pw, DBName: dbModel.DBName,
				}
			}
			continue
		}

		dbName := ddef.DatabaseName
		if dbName == "" {
			dbName = ddef.Name
		}
		dbUser := ddef.User
		if dbUser == "" {
			dbUser = ddef.Name
		}
		db := &models.ManagedDatabase{
			WorkspaceID: bp.WorkspaceID, Name: ddef.Name, Plan: ddef.Plan,
			PGVersion: ddef.PGVersion, Host: "localhost", Port: 5432,
			DBName: dbName, Username: dbUser,
		}
		if db.Plan == "" {
			db.Plan = "starter"
		}
		if db.PGVersion == 0 {
			db.PGVersion = 16
		}

		pw, _ := utils.GenerateRandomString(16)
		encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
		db.EncryptedPassword = encrypted

		if err := models.CreateManagedDatabase(db); err != nil {
			fail("failed to create database " + ddef.Name)
			return
		}
		h.Worker.ProvisionDatabase(db, pw)
		models.CreateBlueprintResource(&models.BlueprintResource{
			BlueprintID: bp.ID, ResourceType: "database",
			ResourceID: db.ID, ResourceName: ddef.Name,
		})
		internalHost := fmt.Sprintf("sr-db-%s", db.ID[:8])
		dbInfoMap[ddef.Name] = &dbConnInfo{
			Host: internalHost, Port: 5432,
			User: db.Username, Password: pw, DBName: db.DBName,
		}
		log.Printf("Blueprint sync: created database %s", ddef.Name)
	}

	// --- Phase 2: Create all key-value stores ---
	for _, kvdef := range spec.KeyValues {
		existing, _ := models.GetBlueprintResourceByName(bp.ID, "keyvalue", kvdef.Name)
		if existing != nil {
			continue
		}

		kv := &models.ManagedKeyValue{
			WorkspaceID: bp.WorkspaceID, Name: kvdef.Name,
			Plan: kvdef.Plan, MaxmemoryPolicy: kvdef.MaxmemoryPolicy,
		}
		if kv.Plan == "" {
			kv.Plan = "starter"
		}
		if kv.MaxmemoryPolicy == "" {
			kv.MaxmemoryPolicy = "allkeys-lru"
		}

		pw, _ := utils.GenerateRandomString(16)
		encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
		kv.EncryptedPassword = encrypted

		if err := models.CreateManagedKeyValue(kv); err != nil {
			fail("failed to create key-value store " + kvdef.Name)
			return
		}
		h.Worker.ProvisionKeyValue(kv, pw)
		models.CreateBlueprintResource(&models.BlueprintResource{
			BlueprintID: bp.ID, ResourceType: "keyvalue",
			ResourceID: kv.ID, ResourceName: kvdef.Name,
		})
		log.Printf("Blueprint sync: created keyvalue %s", kvdef.Name)
	}

	// --- Phase 3: Create all services (no deploys yet) ---
	type pendingDeploy struct {
		svc  *models.Service
		sdef RenderService
	}
	var pending []pendingDeploy
	svcMap := map[string]*models.Service{} // name -> service (for fromService refs)

	for _, sdef := range spec.Services {
		serviceName := strings.TrimSpace(sdef.Name)
		if serviceName == "" {
			fail("service name is required in " + bp.FilePath)
			return
		}

		existing, _ := models.GetBlueprintResourceByName(bp.ID, "service", serviceName)
		if existing != nil {
			// Populate svcMap so fromService refs can resolve to pre-existing blueprint services.
			if svc, _ := models.GetService(existing.ResourceID); svc != nil {
				svcMap[serviceName] = svc
			}
			continue
		}

		dockerCtx := sdef.DockerContext
		if dockerCtx == "" && sdef.RootDir != "" {
			dockerCtx = sdef.RootDir
		}

		// For image-based deploys, set the image URL and runtime to docker
		imageURL := ""
		if sdef.Image != nil && sdef.Image.URL != "" {
			imageURL = sdef.Image.URL
		}

		desiredType := sdef.Type
		if desiredType == "" {
			desiredType = "web"
		}

		// If a service already exists with this name, adopt it instead of creating a duplicate.
		svc, _ := models.GetServiceByWorkspaceAndName(bp.WorkspaceID, serviceName)
		if svc != nil {
			if strings.TrimSpace(svc.RepoURL) != "" && strings.TrimSpace(sdef.Repo) != "" && strings.TrimSpace(svc.RepoURL) != strings.TrimSpace(sdef.Repo) {
				fail("service name already exists with a different repo: " + serviceName)
				return
			}
			if strings.TrimSpace(svc.Type) != "" && strings.TrimSpace(desiredType) != "" && strings.TrimSpace(svc.Type) != strings.TrimSpace(desiredType) {
				fail("service name already exists with a different type: " + serviceName)
				return
			}
		} else {
			autoDeploy := true
			if sdef.AutoDeploy != nil {
				autoDeploy = *sdef.AutoDeploy
			}
			svc = &models.Service{
				WorkspaceID: bp.WorkspaceID, Name: serviceName, Type: desiredType,
				Runtime: sdef.Runtime, RepoURL: sdef.Repo, Branch: sdef.Branch,
				BuildCommand: sdef.BuildCommand, StartCommand: sdef.StartCommand,
				Port: sdef.Port, AutoDeploy: autoDeploy, Plan: sdef.Plan,
				HealthCheckPath: sdef.HealthCheckPath, PreDeployCommand: sdef.PreDeployCmd,
				DockerfilePath: sdef.DockerfilePath, DockerContext: dockerCtx,
				StaticPublishPath: sdef.StaticPublishPath, Schedule: sdef.Schedule,
				ImageURL: imageURL,
			}
			if sdef.DockerCommand != "" {
				svc.StartCommand = sdef.DockerCommand
			}
		}
		if sdef.DockerCommand != "" {
			svc.StartCommand = sdef.DockerCommand
		}
		if svc.Branch == "" {
			svc.Branch = bp.Branch
		}
		if svc.RepoURL == "" {
			svc.RepoURL = bp.RepoURL
		}
		if svc.Port == 0 {
			svc.Port = 10000
		}
		if svc.Plan == "" {
			svc.Plan = "starter"
		}
		if sdef.NumInstances > 0 {
			svc.Instances = sdef.NumInstances
		} else {
			svc.Instances = 1
		}

		created := false
		if strings.TrimSpace(svc.ID) == "" {
			if err := models.CreateService(svc); err != nil {
				fail("failed to create service " + serviceName)
				return
			}
			created = true
		} else {
			// Keep adopted services in sync with the blueprint (only fields supported by UpdateService).
			svc.Branch = sdef.Branch
			if svc.Branch == "" {
				svc.Branch = bp.Branch
			}
			svc.BuildCommand = sdef.BuildCommand
			svc.StartCommand = sdef.StartCommand
			if sdef.DockerCommand != "" {
				svc.StartCommand = sdef.DockerCommand
			}
			svc.DockerfilePath = sdef.DockerfilePath
			svc.DockerContext = dockerCtx
			svc.ImageURL = imageURL
			svc.HealthCheckPath = sdef.HealthCheckPath
			svc.Port = sdef.Port
			if svc.Port == 0 {
				svc.Port = 10000
			}
			if sdef.AutoDeploy != nil {
				svc.AutoDeploy = *sdef.AutoDeploy
			}
			svc.MaxShutdownDelay = 30
			svc.PreDeployCommand = sdef.PreDeployCmd
			svc.StaticPublishPath = sdef.StaticPublishPath
			svc.Schedule = sdef.Schedule
			svc.Plan = sdef.Plan
			if svc.Plan == "" {
				svc.Plan = "starter"
			}
			if sdef.NumInstances > 0 {
				svc.Instances = sdef.NumInstances
			} else {
				svc.Instances = 1
			}

			if err := models.UpdateService(svc); err != nil {
				fail("failed to update service " + serviceName)
				return
			}
		}
		models.CreateBlueprintResource(&models.BlueprintResource{
			BlueprintID: bp.ID, ResourceType: "service",
			ResourceID: svc.ID, ResourceName: serviceName,
		})
		svcMap[serviceName] = svc
		if created {
			log.Printf("Blueprint sync: created service %s", serviceName)
		} else {
			log.Printf("Blueprint sync: adopted existing service %s", serviceName)
		}

		// Create custom domains
		for _, domain := range sdef.Domains {
			domain = strings.ToLower(strings.TrimSpace(domain))
			if domain == "" {
				continue
			}
			models.CreateCustomDomain(&models.CustomDomain{
				ServiceID: svc.ID, Domain: domain,
			})
			log.Printf("Blueprint sync: added domain %s to service %s", domain, serviceName)
		}

		// Create disk if specified
		if sdef.Disk != nil && sdef.Disk.Name != "" {
			sizeGB := sdef.Disk.SizeGB
			if sizeGB == 0 {
				sizeGB = 10
			}
			models.CreateDisk(&models.Disk{
				ServiceID: svc.ID, Name: sdef.Disk.Name,
				MountPath: sdef.Disk.MountPath, SizeGB: sizeGB,
			})
			log.Printf("Blueprint sync: created disk %s for service %s", sdef.Disk.Name, serviceName)
		}

		if created {
			pending = append(pending, pendingDeploy{svc: svc, sdef: sdef})
		}
	}

	// --- Phase 3c: Create env var groups ---
	envGroupMap := map[string]*models.EnvGroup{} // group name -> group
	for _, gdef := range spec.EnvVarGroups {
		existing, _ := models.GetEnvGroupByName(bp.WorkspaceID, gdef.Name)
		if existing != nil {
			envGroupMap[gdef.Name] = existing
			continue
		}
		g := &models.EnvGroup{WorkspaceID: bp.WorkspaceID, Name: gdef.Name}
		if err := models.CreateEnvGroup(g); err != nil {
			fail("failed to create env var group " + gdef.Name)
			return
		}
		// Store group's env vars
		if len(gdef.EnvVars) > 0 {
			var groupVars []models.EnvVar
			for _, ev := range gdef.EnvVars {
				val := ev.Value
				if ev.GenerateValue {
					val, _ = utils.GenerateRandomString(32)
				}
				encrypted, _ := utils.Encrypt(val, h.Config.Crypto.EncryptionKey)
				groupVars = append(groupVars, models.EnvVar{
					OwnerType: "env_group", OwnerID: g.ID,
					Key: ev.Key, EncryptedValue: encrypted,
				})
			}
			models.BulkUpsertEnvVars("env_group", g.ID, groupVars)
		}
		envGroupMap[gdef.Name] = g
		log.Printf("Blueprint sync: created env var group %s", gdef.Name)
	}

	// --- Phase 3d: Resolve env vars for all services (after all services exist) ---
	for _, p := range pending {
		if len(p.sdef.EnvVars) == 0 {
			continue
		}
		var envVars []models.EnvVar
		for _, ev := range p.sdef.EnvVars {
			// fromGroup: link the service to the env group and inject all group vars
			if ev.FromGroup != "" {
				if g, ok := envGroupMap[ev.FromGroup]; ok {
					models.LinkServiceToEnvGroup(p.svc.ID, g.ID)
					groupVars, _ := models.ListEnvVars("env_group", g.ID)
					for _, gv := range groupVars {
						envVars = append(envVars, models.EnvVar{
							OwnerType: "service", OwnerID: p.svc.ID,
							Key: gv.Key, EncryptedValue: gv.EncryptedValue,
						})
					}
				}
				continue
			}
			val := ev.Value
			if ev.GenerateValue {
				val, _ = utils.GenerateRandomString(32)
			}
			if ev.FromDatabase != nil {
				if info, ok := dbInfoMap[ev.FromDatabase.Name]; ok {
					switch ev.FromDatabase.Property {
					case "connectionString":
						val = fmt.Sprintf("postgres://%s:%s@%s:%d/%s", info.User, info.Password, info.Host, info.Port, info.DBName)
					case "host":
						val = info.Host
					case "port":
						val = fmt.Sprintf("%d", info.Port)
					case "user":
						val = info.User
					case "password":
						val = info.Password
					case "database":
						val = info.DBName
					}
				}
			}
			if ev.FromService != nil {
				refName := strings.TrimSpace(ev.FromService.Name)
				if refSvc, ok := svcMap[refName]; ok {
					refHost := fmt.Sprintf("%s.%s", utils.ServiceHostLabel(refSvc.Name, refSvc.Subdomain), h.Config.Deploy.Domain)
					switch ev.FromService.Property {
					case "host":
						val = refHost
					case "port":
						val = fmt.Sprintf("%d", refSvc.Port)
					case "hostport":
						val = fmt.Sprintf("%s:%d", refHost, refSvc.Port)
					case "connectionString":
						if refSvc.Type == "keyvalue" || strings.Contains(refSvc.Name, "redis") {
							val = fmt.Sprintf("redis://%s:%d", refHost, refSvc.Port)
						} else {
							val = fmt.Sprintf("http://%s:%d", refHost, refSvc.Port)
						}
					}
					if ev.FromService.EnvVarKey != "" {
						refEnvVars, _ := models.ListEnvVars("service", refSvc.ID)
						for _, rev := range refEnvVars {
							if rev.Key == ev.FromService.EnvVarKey {
								decrypted, _ := utils.Decrypt(rev.EncryptedValue, h.Config.Crypto.EncryptionKey)
								val = decrypted
								break
							}
						}
					}
				}
			}
			encrypted, _ := utils.Encrypt(val, h.Config.Crypto.EncryptionKey)
			envVars = append(envVars, models.EnvVar{
				OwnerType: "service", OwnerID: p.svc.ID,
				Key: ev.Key, EncryptedValue: encrypted,
			})
		}
		models.BulkUpsertEnvVars("service", p.svc.ID, envVars)
	}

	// --- Phase 4: All resources created — now trigger all deploys ---
	for _, p := range pending {
		deploy := &models.Deploy{
			ServiceID: p.svc.ID, Trigger: "blueprint", Branch: p.svc.Branch,
		}
		if err := models.CreateDeploy(deploy); err == nil {
			h.Worker.Enqueue(services.DeployJob{Deploy: deploy, Service: p.svc, GitHubToken: ghToken})
		}
	}

	models.UpdateBlueprintSync(bp.ID, "synced")
	log.Printf("Blueprint sync completed for %s", bp.Name)
}
