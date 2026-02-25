package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type templateResourceDef struct {
	Kind            string   `json:"kind"`
	NameSuffix      string   `json:"name_suffix"`
	ServiceType     string   `json:"service_type,omitempty"`
	Runtime         string   `json:"runtime,omitempty"`
	BuildCommand    string   `json:"build_command,omitempty"`
	StartCommand    string   `json:"start_command,omitempty"`
	HealthCheckPath string   `json:"health_check_path,omitempty"`
	EnvRefs         []string `json:"env_refs,omitempty"`
}

type serviceTemplate struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Category    string                `json:"category"`
	Tags        []string              `json:"tags"`
	Verified    bool                  `json:"verified"`
	Resources   []templateResourceDef `json:"resources"`
}

type TemplateHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewTemplateHandler(cfg *config.Config, worker *services.Worker) *TemplateHandler {
	return &TemplateHandler{Config: cfg, Worker: worker}
}

var templateCatalog = []serviceTemplate{
	{
		ID:          "nextjs-postgres",
		Name:        "Next.js + PostgreSQL",
		Description: "Deploy a Next.js web service with a managed PostgreSQL database.",
		Category:    "full-stack",
		Tags:        []string{"nextjs", "react", "postgresql", "web"},
		Verified:    true,
		Resources: []templateResourceDef{
			{Kind: "service", NameSuffix: "web", ServiceType: "web", Runtime: "node", BuildCommand: "npm install && npm run build", StartCommand: "npm run start", HealthCheckPath: "/"},
			{Kind: "database", NameSuffix: "db"},
		},
	},
	{
		ID:          "django-postgres-redis",
		Name:        "Django + PostgreSQL + Redis",
		Description: "Deploy Django web + worker services with managed PostgreSQL and Redis.",
		Category:    "full-stack",
		Tags:        []string{"python", "django", "postgresql", "redis", "worker"},
		Verified:    true,
		Resources: []templateResourceDef{
			{Kind: "service", NameSuffix: "web", ServiceType: "web", Runtime: "python", BuildCommand: "pip install -r requirements.txt", StartCommand: "gunicorn app.wsgi:application", HealthCheckPath: "/healthz", EnvRefs: []string{"DATABASE_URL", "REDIS_URL"}},
			{Kind: "service", NameSuffix: "worker", ServiceType: "worker", Runtime: "python", BuildCommand: "pip install -r requirements.txt", StartCommand: "celery -A app worker -l info", EnvRefs: []string{"DATABASE_URL", "REDIS_URL"}},
			{Kind: "database", NameSuffix: "db"},
			{Kind: "keyvalue", NameSuffix: "cache"},
		},
	},
	{
		ID:          "mcp-server",
		Name:        "MCP Server",
		Description: "Deploy a Node.js MCP server starter service.",
		Category:    "agent",
		Tags:        []string{"mcp", "agent", "node"},
		Verified:    true,
		Resources: []templateResourceDef{
			{Kind: "service", NameSuffix: "mcp", ServiceType: "web", Runtime: "node", BuildCommand: "npm install && npm run build", StartCommand: "npm run start", HealthCheckPath: "/healthz"},
		},
	},
	{
		ID:          "webhook-receiver",
		Name:        "Webhook Receiver",
		Description: "Deploy a minimal webhook ingestion service.",
		Category:    "agent",
		Tags:        []string{"webhooks", "api", "node"},
		Verified:    true,
		Resources: []templateResourceDef{
			{Kind: "service", NameSuffix: "receiver", ServiceType: "web", Runtime: "node", BuildCommand: "npm install", StartCommand: "npm run start", HealthCheckPath: "/healthz"},
		},
	},
}

func findTemplateByID(id string) (*serviceTemplate, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for i := range templateCatalog {
		t := &templateCatalog[i]
		if t.ID == id {
			return t, true
		}
	}
	return nil, false
}

func (h *TemplateHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("category")))
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("query")))
	out := make([]serviceTemplate, 0, len(templateCatalog))
	for _, t := range templateCatalog {
		if category != "" && strings.ToLower(t.Category) != category {
			continue
		}
		if query != "" {
			hay := strings.ToLower(t.Name + " " + t.Description + " " + strings.Join(t.Tags, " "))
			if !strings.Contains(hay, query) {
				continue
			}
		}
		out = append(out, t)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"templates": out})
}

func (h *TemplateHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	t, ok := findTemplateByID(id)
	if !ok {
		utils.RespondError(w, http.StatusNotFound, "template not found")
		return
	}
	utils.RespondJSON(w, http.StatusOK, t)
}

type deployTemplateRequest struct {
	WorkspaceID    string                 `json:"workspace_id"`
	ProjectID      *string                `json:"project_id"`
	EnvironmentID  *string                `json:"environment_id"`
	NamePrefix     string                 `json:"name_prefix"`
	RepoURL        string                 `json:"repo_url"`
	Branch         string                 `json:"branch"`
	Plan           string                 `json:"plan"`
	Customizations map[string]interface{} `json:"customizations"`
}

func (h *TemplateHandler) DeployTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := mux.Vars(r)["id"]
	tpl, ok := findTemplateByID(templateID)
	if !ok {
		utils.RespondError(w, http.StatusNotFound, "template not found")
		return
	}

	var req deployTemplateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, req.WorkspaceID)
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		project, err := models.GetProject(strings.TrimSpace(*req.ProjectID))
		if err != nil || project == nil || project.WorkspaceID != workspaceID {
			utils.RespondError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
	}
	if req.EnvironmentID != nil && strings.TrimSpace(*req.EnvironmentID) != "" {
		env, err := models.GetEnvironment(strings.TrimSpace(*req.EnvironmentID))
		if err != nil || env == nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid environment_id")
			return
		}
		if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" && env.ProjectID != strings.TrimSpace(*req.ProjectID) {
			utils.RespondError(w, http.StatusBadRequest, "environment does not belong to project")
			return
		}
	}

	namePrefix := strings.TrimSpace(req.NamePrefix)
	if namePrefix == "" {
		namePrefix = tpl.ID
	}

	serviceNeeded := false
	for _, resource := range tpl.Resources {
		if resource.Kind == "service" {
			serviceNeeded = true
			break
		}
	}
	if serviceNeeded && strings.TrimSpace(req.RepoURL) == "" {
		utils.RespondError(w, http.StatusBadRequest, "repo_url is required for this template")
		return
	}

	plan := strings.TrimSpace(strings.ToLower(req.Plan))
	if plan == "" {
		plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(plan); ok {
		plan = p
	} else {
		utils.RespondError(w, http.StatusBadRequest, "invalid plan")
		return
	}

	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = "main"
	}

	var createdServices []models.Service
	var createdDatabases []models.ManagedDatabase
	var createdKeyValues []models.ManagedKeyValue

	var dbURL string
	var redisURL string
	for _, resource := range tpl.Resources {
		switch resource.Kind {
		case "database":
			db, password, err := h.createTemplateDatabase(workspaceID, namePrefix+"-"+resource.NameSuffix, plan)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to create template database: "+err.Error())
				return
			}
			createdDatabases = append(createdDatabases, *db)
			dbURL = fmt.Sprintf("postgresql://%s:%s@%s:%d/%s", db.Username, password, db.Host, db.Port, db.DBName)
		case "keyvalue":
			kv, password, err := h.createTemplateKeyValue(workspaceID, namePrefix+"-"+resource.NameSuffix, plan)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to create template key-value store: "+err.Error())
				return
			}
			createdKeyValues = append(createdKeyValues, *kv)
			redisURL = fmt.Sprintf("redis://:%s@%s:%d", password, kv.Host, kv.Port)
		}
	}

	for _, resource := range tpl.Resources {
		if resource.Kind != "service" {
			continue
		}
		svc := models.Service{
			WorkspaceID:      workspaceID,
			ProjectID:        req.ProjectID,
			EnvironmentID:    req.EnvironmentID,
			Name:             namePrefix + "-" + resource.NameSuffix,
			Type:             resource.ServiceType,
			Runtime:          resource.Runtime,
			RepoURL:          strings.TrimSpace(req.RepoURL),
			Branch:           branch,
			BuildCommand:     resource.BuildCommand,
			StartCommand:     resource.StartCommand,
			HealthCheckPath:  resource.HealthCheckPath,
			Port:             10000,
			AutoDeploy:       true,
			Plan:             plan,
			Instances:        1,
			MaxShutdownDelay: 30,
		}
		if err := models.CreateService(&svc); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create template service: "+err.Error())
			return
		}

		envVars := make([]models.EnvVar, 0, 2)
		for _, key := range resource.EnvRefs {
			if key == "DATABASE_URL" && dbURL != "" {
				enc, _ := utils.Encrypt(dbURL, h.Config.Crypto.EncryptionKey)
				envVars = append(envVars, models.EnvVar{Key: "DATABASE_URL", EncryptedValue: enc, IsSecret: true})
			}
			if key == "REDIS_URL" && redisURL != "" {
				enc, _ := utils.Encrypt(redisURL, h.Config.Crypto.EncryptionKey)
				envVars = append(envVars, models.EnvVar{Key: "REDIS_URL", EncryptedValue: enc, IsSecret: true})
			}
		}
		if len(envVars) > 0 {
			_ = models.MergeUpsertEnvVars("service", svc.ID, envVars)
		}

		createdServices = append(createdServices, svc)
	}

	services.Audit(workspaceID, userID, "template.deployed", "template", tpl.ID, map[string]interface{}{
		"template_id":     tpl.ID,
		"name_prefix":     namePrefix,
		"services_count":  len(createdServices),
		"databases_count": len(createdDatabases),
		"keyvalue_count":  len(createdKeyValues),
	})

	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"status":    "ok",
		"template":  tpl,
		"services":  createdServices,
		"databases": createdDatabases,
		"keyvalue":  createdKeyValues,
	})
}

func (h *TemplateHandler) createTemplateDatabase(workspaceID, name, plan string) (*models.ManagedDatabase, string, error) {
	dbIdent := sanitizeDBIdentifier(name)
	pw, _ := utils.GenerateRandomString(16)
	encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
	db := &models.ManagedDatabase{
		WorkspaceID:       workspaceID,
		Name:              name,
		Plan:              plan,
		PGVersion:         16,
		Host:              "localhost",
		Port:              5432,
		DBName:            dbIdent,
		Username:          dbIdent,
		EncryptedPassword: encrypted,
	}
	if err := models.CreateManagedDatabase(db); err != nil {
		return nil, "", err
	}
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		internalHost := fmt.Sprintf("sr-db-%s", db.ID[:8])
		db.Host = internalHost
		db.Port = 5432
		_ = models.UpdateManagedDatabaseConnection(db.ID, 5432, internalHost)
	}
	h.Worker.ProvisionDatabase(db, pw)
	return db, pw, nil
}

func (h *TemplateHandler) createTemplateKeyValue(workspaceID, name, plan string) (*models.ManagedKeyValue, string, error) {
	pw, _ := utils.GenerateRandomString(16)
	encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
	kv := &models.ManagedKeyValue{
		WorkspaceID:       workspaceID,
		Name:              name,
		Plan:              plan,
		Host:              "localhost",
		Port:              6379,
		EncryptedPassword: encrypted,
		MaxmemoryPolicy:   "allkeys-lru",
	}
	if err := models.CreateManagedKeyValue(kv); err != nil {
		return nil, "", err
	}
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		internalHost := "sr-kv-" + kv.ID[:8]
		kv.Host = internalHost
		kv.Port = 6379
		_ = models.UpdateManagedKeyValueConnection(kv.ID, 6379, internalHost)
	}
	h.Worker.ProvisionKeyValue(kv, pw)
	return kv, pw, nil
}

func sanitizeDBIdentifier(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.ReplaceAll(clean, "-", "_")
	if clean == "" {
		clean = "app"
	}
	if len(clean) > 30 {
		clean = clean[:30]
	}
	return clean
}
