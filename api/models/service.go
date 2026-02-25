package models

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
	"github.com/railpush/api/utils"
)

type Service struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	ProjectID         *string   `json:"project_id"`
	EnvironmentID     *string   `json:"environment_id"`
	Name              string    `json:"name"`
	Subdomain         string    `json:"subdomain"`
	PublicURL         string    `json:"public_url,omitempty"`
	Type              string    `json:"type"`
	Runtime           string    `json:"runtime"`
	RepoURL           string    `json:"repo_url"`
	Branch            string    `json:"branch"`
	BuildCommand      string    `json:"build_command"`
	StartCommand      string    `json:"start_command"`
	DockerfilePath    string    `json:"dockerfile_path"`
	DockerContext     string    `json:"docker_context"`
	BuildContext      string    `json:"build_context"`
	ImageURL          string    `json:"image_url"`
	HealthCheckPath   string    `json:"health_check_path"`
	Port              int       `json:"port"`
	AutoDeploy        bool      `json:"auto_deploy"`
	IsSuspended       bool      `json:"is_suspended"`
	MaxShutdownDelay  int       `json:"max_shutdown_delay"`
	PreDeployCommand  string    `json:"pre_deploy_command"`
	StaticPublishPath string    `json:"static_publish_path"`
	Schedule          string    `json:"schedule"`
	Plan              string    `json:"plan"`
	Instances         int       `json:"instances"`
	Status            string    `json:"status"`
	DockerAccess      bool      `json:"docker_access"`
	BaseImage         string    `json:"base_image"`
	BuildInclude      string    `json:"build_include"`
	BuildExclude      string    `json:"build_exclude"`
	ContainerID       string    `json:"container_id"`
	HostPort          int       `json:"host_port"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

const serviceSelectCols = `id, COALESCE(workspace_id::text,''), COALESCE(project_id::text,''), COALESCE(environment_id::text,''), COALESCE(name,''), COALESCE(subdomain,''), COALESCE(type,''), COALESCE(runtime,''), COALESCE(repo_url,''), COALESCE(branch,'main'), COALESCE(build_command,''), COALESCE(start_command,''), COALESCE(dockerfile_path,''), COALESCE(docker_context,''), COALESCE(image_url,''), COALESCE(health_check_path,''), COALESCE(port,10000), COALESCE(auto_deploy,false), COALESCE(is_suspended,false), COALESCE(max_shutdown_delay,30), COALESCE(pre_deploy_command,''), COALESCE(static_publish_path,''), COALESCE(schedule,''), COALESCE(plan,'starter'), COALESCE(instances,1), COALESCE(docker_access,false), COALESCE(base_image,''), COALESCE(build_include,''), COALESCE(build_exclude,''), COALESCE(status,'created'), COALESCE(container_id,''), COALESCE(host_port,0), created_at, updated_at`

// fillAliases populates computed alias fields (e.g. build_context mirrors docker_context).
func (s *Service) fillAliases() {
	s.BuildContext = s.DockerContext
}

func serviceStrPtrOrNil(v string) *string {
	if v == "" {
		return nil
	}
	cp := v
	return &cp
}

func ListServices(workspaceID string) ([]Service, error) {
	query := "SELECT " + serviceSelectCols + " FROM services"
	var rows *sql.Rows
	var err error
	if workspaceID != "" {
		rows, err = database.DB.Query(query+" WHERE workspace_id=$1 ORDER BY created_at DESC", workspaceID)
	} else {
		rows, err = database.DB.Query(query + " ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var svcs []Service
	for rows.Next() {
		var s Service
		var projectID, environmentID string
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ProjectID = serviceStrPtrOrNil(projectID)
		s.EnvironmentID = serviceStrPtrOrNil(environmentID)
		s.fillAliases()
		svcs = append(svcs, s)
	}
	return svcs, nil
}

// ListServicesByProject returns services directly assigned to a project (project_id)
// plus services assigned to any environments within that project.
func ListServicesByProject(projectID string) ([]Service, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return []Service{}, nil
	}

	rows, err := database.DB.Query(
		"SELECT "+serviceSelectCols+" FROM services WHERE project_id=$1 OR environment_id IN (SELECT id FROM environments WHERE project_id=$1) ORDER BY created_at DESC",
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var svcs []Service
	for rows.Next() {
		var s Service
		var pid, eid string
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &pid, &eid, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ProjectID = serviceStrPtrOrNil(pid)
		s.EnvironmentID = serviceStrPtrOrNil(eid)
		s.fillAliases()
		svcs = append(svcs, s)
	}
	if svcs == nil {
		svcs = []Service{}
	}
	return svcs, nil
}

func GetServiceByWorkspaceAndName(workspaceID string, name string) (*Service, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	if workspaceID == "" || name == "" {
		return nil, nil
	}

	s := &Service{}
	var projectID, environmentID string
	err := database.DB.QueryRow(
		"SELECT "+serviceSelectCols+" FROM services WHERE workspace_id=$1 AND lower(name)=lower($2) ORDER BY created_at ASC LIMIT 1",
		workspaceID, name,
	).Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.ProjectID = serviceStrPtrOrNil(projectID)
	s.EnvironmentID = serviceStrPtrOrNil(environmentID)
	s.fillAliases()
	return s, nil
}

// ListAutoDeployServicesByRepoBranch returns all services that should auto-deploy for a given repo+branch.
// This is used by GitHub webhooks and avoids loading the entire services table into memory.
func ListAutoDeployServicesByRepoBranch(repoURL string, branch string) ([]Service, error) {
	rows, err := database.DB.Query(
		"SELECT "+serviceSelectCols+" FROM services WHERE repo_url=$1 AND branch=$2 AND auto_deploy=true ORDER BY created_at DESC",
		repoURL, branch,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var svcs []Service
	for rows.Next() {
		var s Service
		var projectID, environmentID string
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ProjectID = serviceStrPtrOrNil(projectID)
		s.EnvironmentID = serviceStrPtrOrNil(environmentID)
		s.fillAliases()
		svcs = append(svcs, s)
	}
	return svcs, nil
}

// ListBaseServicesForPreview finds "base" services for preview environments for a given repo+base branch.
// We exclude preview-* services so previews don't recursively spawn previews.
func ListBaseServicesForPreview(repoURL string, baseBranch string) ([]Service, error) {
	rows, err := database.DB.Query(
		"SELECT "+serviceSelectCols+" FROM services WHERE repo_url=$1 AND branch=$2 AND lower(name) NOT LIKE 'preview-%' ORDER BY created_at DESC",
		repoURL, baseBranch,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var svcs []Service
	for rows.Next() {
		var s Service
		var projectID, environmentID string
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ProjectID = serviceStrPtrOrNil(projectID)
		s.EnvironmentID = serviceStrPtrOrNil(environmentID)
		s.fillAliases()
		svcs = append(svcs, s)
	}
	return svcs, nil
}

func GetService(id string) (*Service, error) {
	lookupID := strings.TrimSpace(id)
	svc, err := getServiceByIDExact(lookupID)
	if err != nil || svc != nil {
		return svc, err
	}

	resolvedID, err := resolveServiceIDPrefix(lookupID)
	if err != nil || resolvedID == "" || resolvedID == lookupID {
		return nil, err
	}
	return getServiceByIDExact(resolvedID)
}

func getServiceByIDExact(id string) (*Service, error) {
	s := &Service{}
	var projectID, environmentID string
	err := database.DB.QueryRow(
		"SELECT "+serviceSelectCols+" FROM services WHERE id::text=$1", id,
	).Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Subdomain, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.DockerAccess, &s.BaseImage, &s.BuildInclude, &s.BuildExclude, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	s.ProjectID = serviceStrPtrOrNil(projectID)
	s.EnvironmentID = serviceStrPtrOrNil(environmentID)
	s.fillAliases()
	return s, err
}

func resolveServiceIDPrefix(prefix string) (string, error) {
	if !isUUIDPrefixCandidate(prefix) {
		return "", nil
	}
	matches, err := listIDPrefixMatches(
		"SELECT id::text FROM services WHERE id::text LIKE $1 ORDER BY created_at DESC LIMIT $2",
		prefix,
		2,
	)
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", nil
}

func SuggestServiceIDs(raw string, limit int) ([]string, error) {
	return suggestIDPrefixes(
		"SELECT id::text FROM services WHERE id::text LIKE $1 ORDER BY created_at DESC LIMIT $2",
		raw,
		limit,
	)
}

func CreateService(s *Service) error {
	if s == nil {
		return fmt.Errorf("missing service")
	}

	base := strings.TrimSpace(s.Subdomain)
	if base == "" {
		base = s.Name
	}
	base = utils.ServiceDomainLabel(base)
	if base == "" {
		base = "service"
	}
	// Reserve platform subdomains under the deploy domain (e.g., grafana.apps.railpush.com).
	reserved := map[string]struct{}{
		"grafana":     {},
		"prometheus":  {},
		"alertmanager": {},
		"loki":        {},
	}
	if _, ok := reserved[base]; ok {
		base = trimToLabelLen(base, "svc")
	}

	// Allocate a unique subdomain globally. This prevents ingress host collisions across workspaces.
	for attempt := 0; attempt < 50; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = trimToLabelLen(base, fmt.Sprintf("%d", attempt+1))
		}
		s.Subdomain = candidate

		err := database.DB.QueryRow(
			"INSERT INTO services (workspace_id, project_id, environment_id, name, subdomain, type, runtime, repo_url, branch, build_command, start_command, dockerfile_path, docker_context, image_url, health_check_path, port, auto_deploy, max_shutdown_delay, pre_deploy_command, static_publish_path, schedule, plan, instances, docker_access, base_image, build_include, build_exclude) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27) RETURNING id, status, created_at, updated_at",
			s.WorkspaceID, s.ProjectID, s.EnvironmentID, s.Name, s.Subdomain, s.Type, s.Runtime, s.RepoURL, s.Branch, s.BuildCommand, s.StartCommand, s.DockerfilePath, s.DockerContext, s.ImageURL, s.HealthCheckPath, s.Port, s.AutoDeploy, s.MaxShutdownDelay, s.PreDeployCommand, s.StaticPublishPath, s.Schedule, s.Plan, s.Instances, s.DockerAccess, s.BaseImage, s.BuildInclude, s.BuildExclude,
		).Scan(&s.ID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err, "idx_services_subdomain_unique") {
			return err
		}
	}

	// Last-resort: add a short random suffix.
	rnd, _ := utils.GenerateRandomString(3) // 6 hex chars
	if rnd == "" {
		rnd = "rand"
	}
	suffix := rnd
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	s.Subdomain = trimToLabelLen(base, suffix)
	return database.DB.QueryRow(
		"INSERT INTO services (workspace_id, project_id, environment_id, name, subdomain, type, runtime, repo_url, branch, build_command, start_command, dockerfile_path, docker_context, image_url, health_check_path, port, auto_deploy, max_shutdown_delay, pre_deploy_command, static_publish_path, schedule, plan, instances, docker_access, base_image, build_include, build_exclude) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27) RETURNING id, status, created_at, updated_at",
		s.WorkspaceID, s.ProjectID, s.EnvironmentID, s.Name, s.Subdomain, s.Type, s.Runtime, s.RepoURL, s.Branch, s.BuildCommand, s.StartCommand, s.DockerfilePath, s.DockerContext, s.ImageURL, s.HealthCheckPath, s.Port, s.AutoDeploy, s.MaxShutdownDelay, s.PreDeployCommand, s.StaticPublishPath, s.Schedule, s.Plan, s.Instances, s.DockerAccess, s.BaseImage, s.BuildInclude, s.BuildExclude,
	).Scan(&s.ID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
}

func isUniqueViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	if pqErr.Code != "23505" {
		return false
	}
	if constraint == "" {
		return true
	}
	return pqErr.Constraint == constraint
}

func trimToLabelLen(base, suffix string) string {
	base = utils.ServiceDomainLabel(base)
	suffix = utils.ServiceDomainLabel(suffix)
	base = strings.Trim(base, "-")
	suffix = strings.Trim(suffix, "-")
	if base == "" {
		base = "service"
	}
	if suffix == "" {
		return base
	}

	// Ensure `base-suffix` fits within a single DNS label (63 chars).
	maxBaseLen := 63 - (len(suffix) + 1)
	if maxBaseLen < 1 {
		maxBaseLen = 1
	}
	if len(base) > maxBaseLen {
		base = strings.Trim(base[:maxBaseLen], "-")
		if base == "" {
			base = "service"
		}
	}
	return base + "-" + suffix
}

func UpdateService(s *Service) error {
	_, err := database.DB.Exec(
		"UPDATE services SET project_id=$1, environment_id=$2, name=$3, branch=$4, build_command=$5, start_command=$6, dockerfile_path=$7, docker_context=$8, image_url=$9, health_check_path=$10, port=$11, auto_deploy=$12, max_shutdown_delay=$13, pre_deploy_command=$14, static_publish_path=$15, schedule=$16, plan=$17, instances=$18, docker_access=$19, base_image=$20, build_include=$21, build_exclude=$22, updated_at=NOW() WHERE id=$23",
		s.ProjectID, s.EnvironmentID, s.Name, s.Branch, s.BuildCommand, s.StartCommand, s.DockerfilePath, s.DockerContext, s.ImageURL, s.HealthCheckPath, s.Port, s.AutoDeploy, s.MaxShutdownDelay, s.PreDeployCommand, s.StaticPublishPath, s.Schedule, s.Plan, s.Instances, s.DockerAccess, s.BaseImage, s.BuildInclude, s.BuildExclude, s.ID,
	)
	return err
}

func UpdateServiceStatus(id, status, containerID string, hostPort int) error {
	_, err := database.DB.Exec(
		"UPDATE services SET status=$1, container_id=$2, host_port=$3, updated_at=NOW() WHERE id=$4",
		status, containerID, hostPort, id,
	)
	return err
}

func SetServiceSuspended(id string, suspended bool) error {
	_, err := database.DB.Exec("UPDATE services SET is_suspended=$1, updated_at=NOW() WHERE id=$2", suspended, id)
	return err
}

func DeleteService(id string) error {
	// env_vars.owner_id does not have a FK to services; delete to avoid orphans.
	_ = DeleteEnvVars("service", id)
	_, err := database.DB.Exec("DELETE FROM services WHERE id=$1", id)
	return err
}
