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
	type pendingDeploy struct {
		svc  *models.Service
		sdef RenderService
	}
	type pendingDBProvision struct {
		db       *models.ManagedDatabase
		password string
	}
	type pendingKVProvision struct {
		kv       *models.ManagedKeyValue
		password string
	}
	type createdDomain struct {
		serviceID string
		domain    string
	}

	type syncState struct {
		createdServices []*models.Service
		createdDBIDs    []string
		createdKVIDs    []string
		createdEnvGroupIDs []string
		createdDiskIDs  []string
		createdDomains  []createdDomain
		updatedServices []models.Service // snapshots for adopted service rollback
		insertedBRs     []models.BlueprintResource
	}

	if bp == nil {
		return
	}

	st := syncState{}
	var (
		success bool
		failMsg string
	)
	fail := func(msg string) {
		if failMsg != "" {
			return
		}
		failMsg = msg
	}

	rollback := func() {
		// Delete blueprint resource links we inserted during this sync attempt.
		for _, br := range st.insertedBRs {
			_ = models.DeleteBlueprintResource(&br)
		}

		// Revert adopted service updates (best-effort).
		for _, before := range st.updatedServices {
			_ = models.UpdateService(&before)
		}

		// Delete domains/disks we created during this sync attempt (safe even if service is deleted via cascade).
		for _, d := range st.createdDomains {
			_ = models.DeleteCustomDomain(d.serviceID, d.domain)
		}
		for _, diskID := range st.createdDiskIDs {
			_ = models.DeleteDisk(diskID)
		}

		// Initialize kube deployer if available (k8s mode best-effort cleanup).
		var kd *services.KubeDeployer
		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			if h.Worker != nil {
				if k, err := h.Worker.GetKubeDeployer(); err == nil {
					kd = k
				}
			}
			if kd == nil {
				if k, err := services.NewKubeDeployer(h.Config); err == nil {
					kd = k
				}
			}
		}

		// Delete services created by this sync attempt.
		for _, svc := range st.createdServices {
			if kd != nil {
				_ = kd.DeleteServiceResources(svc)
			}
			_ = models.DeleteService(svc.ID)
		}

		// Delete env groups created by this sync attempt.
		for _, id := range st.createdEnvGroupIDs {
			_ = models.DeleteEnvGroup(id)
		}

		// Delete managed key-value stores created by this sync attempt.
		for _, id := range st.createdKVIDs {
			if kd != nil {
				_ = kd.DeleteManagedKeyValueResources(id)
			}
			_ = models.DeleteManagedKeyValue(id)
		}

		// Delete managed databases created by this sync attempt.
		for _, id := range st.createdDBIDs {
			if kd != nil {
				_ = kd.DeleteManagedDatabaseResources(id)
			}
			_ = models.DeleteManagedDatabase(id)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Blueprint sync panicked for %s (%s): %v", bp.Name, bp.ID, r)
			if failMsg == "" {
				failMsg = "internal error"
			}
		}
		if success {
			return
		}
		if failMsg == "" {
			failMsg = "internal error"
		}
		log.Printf("Blueprint sync failed for %s (%s): %s", bp.Name, bp.ID, failMsg)
		rollback()
		_ = models.UpdateBlueprintSync(bp.ID, "failed: "+failMsg)
	}()

	// Clone repo to temp dir
	idPrefix := strings.TrimSpace(bp.ID)
	if len(idPrefix) >= 8 {
		idPrefix = idPrefix[:8]
	}
	if idPrefix == "" {
		idPrefix = "unknown"
	}
	tmpDir := filepath.Join(os.TempDir(), "sr-bp-"+idPrefix)
	defer os.RemoveAll(tmpDir)

	if h == nil || h.Worker == nil || h.Worker.Builder == nil {
		fail("worker not initialized")
		return
	}
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

	// --- Preflight validation (avoid partial creates on obvious conflicts) ---
	dbSeen := map[string]struct{}{}
	for _, ddef := range spec.Databases {
		name := strings.TrimSpace(ddef.Name)
		if name == "" {
			fail("database name is required in " + bp.FilePath)
			return
		}
		k := strings.ToLower(name)
		if _, ok := dbSeen[k]; ok {
			fail("duplicate database name in blueprint: " + name)
			return
		}
		dbSeen[k] = struct{}{}
	}
	kvSeen := map[string]struct{}{}
	for _, kvdef := range spec.KeyValues {
		name := strings.TrimSpace(kvdef.Name)
		if name == "" {
			fail("key-value name is required in " + bp.FilePath)
			return
		}
		k := strings.ToLower(name)
		if _, ok := kvSeen[k]; ok {
			fail("duplicate key-value name in blueprint: " + name)
			return
		}
		kvSeen[k] = struct{}{}
	}
	egSeen := map[string]struct{}{}
	for _, gdef := range spec.EnvVarGroups {
		name := strings.TrimSpace(gdef.Name)
		if name == "" {
			fail("env var group name is required in " + bp.FilePath)
			return
		}
		k := strings.ToLower(name)
		if _, ok := egSeen[k]; ok {
			fail("duplicate env var group name in blueprint: " + name)
			return
		}
		egSeen[k] = struct{}{}
	}

	svcSeen := map[string]struct{}{}
	svcExistingByName := map[string]*models.Service{} // lower(name) -> existing service (if any)
	svcAlreadyLinked := map[string]struct{}{}         // services already linked to this blueprint (will be skipped)
	for _, sdef := range spec.Services {
		name := strings.TrimSpace(sdef.Name)
		if name == "" {
			fail("service name is required in " + bp.FilePath)
			return
		}
		k := strings.ToLower(name)
		if _, ok := svcSeen[k]; ok {
			fail("duplicate service name in blueprint: " + name)
			return
		}
		svcSeen[k] = struct{}{}

		// Mirror existing behavior: if the service is already linked to this blueprint, we skip it.
		if existing, err := models.GetBlueprintResourceByName(bp.ID, "service", name); err != nil {
			fail("database error")
			return
		} else if existing != nil {
			svcAlreadyLinked[k] = struct{}{}
			continue
		}

		desiredType := strings.TrimSpace(sdef.Type)
		if desiredType == "" {
			desiredType = "web"
		}
		svc, err := models.GetServiceByWorkspaceAndName(bp.WorkspaceID, name)
		if err != nil {
			fail("database error")
			return
		}
		if svc != nil {
			if strings.TrimSpace(svc.RepoURL) != "" && strings.TrimSpace(sdef.Repo) != "" && strings.TrimSpace(svc.RepoURL) != strings.TrimSpace(sdef.Repo) {
				fail("service name already exists with a different repo: " + name)
				return
			}
			if strings.TrimSpace(svc.Type) != "" && strings.TrimSpace(desiredType) != "" && strings.TrimSpace(svc.Type) != strings.TrimSpace(desiredType) {
				fail("service name already exists with a different type: " + name)
				return
			}
			// Disk conflicts for adopted services (no updates supported yet; fail early).
			if sdef.Disk != nil && strings.TrimSpace(sdef.Disk.Name) != "" {
				if existingDisk, derr := models.GetDiskByService(svc.ID); derr != nil {
					fail("database error")
					return
				} else if existingDisk != nil {
					if strings.TrimSpace(existingDisk.Name) != strings.TrimSpace(sdef.Disk.Name) ||
						strings.TrimSpace(existingDisk.MountPath) != strings.TrimSpace(sdef.Disk.MountPath) {
						fail("service already has a different disk configured: " + name)
						return
					}
				}
			}
		}
		svcExistingByName[k] = svc
	}

	// Global custom domain conflicts.
	domainSeen := map[string]struct{}{}
	for _, sdef := range spec.Services {
		nameKey := strings.ToLower(strings.TrimSpace(sdef.Name))
		if nameKey == "" {
			continue
		}
		if _, skip := svcAlreadyLinked[nameKey]; skip {
			continue
		}
		for _, domain := range sdef.Domains {
			d := strings.ToLower(strings.TrimSpace(domain))
			if d == "" {
				continue
			}
			if _, ok := domainSeen[d]; ok {
				fail("duplicate custom domain in blueprint: " + d)
				return
			}
			domainSeen[d] = struct{}{}

			existing, derr := models.GetCustomDomainByDomain(d)
			if derr != nil {
				fail("database error")
				return
			}
			if existing != nil {
				svc := svcExistingByName[nameKey]
				if svc == nil || existing.ServiceID != svc.ID {
					fail("custom domain already in use: " + d)
					return
				}
			}
		}
	}

	// --- Phase 1: Create all databases ---
	dbInfoMap := map[string]*dbConnInfo{}
	var dbProvision []pendingDBProvision
	var linkBRs []models.BlueprintResource

	for _, ddef := range spec.Databases {
		existing, err := models.GetBlueprintResourceByName(bp.ID, "database", ddef.Name)
		if err != nil {
			fail("database error")
			return
		}
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
		st.createdDBIDs = append(st.createdDBIDs, db.ID)
		dbProvision = append(dbProvision, pendingDBProvision{db: db, password: pw})
		linkBRs = append(linkBRs, models.BlueprintResource{
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
	var kvProvision []pendingKVProvision
	for _, kvdef := range spec.KeyValues {
		existing, err := models.GetBlueprintResourceByName(bp.ID, "keyvalue", kvdef.Name)
		if err != nil {
			fail("database error")
			return
		}
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
		st.createdKVIDs = append(st.createdKVIDs, kv.ID)
		kvProvision = append(kvProvision, pendingKVProvision{kv: kv, password: pw})
		linkBRs = append(linkBRs, models.BlueprintResource{
			BlueprintID: bp.ID, ResourceType: "keyvalue",
			ResourceID: kv.ID, ResourceName: kvdef.Name,
		})
		log.Printf("Blueprint sync: created keyvalue %s", kvdef.Name)
	}

	// --- Phase 3: Create all services (no deploys yet) ---
	var pending []pendingDeploy
	svcMap := map[string]*models.Service{} // name -> service (for fromService refs)

	for _, sdef := range spec.Services {
		serviceName := strings.TrimSpace(sdef.Name)
		if serviceName == "" {
			fail("service name is required in " + bp.FilePath)
			return
		}

		existing, err := models.GetBlueprintResourceByName(bp.ID, "service", serviceName)
		if err != nil {
			fail("database error")
			return
		}
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

		desiredType := strings.TrimSpace(sdef.Type)
		if desiredType == "" {
			desiredType = "web"
		}

		// If a service already exists with this name, adopt it instead of creating a duplicate.
		svc, _ := models.GetServiceByWorkspaceAndName(bp.WorkspaceID, serviceName)
		if svc == nil {
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
			st.createdServices = append(st.createdServices, svc)
		} else {
			// Keep adopted services in sync with the blueprint (only fields supported by UpdateService).
			before := *svc
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
			st.updatedServices = append(st.updatedServices, before)
		}

		linkBRs = append(linkBRs, models.BlueprintResource{
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
			// Avoid duplicates for the same service.
			existingCD, err := models.GetCustomDomain(svc.ID, domain)
			if err != nil {
				fail("database error")
				return
			}
			if existingCD != nil {
				continue
			}
			if err := models.CreateCustomDomain(&models.CustomDomain{ServiceID: svc.ID, Domain: domain}); err != nil {
				// Provide a more actionable error for global uniqueness violations.
				if taken, _ := models.GetCustomDomainByDomain(domain); taken != nil && taken.ServiceID != svc.ID {
					fail("custom domain already in use: " + domain)
					return
				}
				fail("failed to add domain " + domain + " to service " + serviceName)
				return
			}
			st.createdDomains = append(st.createdDomains, createdDomain{serviceID: svc.ID, domain: domain})
			log.Printf("Blueprint sync: added domain %s to service %s", domain, serviceName)
		}

		// Create disk if specified
		if sdef.Disk != nil && strings.TrimSpace(sdef.Disk.Name) != "" {
			existingDisk, err := models.GetDiskByService(svc.ID)
			if err != nil {
				fail("database error")
				return
			}
			if existingDisk == nil {
				sizeGB := sdef.Disk.SizeGB
				if sizeGB == 0 {
					sizeGB = 10
				}
				d := &models.Disk{
					ServiceID: svc.ID, Name: sdef.Disk.Name,
					MountPath: sdef.Disk.MountPath, SizeGB: sizeGB,
				}
				if err := models.CreateDisk(d); err != nil {
					fail("failed to create disk " + sdef.Disk.Name + " for service " + serviceName)
					return
				}
				st.createdDiskIDs = append(st.createdDiskIDs, d.ID)
				log.Printf("Blueprint sync: created disk %s for service %s", sdef.Disk.Name, serviceName)
			}
		}

		if created {
			pending = append(pending, pendingDeploy{svc: svc, sdef: sdef})
		}
	}

	// --- Phase 3c: Create env var groups ---
	envGroupMap := map[string]*models.EnvGroup{} // group name -> group
	for _, gdef := range spec.EnvVarGroups {
		existing, err := models.GetEnvGroupByName(bp.WorkspaceID, gdef.Name)
		if err != nil {
			fail("database error")
			return
		}
		if existing != nil {
			envGroupMap[gdef.Name] = existing
			continue
		}
		g := &models.EnvGroup{WorkspaceID: bp.WorkspaceID, Name: gdef.Name}
		if err := models.CreateEnvGroup(g); err != nil {
			fail("failed to create env var group " + gdef.Name)
			return
		}
		st.createdEnvGroupIDs = append(st.createdEnvGroupIDs, g.ID)

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
			if err := models.BulkUpsertEnvVars("env_group", g.ID, groupVars); err != nil {
				fail("failed to set env vars for group " + gdef.Name)
				return
			}
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
					if err := models.LinkServiceToEnvGroup(p.svc.ID, g.ID); err != nil {
						fail("failed to link env group " + ev.FromGroup + " to service " + p.svc.Name)
						return
					}
					groupVars, err := models.ListEnvVars("env_group", g.ID)
					if err != nil {
						fail("failed to load env vars for group " + ev.FromGroup)
						return
					}
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
						refEnvVars, err := models.ListEnvVars("service", refSvc.ID)
						if err == nil {
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
			}
			encrypted, _ := utils.Encrypt(val, h.Config.Crypto.EncryptionKey)
			envVars = append(envVars, models.EnvVar{
				OwnerType: "service", OwnerID: p.svc.ID,
				Key: ev.Key, EncryptedValue: encrypted,
			})
		}
		if err := models.BulkUpsertEnvVars("service", p.svc.ID, envVars); err != nil {
			fail("failed to set env vars for service " + p.svc.Name)
			return
		}
	}

	// Link all new/adopted resources to the blueprint at the end so a failed sync doesn't leave partial links.
	for _, br := range linkBRs {
		if err := models.CreateBlueprintResource(&br); err != nil {
			fail("failed to link blueprint resources")
			return
		}
		st.insertedBRs = append(st.insertedBRs, br)
	}

	// --- Phase 4: Create queued deploys (so workers don't pick them up until we flip to pending) ---
	var deployIDs []string
	var jobs []services.DeployJob
	for _, p := range pending {
		deploy := &models.Deploy{
			ServiceID: p.svc.ID, Trigger: "blueprint", Branch: p.svc.Branch,
			Status: "queued",
		}
		if err := models.CreateDeploy(deploy); err != nil {
			fail("failed to create deploy for service " + p.svc.Name)
			return
		}
		deployIDs = append(deployIDs, deploy.ID)
		jobs = append(jobs, services.DeployJob{Deploy: deploy, Service: p.svc, GitHubToken: ghToken})
	}

	// Start provisioning managed DB/KV only after we know we're going to complete the sync.
	for _, p := range dbProvision {
		h.Worker.ProvisionDatabase(p.db, p.password)
	}
	for _, p := range kvProvision {
		h.Worker.ProvisionKeyValue(p.kv, p.password)
	}

	// Flip deploys to pending in one shot; workers poll only pending/building/deploying.
	if err := models.UpdateDeployStatuses(deployIDs, "pending"); err != nil {
		fail("failed to enqueue deploys")
		return
	}

	// Best-effort immediate enqueue for pods with WORKER_ENABLED=true; otherwise worker Deployment picks them up.
	for _, job := range jobs {
		h.Worker.Enqueue(job)
	}

	success = true
	_ = models.UpdateBlueprintSync(bp.ID, "synced")
	log.Printf("Blueprint sync completed for %s", bp.Name)
}
