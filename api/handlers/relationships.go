package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type RelationshipHandler struct {
	Config *config.Config
}

type dependencyItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Via    string `json:"via"`
	Source string `json:"source"`
}

type topologyNode struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type topologyEdge struct {
	FromType string `json:"from_type"`
	FromID   string `json:"from_id"`
	ToType   string `json:"to_type"`
	ToID     string `json:"to_id"`
	Via      string `json:"via"`
	Source   string `json:"source"`
}

type serviceDependencySet struct {
	Databases []dependencyItem
	KeyValue  []dependencyItem
	Services  []dependencyItem
	Edges     []topologyEdge
}

type serviceEnvValue struct {
	Key   string
	Value string
}

func NewRelationshipHandler(cfg *config.Config) *RelationshipHandler {
	return &RelationshipHandler{Config: cfg}
}

func isSoftDeletedStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "soft_deleted")
}

func (h *RelationshipHandler) controlPlaneDomain() string {
	if h == nil || h.Config == nil {
		return ""
	}
	if d := strings.TrimSpace(h.Config.ControlPlane.Domain); d != "" {
		return d
	}
	return strings.TrimSpace(h.Config.Deploy.Domain)
}

func (h *RelationshipHandler) listServiceEnvValues(serviceID string) ([]serviceEnvValue, error) {
	vars, err := models.ListEnvVars("service", serviceID)
	if err != nil {
		return nil, err
	}
	encryptionKey := ""
	if h != nil && h.Config != nil {
		encryptionKey = strings.TrimSpace(h.Config.Crypto.EncryptionKey)
	}
	out := make([]serviceEnvValue, 0, len(vars))
	for _, v := range vars {
		key := strings.TrimSpace(v.Key)
		if key == "" {
			continue
		}
		value := ""
		if encryptionKey != "" && strings.TrimSpace(v.EncryptedValue) != "" {
			if decrypted, err := utils.Decrypt(v.EncryptedValue, encryptionKey); err == nil {
				value = decrypted
			}
		}
		out = append(out, serviceEnvValue{Key: key, Value: value})
	}
	return out, nil
}

func shouldIgnoreHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}

func envValueReferencesHost(value, host string, port int) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	host = strings.ToLower(strings.TrimSpace(host))
	if value == "" || shouldIgnoreHost(host) {
		return false
	}
	if strings.Contains(value, host) {
		return true
	}
	if port > 0 {
		hostPort := host + ":" + strconv.Itoa(port)
		if strings.Contains(value, hostPort) {
			return true
		}
	}
	return false
}

func envValueReferencesService(value string, svc models.Service, domain string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(svc.Name))
	if name == "" {
		return false
	}
	if value == name || strings.Contains(value, "://"+name) || strings.Contains(value, name+":") || strings.Contains(value, name+"/") || strings.Contains(value, name+".") {
		return true
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain != "" {
		host := name + "." + domain
		if strings.Contains(value, host) {
			return true
		}
	}
	return false
}

func activeServicesByWorkspace(workspaceID string) ([]models.Service, map[string]models.Service, error) {
	items, err := models.ListServices(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]models.Service, 0, len(items))
	byID := make(map[string]models.Service, len(items))
	for _, item := range items {
		if isSoftDeletedStatus(item.Status) {
			continue
		}
		out = append(out, item)
		byID[item.ID] = item
	}
	return out, byID, nil
}

func activeDatabasesByWorkspace(workspaceID string) ([]models.ManagedDatabase, map[string]models.ManagedDatabase, error) {
	items, err := models.ListManagedDatabasesByWorkspace(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]models.ManagedDatabase, 0, len(items))
	byID := make(map[string]models.ManagedDatabase, len(items))
	for _, item := range items {
		if isSoftDeletedStatus(item.Status) {
			continue
		}
		out = append(out, item)
		byID[item.ID] = item
	}
	return out, byID, nil
}

func activeKeyValuesByWorkspace(workspaceID string) ([]models.ManagedKeyValue, map[string]models.ManagedKeyValue, error) {
	items, err := models.ListManagedKeyValuesByWorkspace(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]models.ManagedKeyValue, 0, len(items))
	byID := make(map[string]models.ManagedKeyValue, len(items))
	for _, item := range items {
		if isSoftDeletedStatus(item.Status) {
			continue
		}
		out = append(out, item)
		byID[item.ID] = item
	}
	return out, byID, nil
}

func appendUniqueDependency(dest *[]dependencyItem, seen map[string]struct{}, item dependencyItem) {
	key := item.ID + "|" + strings.ToLower(strings.TrimSpace(item.Via)) + "|" + strings.ToLower(strings.TrimSpace(item.Source))
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*dest = append(*dest, item)
}

func appendUniqueEdge(dest *[]topologyEdge, seen map[string]struct{}, edge topologyEdge) {
	key := strings.Join([]string{edge.FromType, edge.FromID, edge.ToType, edge.ToID, strings.ToLower(strings.TrimSpace(edge.Via)), strings.ToLower(strings.TrimSpace(edge.Source))}, "|")
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*dest = append(*dest, edge)
}

func (h *RelationshipHandler) collectDependenciesForService(
	svc models.Service,
	workspaceServices []models.Service,
	databases []models.ManagedDatabase,
	keyValues []models.ManagedKeyValue,
) (serviceDependencySet, error) {
	result := serviceDependencySet{
		Databases: []dependencyItem{},
		KeyValue:  []dependencyItem{},
		Services:  []dependencyItem{},
		Edges:     []topologyEdge{},
	}

	envValues, err := h.listServiceEnvValues(svc.ID)
	if err != nil {
		return result, err
	}

	dbByID := make(map[string]models.ManagedDatabase, len(databases))
	for _, db := range databases {
		dbByID[db.ID] = db
	}

	seenDB := map[string]struct{}{}
	seenKV := map[string]struct{}{}
	seenSvc := map[string]struct{}{}
	seenEdges := map[string]struct{}{}

	addEdge := func(toType, toID, via, source string) {
		appendUniqueEdge(&result.Edges, seenEdges, topologyEdge{
			FromType: "service",
			FromID:   svc.ID,
			ToType:   toType,
			ToID:     toID,
			Via:      via,
			Source:   source,
		})
	}

	links, err := models.ListServiceDatabaseLinks(svc.ID)
	if err != nil {
		return result, err
	}
	for _, link := range links {
		db, ok := dbByID[link.DatabaseID]
		if !ok || isSoftDeletedStatus(db.Status) {
			continue
		}
		via := strings.TrimSpace(link.EnvVarName)
		if via == "" {
			via = "DATABASE_URL"
		}
		appendUniqueDependency(&result.Databases, seenDB, dependencyItem{ID: db.ID, Name: db.Name, Via: via, Source: "service_database_link"})
		addEdge("database", db.ID, via, "service_database_link")
	}

	domain := h.controlPlaneDomain()
	for _, env := range envValues {
		value := strings.TrimSpace(env.Value)
		if value == "" {
			continue
		}
		for _, db := range databases {
			if envValueReferencesHost(value, db.Host, db.Port) {
				appendUniqueDependency(&result.Databases, seenDB, dependencyItem{ID: db.ID, Name: db.Name, Via: env.Key, Source: "env_var"})
				addEdge("database", db.ID, env.Key, "env_var")
			}
		}
		for _, kv := range keyValues {
			if envValueReferencesHost(value, kv.Host, kv.Port) {
				appendUniqueDependency(&result.KeyValue, seenKV, dependencyItem{ID: kv.ID, Name: kv.Name, Via: env.Key, Source: "env_var"})
				addEdge("key_value", kv.ID, env.Key, "env_var")
			}
		}
		for _, target := range workspaceServices {
			if target.ID == svc.ID || isSoftDeletedStatus(target.Status) {
				continue
			}
			if envValueReferencesService(value, target, domain) {
				appendUniqueDependency(&result.Services, seenSvc, dependencyItem{ID: target.ID, Name: target.Name, Via: env.Key, Source: "env_var"})
				addEdge("service", target.ID, env.Key, "env_var")
			}
		}
	}

	return result, nil
}

func (h *RelationshipHandler) GetDatabaseConnectedServices(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	databaseID := strings.TrimSpace(mux.Vars(r)["id"])
	db, err := models.GetManagedDatabase(databaseID)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	workspaceServices, _, err := activeServicesByWorkspace(db.WorkspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list workspace services")
		return
	}
	connections := make([]map[string]string, 0)
	seen := map[string]struct{}{}
	for _, svc := range workspaceServices {
		deps, err := h.collectDependenciesForService(svc, workspaceServices, []models.ManagedDatabase{*db}, nil)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to inspect service dependencies")
			return
		}
		for _, dep := range deps.Databases {
			if dep.ID != db.ID {
				continue
			}
			key := svc.ID + "|" + dep.Via + "|" + dep.Source
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			connections = append(connections, map[string]string{
				"id":     svc.ID,
				"name":   svc.Name,
				"via":    dep.Via,
				"source": dep.Source,
			})
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"database_id":         db.ID,
		"database_name":       db.Name,
		"connected_services":  connections,
		"connected_service_count": len(connections),
	})
}

func (h *RelationshipHandler) GetDatabaseImpact(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	databaseID := strings.TrimSpace(mux.Vars(r)["id"])
	db, err := models.GetManagedDatabase(databaseID)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	workspaceServices, _, err := activeServicesByWorkspace(db.WorkspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list workspace services")
		return
	}

	affected := make([]map[string]string, 0)
	seen := map[string]struct{}{}
	for _, svc := range workspaceServices {
		deps, err := h.collectDependenciesForService(svc, workspaceServices, []models.ManagedDatabase{*db}, nil)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to inspect service dependencies")
			return
		}
		matched := false
		for _, dep := range deps.Databases {
			if dep.ID == db.ID {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if _, exists := seen[svc.ID]; exists {
			continue
		}
		seen[svc.ID] = struct{}{}
		affected = append(affected, map[string]string{"id": svc.ID, "name": svc.Name})
	}

	count := len(affected)
	summary := fmt.Sprintf("If this database is unavailable, %d service(s) may be affected.", count)
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"database_id":       db.ID,
		"database_name":     db.Name,
		"impact_count":      count,
		"affected_services": affected,
		"summary":           summary,
	})
}

func (h *RelationshipHandler) GetServiceDependencies(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := strings.TrimSpace(mux.Vars(r)["id"])
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	workspaceServices, _, err := activeServicesByWorkspace(svc.WorkspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list workspace services")
		return
	}
	databases, _, err := activeDatabasesByWorkspace(svc.WorkspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list databases")
		return
	}
	keyValues, _, err := activeKeyValuesByWorkspace(svc.WorkspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list key-value stores")
		return
	}

	deps, err := h.collectDependenciesForService(*svc, workspaceServices, databases, keyValues)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to resolve dependencies")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_id":  svc.ID,
		"service_name": svc.Name,
		"databases":   deps.Databases,
		"key_value":   deps.KeyValue,
		"services":    deps.Services,
	})
}

func (h *RelationshipHandler) GetWorkspaceTopology(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := strings.TrimSpace(mux.Vars(r)["id"])
	if workspaceID == "" {
		resolved, err := resolveWorkspaceID(r, r.URL.Query().Get("workspace_id"))
		if err != nil || strings.TrimSpace(resolved) == "" {
			utils.RespondError(w, http.StatusBadRequest, "workspace not found")
			return
		}
		workspaceID = resolved
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	workspaceServices, _, err := activeServicesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list services")
		return
	}
	databases, _, err := activeDatabasesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list databases")
		return
	}
	keyValues, _, err := activeKeyValuesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list key-value stores")
		return
	}

	nodes := make([]topologyNode, 0, len(workspaceServices)+len(databases)+len(keyValues))
	for _, svc := range workspaceServices {
		nodes = append(nodes, topologyNode{Type: "service", ID: svc.ID, Name: svc.Name, Status: svc.Status})
	}
	for _, db := range databases {
		nodes = append(nodes, topologyNode{Type: "database", ID: db.ID, Name: db.Name, Status: db.Status})
	}
	for _, kv := range keyValues {
		nodes = append(nodes, topologyNode{Type: "key_value", ID: kv.ID, Name: kv.Name, Status: kv.Status})
	}

	edges := make([]topologyEdge, 0)
	seenEdges := map[string]struct{}{}
	for _, svc := range workspaceServices {
		deps, err := h.collectDependenciesForService(svc, workspaceServices, databases, keyValues)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to resolve topology")
			return
		}
		for _, edge := range deps.Edges {
			appendUniqueEdge(&edges, seenEdges, edge)
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"workspace_id": workspaceID,
		"nodes":        nodes,
		"edges":        edges,
	})
}
