package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	userID := middleware.GetUserID(r)
	wsID := r.URL.Query().Get("workspace_id")
	if wsID == "" {
		if ws, err := models.GetWorkspaceByOwner(userID); err == nil && ws != nil {
			wsID = ws.ID
		}
	}
	if wsID == "" {
		utils.RespondJSON(w, http.StatusOK, []models.Blueprint{})
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, wsID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	bps, err := models.ListBlueprintsByWorkspace(wsID)
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
	userID := middleware.GetUserID(r)
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
		bp.FilePath = "railpush.yaml"
	}
	if bp.WorkspaceID == "" {
		ws, err := models.GetWorkspaceByOwner(userID)
		if err != nil || ws == nil {
			utils.RespondError(w, http.StatusBadRequest, "no workspace found for user")
			return
		}
		bp.WorkspaceID = ws.ID
	}
	if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
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

type blueprintResourceResponse struct {
	models.BlueprintResource
	Status string `json:"status"`
}

type blueprintDetailResponse struct {
	models.Blueprint
	Resources []blueprintResourceResponse `json:"resources"`
}

func (h *BlueprintHandler) GetBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	bp, err := models.GetBlueprint(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	resources, err := models.ListBlueprintResources(id)
	if err != nil {
		resources = []models.BlueprintResource{}
	}
	if resources == nil {
		resources = []models.BlueprintResource{}
	}
	enriched := make([]blueprintResourceResponse, len(resources))
	for i, r := range resources {
		enriched[i] = blueprintResourceResponse{BlueprintResource: r, Status: blueprintResourceStatus(r)}
	}
	utils.RespondJSON(w, http.StatusOK, blueprintDetailResponse{Blueprint: *bp, Resources: enriched})
}

func (h *BlueprintHandler) UpdateBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	bp, err := models.GetBlueprint(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if rawFolderID, ok := req["folder_id"]; ok {
		switch v := rawFolderID.(type) {
		case nil:
			if err := models.UpdateBlueprintFolderID(id, nil); err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to update folder")
				return
			}
		case string:
			folderID := strings.TrimSpace(v)
			if folderID == "" {
				if err := models.UpdateBlueprintFolderID(id, nil); err != nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to update folder")
					return
				}
			} else {
				if err := models.UpdateBlueprintFolderID(id, &folderID); err != nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to update folder")
					return
				}
			}
		default:
			utils.RespondError(w, http.StatusBadRequest, "folder_id must be a string or null")
			return
		}
	}

	updated, _ := models.GetBlueprint(id)
	if updated == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	utils.RespondJSON(w, http.StatusOK, updated)
}

func blueprintResourceStatus(r models.BlueprintResource) string {
	switch r.ResourceType {
	case "service":
		if svc, err := models.GetService(r.ResourceID); err == nil && svc != nil {
			return svc.Status
		}
	case "database":
		if db, err := models.GetManagedDatabase(r.ResourceID); err == nil && db != nil {
			return db.Status
		}
	case "keyvalue":
		if kv, err := models.GetManagedKeyValue(r.ResourceID); err == nil && kv != nil {
			return kv.Status
		}
	}
	return "unknown"
}

func blueprintIDSuffix(id string) string {
	// Keep it deterministic and short for readable service names.
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 6 {
		id = id[:6]
	}
	return id
}

func applyRenderServiceNameRenames(spec *RenderYAML, renames map[string]string) {
	if spec == nil || len(renames) == 0 {
		return
	}
	for si := range spec.Services {
		for ei := range spec.Services[si].EnvVars {
			fs := spec.Services[si].EnvVars[ei].FromService
			if fs == nil {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(fs.Name))
			if k == "" {
				continue
			}
			if newName, ok := renames[k]; ok {
				spec.Services[si].EnvVars[ei].FromService.Name = newName
			}
		}
	}
}

func rewriteAIGeneratedServiceNamesForWorkspace(bp *models.Blueprint, spec *RenderYAML) error {
	if bp == nil || spec == nil {
		return nil
	}

	// Normalize static services. AI sometimes includes startCommand for static sites, which breaks
	// k8s deployments by overriding the container command.
	for i := range spec.Services {
		if strings.ToLower(strings.TrimSpace(spec.Services[i].Type)) != "static" {
			continue
		}
		spec.Services[i].StartCommand = ""
		if strings.TrimSpace(spec.Services[i].StaticPublishPath) == "" {
			spec.Services[i].StaticPublishPath = "dist"
		}
		if strings.TrimSpace(spec.Services[i].BuildCommand) == "" {
			spec.Services[i].BuildCommand = "npm run build"
		}
	}

	existing, err := models.ListServices(bp.WorkspaceID)
	if err != nil {
		return err
	}
	existingByLower := map[string]models.Service{} // lower(name) -> service
	for _, svc := range existing {
		k := strings.ToLower(strings.TrimSpace(svc.Name))
		if k == "" {
			continue
		}
		existingByLower[k] = svc
	}

	used := map[string]struct{}{} // names already present in the generated spec
	for _, s := range spec.Services {
		k := strings.ToLower(strings.TrimSpace(s.Name))
		if k == "" {
			continue
		}
		used[k] = struct{}{}
	}

	suffix := blueprintIDSuffix(bp.ID)
	if suffix == "" {
		suffix = "bp"
	}

	renames := map[string]string{} // lower(old) -> new
	for i := range spec.Services {
		name := strings.TrimSpace(spec.Services[i].Name)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		existingSvc, ok := existingByLower[lower]
		if !ok {
			continue
		}

		desiredRepo := strings.TrimSpace(spec.Services[i].Repo)
		if desiredRepo == "" {
			desiredRepo = strings.TrimSpace(bp.RepoURL)
		}
		desiredType := strings.TrimSpace(spec.Services[i].Type)

		mismatchRepo := desiredRepo != "" && strings.TrimSpace(existingSvc.RepoURL) != "" && strings.TrimSpace(existingSvc.RepoURL) != desiredRepo
		mismatchType := desiredType != "" && strings.TrimSpace(existingSvc.Type) != "" && strings.TrimSpace(existingSvc.Type) != desiredType
		if !(mismatchRepo || mismatchType) {
			continue
		}

		baseName := strings.Trim(fmt.Sprintf("%s-%s", name, suffix), "-")
		if baseName == "" {
			baseName = fmt.Sprintf("service-%s", suffix)
		}

		candidate := baseName
		for n := 1; n < 1000; n++ {
			candLower := strings.ToLower(strings.TrimSpace(candidate))
			if candLower == "" {
				candidate = fmt.Sprintf("service-%s", suffix)
				candLower = strings.ToLower(candidate)
			}

			// Avoid collisions within the AI spec itself.
			if _, taken := used[candLower]; taken {
				// If the name is unchanged, no rewrite needed.
				if candLower == lower {
					break
				}
				candidate = fmt.Sprintf("%s-%d", baseName, n)
				continue
			}

			// If the candidate exists already, only accept it when it matches the desired repo/type.
			if ex2, ok2 := existingByLower[candLower]; ok2 {
				mismatchRepo2 := desiredRepo != "" && strings.TrimSpace(ex2.RepoURL) != "" && strings.TrimSpace(ex2.RepoURL) != desiredRepo
				mismatchType2 := desiredType != "" && strings.TrimSpace(ex2.Type) != "" && strings.TrimSpace(ex2.Type) != desiredType
				if mismatchRepo2 || mismatchType2 {
					candidate = fmt.Sprintf("%s-%d", baseName, n)
					continue
				}
			}
			break
		}

		if strings.TrimSpace(candidate) == "" || strings.ToLower(candidate) == lower {
			continue
		}

		renames[lower] = candidate
		used[strings.ToLower(candidate)] = struct{}{}
		spec.Services[i].Name = candidate
	}

	if len(renames) > 0 {
		applyRenderServiceNameRenames(spec, renames)
	}
	return nil
}

// SyncBlueprint clones the repo, parses render.yaml, and creates/updates services
func (h *BlueprintHandler) SyncBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	bp, err := models.GetBlueprint(id)
	if err != nil || bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.UpdateBlueprintSyncWithLog(id, "syncing", ""); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to start sync")
		return
	}

	ghToken := h.resolveGitHubToken(bp.WorkspaceID)
	go h.doSync(bp, ghToken)

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}

func (h *BlueprintHandler) DeleteBlueprint(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	bp, err := models.GetBlueprint(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if bp == nil {
		utils.RespondError(w, http.StatusNotFound, "blueprint not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	resources, err := models.ListBlueprintResources(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load blueprint resources")
		return
	}

	// If any linked service is in a protected environment, require admin.
	adminOK := false
	for _, br := range resources {
		if br.ResourceType != "service" || strings.TrimSpace(br.ResourceID) == "" {
			continue
		}
		svc, _ := models.GetService(br.ResourceID)
		if svc == nil || svc.EnvironmentID == nil || strings.TrimSpace(*svc.EnvironmentID) == "" {
			continue
		}
		env, err := models.GetEnvironment(strings.TrimSpace(*svc.EnvironmentID))
		if err != nil || env == nil || !env.IsProtected {
			continue
		}
		if !adminOK {
			if err := services.EnsureWorkspaceAccess(userID, bp.WorkspaceID, models.RoleAdmin); err != nil {
				utils.RespondError(w, http.StatusForbidden, "admin required to delete blueprints in protected environments")
				return
			}
			adminOK = true
		}
	}

	var stripeSvc *services.StripeService
	if h != nil && h.Config != nil {
		if s := services.NewStripeService(h.Config); s != nil && s.Enabled() {
			stripeSvc = s
		}
	}
	var kd *services.KubeDeployer
	if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
		if k, err := h.Worker.GetKubeDeployer(); err == nil {
			kd = k
		}
	}

	deleted := 0
	var deleteErrors []string

	// Delete all services linked to this blueprint.
	for _, br := range resources {
		if br.ResourceType != "service" || strings.TrimSpace(br.ResourceID) == "" {
			continue
		}
		svcID := strings.TrimSpace(br.ResourceID)
		svc, _ := models.GetService(svcID)
		if svc != nil && strings.TrimSpace(svc.WorkspaceID) != "" && svc.WorkspaceID != bp.WorkspaceID {
			// Safety: never delete cross-workspace resources.
			continue
		}

		// Remove from Stripe subscription before deleting.
		if stripeSvc != nil {
			if err := stripeSvc.RemoveSubscriptionItem("service", svcID); err != nil {
				log.Printf("Blueprint delete: failed to remove billing for service %s: %v", svcID, err)
			}
		}

		// Delete runtime resources (Kubernetes/Docker/Caddy).
		if kd != nil && svc != nil {
			_ = kd.DeleteServiceResources(svc)
		} else if h != nil && h.Worker != nil && svc != nil {
			if svc.ContainerID != "" && h.Worker.Deployer != nil {
				h.Worker.Deployer.RemoveContainer(svc.ContainerID)
			}
			if instances, err := models.ListServiceInstances(svcID); err == nil {
				for _, inst := range instances {
					if inst.ContainerID != "" && h.Worker.Deployer != nil {
						_ = h.Worker.Deployer.RemoveContainer(inst.ContainerID)
					}
				}
			}
			_ = models.DeleteServiceInstancesByService(svcID)
			if h.Config != nil && strings.TrimSpace(h.Config.Deploy.Domain) != "" && h.Config.Deploy.Domain != "localhost" && !h.Config.Deploy.DisableRouter && h.Worker.Router != nil {
				domain := utils.ServiceHostLabel(svc.Name, svc.Subdomain) + "." + h.Config.Deploy.Domain
				h.Worker.Router.RemoveRoute(domain)
			}
		}

		if err := models.DeleteService(svcID); err != nil {
			deleteErrors = append(deleteErrors, svcID)
			continue
		}
		_ = models.DeleteBlueprintResource(&br)
		deleted++
		services.Audit(bp.WorkspaceID, userID, "blueprint.service_deleted", "service", svcID, map[string]interface{}{
			"blueprint_id": id,
		})
	}

	if len(deleteErrors) > 0 {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete one or more services linked to this blueprint")
		return
	}

	if err := models.DeleteBlueprint(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete blueprint")
		return
	}
	services.Audit(bp.WorkspaceID, userID, "blueprint.deleted", "blueprint", id, map[string]interface{}{
		"deleted_services": deleted,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "deleted", "deleted_services": deleted})
}

// RenderYAML represents the railpush.yaml (or render.yaml) blueprint file format
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
	BuildContextAlias string             `yaml:"buildContext"`
	DockerCommand     string             `yaml:"dockerCommand"`
	RootDir           string             `yaml:"rootDir"`
	StaticPublishPath string             `yaml:"staticPublishPath"`
	Schedule          string             `yaml:"schedule"`
	NumInstances      int                `yaml:"numInstances"`
	Domains           []string           `yaml:"domains"`
	Disk              *RenderDisk        `yaml:"disk"`
	BuildFilter       *RenderBuildFilter `yaml:"buildFilter"`
	Image             *RenderImage       `yaml:"image"`
	BuildInclude      []string           `yaml:"buildInclude"`
	BuildExclude      []string           `yaml:"buildExclude"`
	BaseImage         string             `yaml:"baseImage"`
	DependsOn         []string           `yaml:"depends_on"`
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
	Name           string `yaml:"name"`
	Plan           string `yaml:"plan"`
	PGVersion      int    `yaml:"postgresMajorVersion"`
	DatabaseName   string `yaml:"databaseName"`
	User           string `yaml:"user"`
	InitScript     string `yaml:"initScript"`
	InitScriptPath string `yaml:"initScriptPath"`
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

func (h *BlueprintHandler) blueprintAIAutogenEnabled(workspaceID string) bool {
	if h == nil || h.Config == nil {
		return false
	}
	if !h.Config.BlueprintAI.Enabled || strings.TrimSpace(h.Config.BlueprintAI.OpenRouterAPIKey) == "" {
		return false
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil || strings.TrimSpace(ws.OwnerID) == "" {
		return false
	}
	owner, err := models.GetUserByID(ws.OwnerID)
	if err != nil || owner == nil {
		return false
	}
	return owner.BlueprintAIAutogenEnabled
}

// dbConnInfo holds connection info for a provisioned database
type dbConnInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
}

// normalizeBlueprintPlan coerces user/AI supplied plans into supported tiers.
// Empty plans default to starter. Unknown values are repaired instead of failing sync.
func normalizeBlueprintPlan(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return services.PlanStarter, false
	}
	if p, ok := services.NormalizePlan(trimmed); ok {
		return p, false
	}

	alias := strings.ToLower(trimmed)
	switch alias {
	case "hobby", "basic", "small":
		return services.PlanStarter, true
	case "medium":
		return services.PlanStandard, true
	case "professional", "business", "enterprise", "team":
		return services.PlanPro, true
	case "trial":
		return services.PlanFree, true
	}

	// Heuristic fallback for generated plans like "starter-1x", "pro-plus", etc.
	switch {
	case strings.Contains(alias, "free"), strings.Contains(alias, "trial"):
		return services.PlanFree, true
	case strings.Contains(alias, "start"), strings.Contains(alias, "hobby"), strings.Contains(alias, "basic"), strings.Contains(alias, "small"):
		return services.PlanStarter, true
	case strings.Contains(alias, "standard"), strings.Contains(alias, "medium"):
		return services.PlanStandard, true
	case strings.Contains(alias, "pro"), strings.Contains(alias, "business"), strings.Contains(alias, "enterprise"), strings.Contains(alias, "team"), strings.Contains(alias, "scale"):
		return services.PlanPro, true
	default:
		return services.PlanStarter, true
	}
}

func (h *BlueprintHandler) doSync(bp *models.Blueprint, ghToken string) {
	type pendingDeploy struct {
		svc     *models.Service
		sdef    RenderService
		created bool // true if newly created, false if adopted/updated
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
		createdServices    []*models.Service
		createdDBIDs       []string
		createdKVIDs       []string
		createdEnvGroupIDs []string
		createdDiskIDs     []string
		createdDomains     []createdDomain
		updatedServices    []models.Service // snapshots for adopted service rollback
		insertedBRs        []models.BlueprintResource
	}

	if bp == nil {
		return
	}

	st := syncState{}
	var syncLog strings.Builder
	logLine := func(msg string) {
		syncLog.WriteString(time.Now().UTC().Format("15:04:05") + "  " + msg + "\n")
	}
	logLine("Starting sync for blueprint " + bp.Name)

	var (
		success bool
		failMsg string
	)
	fail := func(msg string) {
		if failMsg != "" {
			return
		}
		failMsg = msg
		logLine("ERROR: " + msg)
	}
	// warnBilling logs billing errors but does NOT abort the sync.
	// Blueprint sync should never fail because of a billing issue — the platform
	// reconciles billing asynchronously. Resources are created first; billing follows.
	warnBilling := func(err error) {
		if err != nil {
			log.Printf("Blueprint sync billing warning (non-fatal) blueprint=%s err=%v", bp.ID, err)
		}
	}

	// Stripe billing: blueprint sync can create paid resources, so we must bill (or block) here too.
	var stripeSvc *services.StripeService
	if h != nil && h.Config != nil {
		if s := services.NewStripeService(h.Config); s != nil && s.Enabled() {
			stripeSvc = s
		}
	}
	var billingCustomer *models.BillingCustomer
	getBillingCustomer := func() (*models.BillingCustomer, error) {
		if stripeSvc == nil {
			return nil, nil
		}
		if billingCustomer != nil {
			return billingCustomer, nil
		}
		ws, err := models.GetWorkspace(bp.WorkspaceID)
		if err != nil || ws == nil || strings.TrimSpace(ws.OwnerID) == "" {
			return nil, fmt.Errorf("workspace not found")
		}
		owner, err := models.GetUserByID(ws.OwnerID)
		if err != nil || owner == nil || strings.TrimSpace(owner.Email) == "" {
			return nil, fmt.Errorf("workspace owner not found")
		}
		bc, err := stripeSvc.EnsureCustomer(owner.ID, owner.Email)
		if err != nil || bc == nil {
			if err == nil {
				err = fmt.Errorf("billing customer not found")
			}
			return nil, err
		}
		billingCustomer = bc
		return billingCustomer, nil
	}

	rollback := func() {
		// Delete blueprint resource links we inserted during this sync attempt.
		for _, br := range st.insertedBRs {
			_ = models.DeleteBlueprintResource(&br)
		}

		// Best-effort: roll back Stripe billing side-effects so a failed sync doesn't leave charges behind.
		if stripeSvc != nil {
			bc, err := getBillingCustomer()
			if err != nil || bc == nil {
				log.Printf("Blueprint sync rollback: failed to resolve billing customer: %v", err)
			} else {
				// Remove billing for resources created by this sync attempt.
				for _, svc := range st.createdServices {
					if svc == nil || strings.TrimSpace(svc.ID) == "" {
						continue
					}
					if err := stripeSvc.RemoveSubscriptionItem("service", svc.ID); err != nil {
						log.Printf("Blueprint sync rollback: failed to remove billing for service %s: %v", svc.ID, err)
					}
				}
				for _, id := range st.createdDBIDs {
					if strings.TrimSpace(id) == "" {
						continue
					}
					if err := stripeSvc.RemoveSubscriptionItem("database", id); err != nil {
						log.Printf("Blueprint sync rollback: failed to remove billing for database %s: %v", id, err)
					}
				}
				for _, id := range st.createdKVIDs {
					if strings.TrimSpace(id) == "" {
						continue
					}
					if err := stripeSvc.RemoveSubscriptionItem("keyvalue", id); err != nil {
						log.Printf("Blueprint sync rollback: failed to remove billing for keyvalue %s: %v", id, err)
					}
				}

				// Restore billing for adopted services to their original plan (best-effort).
				for _, before := range st.updatedServices {
					if strings.TrimSpace(before.ID) == "" {
						continue
					}
					beforePlan := strings.ToLower(strings.TrimSpace(before.Plan))
					if p, ok := services.NormalizePlan(before.Plan); ok {
						beforePlan = p
					}
					if beforePlan == services.PlanFree {
						if err := stripeSvc.RemoveSubscriptionItem("service", before.ID); err != nil {
							log.Printf("Blueprint sync rollback: failed to remove billing for adopted service %s: %v", before.ID, err)
						}
						continue
					}
					if err := stripeSvc.AddSubscriptionItem(bc, before.WorkspaceID, "service", before.ID, before.Name, beforePlan); err != nil {
						log.Printf("Blueprint sync rollback: failed to restore billing for adopted service %s: %v", before.ID, err)
					}
				}
			}
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
			logLine("PANIC: " + fmt.Sprintf("%v", r))
		}
		if success {
			return
		}
		if failMsg == "" {
			failMsg = "internal error"
		}
		log.Printf("Blueprint sync failed for %s (%s): %s", bp.Name, bp.ID, failMsg)
		logLine("Sync failed. Rolling back created resources...")
		rollback()
		logLine("Rollback complete.")
		_ = models.UpdateBlueprintSyncWithLog(bp.ID, "failed: "+failMsg, syncLog.String())
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
	logLine("Cloning repository " + bp.RepoURL + " (branch: " + bp.Branch + ")...")
	if err := h.Worker.Builder.CloneRepo(bp.RepoURL, bp.Branch, tmpDir, ghToken); err != nil {
		fail("clone failed — check repository URL and branch")
		return
	}
	logLine("Repository cloned successfully.")

	// Read railpush.yaml (preferred) or render.yaml (fallback).
	// If neither exists, use the stored generated YAML if present, otherwise auto-generate via OpenRouter.
	logLine("Looking for " + bp.FilePath + " in repository...")
	yamlPath := filepath.Join(tmpDir, bp.FilePath)
	data, err := os.ReadFile(yamlPath)
	repoFileExists := err == nil
	// Fallback: if the primary file doesn't exist, try the other name.
	if !repoFileExists {
		var fallback string
		if strings.HasSuffix(bp.FilePath, "railpush.yaml") {
			fallback = strings.Replace(bp.FilePath, "railpush.yaml", "render.yaml", 1)
		} else if strings.HasSuffix(bp.FilePath, "render.yaml") {
			fallback = strings.Replace(bp.FilePath, "render.yaml", "railpush.yaml", 1)
		}
		if fallback != "" {
			if fbData, fbErr := os.ReadFile(filepath.Join(tmpDir, fallback)); fbErr == nil {
				data = fbData
				repoFileExists = true
			}
		}
	}
	specGeneratedByAI := false

	if repoFileExists {
		logLine("Found " + bp.FilePath + " in repository.")
	}

	// Prefer the stored generated blueprint when the repo has no yaml.
	if !repoFileExists && len(data) == 0 && strings.TrimSpace(bp.GeneratedYAML) != "" {
		data = []byte(bp.GeneratedYAML)
		specGeneratedByAI = true
	}

	aiEnabled := h.blueprintAIAutogenEnabled(bp.WorkspaceID)
	aiShouldGenerate := false
	// Automatic mode: if the repo doesn't have a blueprint file, try to generate one.
	if !repoFileExists && len(data) == 0 {
		aiShouldGenerate = true
	} else if aiEnabled && bp.AIIgnoreRepoYAML {
		// Opt-in: allow users to ignore repo YAML and regenerate from source.
		aiShouldGenerate = true
	}
	if aiShouldGenerate {
		logLine("No blueprint file found. Attempting to generate via Blueprint AI...")
		ai := services.NewBlueprintAIGenerator(h.Config)
		if !ai.Available() {
			if !repoFileExists && len(data) == 0 {
				fail(bp.FilePath + " not found in repository (and automatic blueprint generation isn't configured)")
				return
			}
		} else {
			generated, genErr := ai.GenerateRenderYAMLFromRepo(tmpDir, bp.RepoURL, bp.Branch)
			if genErr != nil {
				log.Printf("Blueprint sync: OpenRouter generation failed blueprint=%s err=%v", bp.ID, genErr)
				logLine("Blueprint AI error: " + genErr.Error())
				if !repoFileExists && len(data) == 0 {
					fail("failed to generate " + bp.FilePath + " with Blueprint AI: " + genErr.Error())
					return
				}
			} else {
				var candidate RenderYAML
				if parseErr := yaml.Unmarshal([]byte(generated), &candidate); parseErr != nil {
					log.Printf("Blueprint sync: OpenRouter returned invalid YAML blueprint=%s err=%v", bp.ID, parseErr)
					logLine("Blueprint AI returned invalid YAML: " + parseErr.Error())
					if !repoFileExists && len(data) == 0 {
						fail("Blueprint AI generated invalid YAML: " + parseErr.Error())
						return
					}
				} else if len(candidate.Services) == 0 && len(candidate.Databases) == 0 && len(candidate.KeyValues) == 0 && len(candidate.EnvVarGroups) == 0 {
					log.Printf("Blueprint sync: OpenRouter returned empty blueprint=%s", bp.ID)
					logLine("Blueprint AI generated an empty blueprint (no services, databases, or key-value stores).")
					if !repoFileExists && len(data) == 0 {
						fail("Blueprint AI generated an empty blueprint")
						return
					}
				} else {
					data = []byte(generated)
					specGeneratedByAI = true
					if mkErr := os.MkdirAll(filepath.Dir(yamlPath), 0o755); mkErr == nil {
						_ = os.WriteFile(yamlPath, data, 0o644)
					}
					// Persist generated YAML so future syncs don't depend on another AI call.
					if err := models.UpdateBlueprintGeneratedYAML(bp.ID, generated); err != nil {
						log.Printf("Blueprint sync: failed to persist generated yaml blueprint=%s err=%v", bp.ID, err)
					} else {
						bp.GeneratedYAML = generated
					}
					logLine("Blueprint AI generated " + bp.FilePath + " successfully.")
				log.Printf("Blueprint sync: generated %s via OpenRouter for blueprint=%s", bp.FilePath, bp.ID)
				}
			}
		}
	}
	if len(data) == 0 {
		if repoFileExists {
			fail("invalid " + bp.FilePath)
			return
		}
		fail(bp.FilePath + " not found in repository")
		return
	}

	logLine("Parsing YAML...")
	var spec RenderYAML
	if err := yaml.Unmarshal(data, &spec); err != nil {
		fail("invalid YAML syntax in " + bp.FilePath + ": " + err.Error())
		return
	}

	// AI-generated specs can clash with existing workspace service names. When we autogenerate YAML,
	// rewrite conflicting service names deterministically so the sync can proceed without mutating
	// existing services in this workspace.
	if specGeneratedByAI {
		if err := rewriteAIGeneratedServiceNamesForWorkspace(bp, &spec); err != nil {
			log.Printf("Blueprint sync: failed to rewrite AI service names blueprint=%s err=%v", bp.ID, err)
			// Best-effort only. We'll still proceed with the original spec; preflight may fail with a clear message.
		}
	}

	// Resolve initScriptPath: read SQL files from the cloned repo and merge into initScript.
	for i, ddef := range spec.Databases {
		scriptPath := strings.TrimSpace(ddef.InitScriptPath)
		if scriptPath == "" {
			continue
		}
		// Validate path: must be relative, no traversal.
		cleaned := filepath.Clean(scriptPath)
		if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			fail(fmt.Sprintf("database %s: initScriptPath %q is invalid (must be a relative path within the repo)", ddef.Name, scriptPath))
			return
		}
		sqlData, err := os.ReadFile(filepath.Join(tmpDir, cleaned))
		if err != nil {
			fail(fmt.Sprintf("database %s: initScriptPath %q not found in repo", ddef.Name, scriptPath))
			return
		}
		fileSQL := strings.TrimSpace(string(sqlData))
		if fileSQL == "" {
			logLine(fmt.Sprintf("WARNING: database %s: initScriptPath %q is empty, skipping", ddef.Name, scriptPath))
			continue
		}
		// If both initScript (inline) and initScriptPath are set, concatenate them
		// (inline runs first, then file). This lets users do quick one-liners inline
		// and keep the heavy schema in a file.
		if existing := strings.TrimSpace(ddef.InitScript); existing != "" {
			spec.Databases[i].InitScript = existing + "\n" + fileSQL
		} else {
			spec.Databases[i].InitScript = fileSQL
		}
		logLine(fmt.Sprintf("Database %s: loaded %d bytes from %s", ddef.Name, len(fileSQL), scriptPath))
	}

	if len(spec.Services) == 0 && len(spec.Databases) == 0 && len(spec.KeyValues) == 0 {
		fail("no services, databases, or key-value stores defined in " + bp.FilePath)
		return
	}

	logLine(fmt.Sprintf("Found %d service(s), %d database(s), %d key-value store(s), %d env group(s) in blueprint.",
		len(spec.Services), len(spec.Databases), len(spec.KeyValues), len(spec.EnvVarGroups)))
	logLine("Running preflight validation...")

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
			desiredRepo := strings.TrimSpace(sdef.Repo)
			if desiredRepo == "" {
				desiredRepo = strings.TrimSpace(bp.RepoURL)
			}
			if strings.TrimSpace(svc.RepoURL) != "" && desiredRepo != "" && strings.TrimSpace(svc.RepoURL) != desiredRepo {
				fail(fmt.Sprintf("service name conflict: %q already exists in this workspace (repo=%q). Blueprint wants repo=%q. Rename the service in %s or rename/delete the existing service.", name, strings.TrimSpace(svc.RepoURL), desiredRepo, bp.FilePath))
				return
			}
			if strings.TrimSpace(svc.Type) != "" && strings.TrimSpace(desiredType) != "" && strings.TrimSpace(svc.Type) != strings.TrimSpace(desiredType) {
				fail(fmt.Sprintf("service name conflict: %q already exists in this workspace (type=%q). Blueprint wants type=%q. Rename the service in %s or rename/delete the existing service.", name, strings.TrimSpace(svc.Type), strings.TrimSpace(desiredType), bp.FilePath))
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

	logLine("Preflight validation passed.")

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
				DBName: dbName, Username: dbUser, InitScript: ddef.InitScript,
			}
			if p, repaired := normalizeBlueprintPlan(db.Plan); repaired {
				log.Printf("Blueprint sync: repaired database plan for %s from %q to %q", ddef.Name, db.Plan, p)
				db.Plan = p
			} else {
				db.Plan = p
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
		// Track for rollback before any external side-effects (e.g. billing).
		st.createdDBIDs = append(st.createdDBIDs, db.ID)
		if stripeSvc != nil && db.Plan != services.PlanFree {
			if bc, err := getBillingCustomer(); err != nil || bc == nil {
				warnBilling(err)
			} else if err := stripeSvc.AddSubscriptionItem(bc, db.WorkspaceID, "database", db.ID, db.Name, db.Plan); err != nil {
				warnBilling(err)
			}
		}

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
			if p, repaired := normalizeBlueprintPlan(kv.Plan); repaired {
				log.Printf("Blueprint sync: repaired key-value plan for %s from %q to %q", kvdef.Name, kv.Plan, p)
				kv.Plan = p
			} else {
				kv.Plan = p
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
		// Track for rollback before any external side-effects (e.g. billing).
		st.createdKVIDs = append(st.createdKVIDs, kv.ID)
		if stripeSvc != nil && kv.Plan != services.PlanFree {
			if bc, err := getBillingCustomer(); err != nil || bc == nil {
				warnBilling(err)
			} else if err := stripeSvc.AddSubscriptionItem(bc, kv.WorkspaceID, "keyvalue", kv.ID, kv.Name, kv.Plan); err != nil {
				warnBilling(err)
			}
		}

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

		// Check if this service is already linked to the blueprint from a prior sync.
		alreadyLinked := false
		existing, err := models.GetBlueprintResourceByName(bp.ID, "service", serviceName)
		if err != nil {
			fail("database error")
			return
		}
		if existing != nil {
			alreadyLinked = true
		}

		dockerCtx := sdef.DockerContext
		if dockerCtx == "" && sdef.BuildContextAlias != "" {
			dockerCtx = sdef.BuildContextAlias
		}
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
		startCommand := sdef.StartCommand
		if strings.ToLower(strings.TrimSpace(desiredType)) == "static" {
			// Static sites are served by the built image; startCommand is not applicable.
			startCommand = ""
		}

		// Load the existing service: from the blueprint link, or by name in the workspace.
		var svc *models.Service
		if alreadyLinked {
			svc, _ = models.GetService(existing.ResourceID)
		}
		if svc == nil {
			svc, _ = models.GetServiceByWorkspaceAndName(bp.WorkspaceID, serviceName)
		}
		if svc == nil {
			autoDeploy := true
			if sdef.AutoDeploy != nil {
				autoDeploy = *sdef.AutoDeploy
			}
			svc = &models.Service{
				WorkspaceID: bp.WorkspaceID, Name: serviceName, Type: desiredType,
				Runtime: sdef.Runtime, RepoURL: sdef.Repo, Branch: sdef.Branch,
				BuildCommand: sdef.BuildCommand, StartCommand: startCommand,
				Port: sdef.Port, AutoDeploy: autoDeploy, Plan: sdef.Plan,
				HealthCheckPath: sdef.HealthCheckPath, PreDeployCommand: sdef.PreDeployCmd,
				DockerfilePath: sdef.DockerfilePath, DockerContext: dockerCtx,
				StaticPublishPath: sdef.StaticPublishPath, Schedule: sdef.Schedule,
				ImageURL: imageURL, BaseImage: sdef.BaseImage,
				BuildInclude: strings.Join(sdef.BuildInclude, "\n"),
				BuildExclude: strings.Join(sdef.BuildExclude, "\n"),
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
		if p, repaired := normalizeBlueprintPlan(svc.Plan); repaired {
			log.Printf("Blueprint sync: repaired service plan for %s from %q to %q", serviceName, svc.Plan, p)
			svc.Plan = p
		} else {
			svc.Plan = p
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
			// Track for rollback before any external side-effects (e.g. billing).
			st.createdServices = append(st.createdServices, svc)
			if stripeSvc != nil && svc.Plan != services.PlanFree {
				if bc, err := getBillingCustomer(); err != nil || bc == nil {
					warnBilling(err)
				} else if err := stripeSvc.AddSubscriptionItem(bc, svc.WorkspaceID, "service", svc.ID, svc.Name, svc.Plan); err != nil {
					warnBilling(err)
				}
			}
		} else {
			// Keep existing/adopted services in sync with the blueprint YAML.
			before := *svc
			// Track for rollback before any external side-effects (e.g. billing) or writes.
			st.updatedServices = append(st.updatedServices, before)

			svc.Branch = sdef.Branch
			if svc.Branch == "" {
				svc.Branch = bp.Branch
			}
			svc.BuildCommand = sdef.BuildCommand
			svc.StartCommand = startCommand
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
			svc.BaseImage = sdef.BaseImage
			svc.BuildInclude = strings.Join(sdef.BuildInclude, "\n")
			svc.BuildExclude = strings.Join(sdef.BuildExclude, "\n")

			desiredPlan, repairedPlan := normalizeBlueprintPlan(sdef.Plan)
			if repairedPlan {
				log.Printf("Blueprint sync: repaired desired plan for %s from %q to %q", serviceName, sdef.Plan, desiredPlan)
			}
			if sdef.NumInstances > 0 {
				svc.Instances = sdef.NumInstances
			} else {
				svc.Instances = 1
			}

			// Best-effort billing for plan changes — never blocks the sync.
			oldPlanEffective := strings.ToLower(strings.TrimSpace(before.Plan))
			if p, ok := services.NormalizePlan(before.Plan); ok {
				oldPlanEffective = p
			}
			if stripeSvc != nil && desiredPlan != oldPlanEffective {
				if desiredPlan == services.PlanFree {
					if err := stripeSvc.RemoveSubscriptionItem("service", svc.ID); err != nil {
						warnBilling(err)
					}
				} else {
					if bc, err := getBillingCustomer(); err != nil || bc == nil {
						warnBilling(err)
					} else if err := stripeSvc.AddSubscriptionItem(bc, svc.WorkspaceID, "service", svc.ID, svc.Name, desiredPlan); err != nil {
						warnBilling(err)
					}
				}
			}
			svc.Plan = desiredPlan

			if err := models.UpdateService(svc); err != nil {
				fail("failed to update service " + serviceName)
				return
			}
		}

		// Only create a new blueprint_resources link if this service wasn't already linked.
		if !alreadyLinked {
			linkBRs = append(linkBRs, models.BlueprintResource{
				BlueprintID: bp.ID, ResourceType: "service",
				ResourceID: svc.ID, ResourceName: serviceName,
			})
		}

		svcMap[serviceName] = svc
		if created {
			log.Printf("Blueprint sync: created service %s", serviceName)
		} else if alreadyLinked {
			log.Printf("Blueprint sync: updated existing service %s", serviceName)
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

		// Track for env var resolution in Phase 3d — both new and adopted services
		// need their YAML-declared envVars applied.
		pending = append(pending, pendingDeploy{svc: svc, sdef: sdef, created: created})
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
		if len(p.sdef.DependsOn) > 0 {
			deps := []string{}
			seen := map[string]struct{}{}
			for _, d := range p.sdef.DependsOn {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				k := strings.ToLower(d)
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				deps = append(deps, d)
			}
			if len(deps) > 0 {
				v, _ := utils.Encrypt(strings.Join(deps, ","), h.Config.Crypto.EncryptionKey)
				envVars = append(envVars, models.EnvVar{
					OwnerType: "service", OwnerID: p.svc.ID,
					Key: "RAILPUSH_DEPENDS_ON", EncryptedValue: v,
				})
			}
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
	// Only newly created services get an initial deploy; adopted services already have deploy history.
	var deployIDs []string
	var jobs []services.DeployJob
	for _, p := range pending {
		if !p.created {
			continue
		}
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

	if len(deployIDs) > 0 {
		logLine(fmt.Sprintf("Enqueued %d deploy(s).", len(deployIDs)))
	}
	logLine("Sync completed successfully.")

	success = true
	_ = models.UpdateBlueprintSyncWithLog(bp.ID, "synced", syncLog.String())
	log.Printf("Blueprint sync completed for %s", bp.Name)
}
