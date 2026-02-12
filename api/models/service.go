package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type Service struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	ProjectID         *string   `json:"project_id"`
	EnvironmentID     *string   `json:"environment_id"`
	Name              string    `json:"name"`
	PublicURL         string    `json:"public_url,omitempty"`
	Type              string    `json:"type"`
	Runtime           string    `json:"runtime"`
	RepoURL           string    `json:"repo_url"`
	Branch            string    `json:"branch"`
	BuildCommand      string    `json:"build_command"`
	StartCommand      string    `json:"start_command"`
	DockerfilePath    string    `json:"dockerfile_path"`
	DockerContext     string    `json:"docker_context"`
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
	ContainerID       string    `json:"container_id"`
	HostPort          int       `json:"host_port"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

const serviceSelectCols = `id, COALESCE(workspace_id::text,''), COALESCE(project_id::text,''), COALESCE(environment_id::text,''), name, COALESCE(type,''), COALESCE(runtime,''), COALESCE(repo_url,''), COALESCE(branch,'main'), COALESCE(build_command,''), COALESCE(start_command,''), COALESCE(dockerfile_path,''), COALESCE(docker_context,''), COALESCE(image_url,''), COALESCE(health_check_path,''), COALESCE(port,10000), COALESCE(auto_deploy,false), COALESCE(is_suspended,false), COALESCE(max_shutdown_delay,30), COALESCE(pre_deploy_command,''), COALESCE(static_publish_path,''), COALESCE(schedule,''), COALESCE(plan,'starter'), COALESCE(instances,1), COALESCE(status,'created'), COALESCE(container_id,''), COALESCE(host_port,0), created_at, updated_at`

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
		if err := rows.Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ProjectID = serviceStrPtrOrNil(projectID)
		s.EnvironmentID = serviceStrPtrOrNil(environmentID)
		svcs = append(svcs, s)
	}
	return svcs, nil
}

func GetService(id string) (*Service, error) {
	s := &Service{}
	var projectID, environmentID string
	err := database.DB.QueryRow(
		"SELECT "+serviceSelectCols+" FROM services WHERE id=$1", id,
	).Scan(&s.ID, &s.WorkspaceID, &projectID, &environmentID, &s.Name, &s.Type, &s.Runtime, &s.RepoURL, &s.Branch, &s.BuildCommand, &s.StartCommand, &s.DockerfilePath, &s.DockerContext, &s.ImageURL, &s.HealthCheckPath, &s.Port, &s.AutoDeploy, &s.IsSuspended, &s.MaxShutdownDelay, &s.PreDeployCommand, &s.StaticPublishPath, &s.Schedule, &s.Plan, &s.Instances, &s.Status, &s.ContainerID, &s.HostPort, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	s.ProjectID = serviceStrPtrOrNil(projectID)
	s.EnvironmentID = serviceStrPtrOrNil(environmentID)
	return s, err
}

func CreateService(s *Service) error {
	return database.DB.QueryRow(
		"INSERT INTO services (workspace_id, project_id, environment_id, name, type, runtime, repo_url, branch, build_command, start_command, dockerfile_path, docker_context, image_url, health_check_path, port, auto_deploy, max_shutdown_delay, pre_deploy_command, static_publish_path, schedule, plan, instances) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) RETURNING id, status, created_at, updated_at",
		s.WorkspaceID, s.ProjectID, s.EnvironmentID, s.Name, s.Type, s.Runtime, s.RepoURL, s.Branch, s.BuildCommand, s.StartCommand, s.DockerfilePath, s.DockerContext, s.ImageURL, s.HealthCheckPath, s.Port, s.AutoDeploy, s.MaxShutdownDelay, s.PreDeployCommand, s.StaticPublishPath, s.Schedule, s.Plan, s.Instances,
	).Scan(&s.ID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
}

func UpdateService(s *Service) error {
	_, err := database.DB.Exec(
		"UPDATE services SET project_id=$1, environment_id=$2, name=$3, branch=$4, build_command=$5, start_command=$6, dockerfile_path=$7, docker_context=$8, image_url=$9, health_check_path=$10, port=$11, auto_deploy=$12, max_shutdown_delay=$13, pre_deploy_command=$14, static_publish_path=$15, schedule=$16, plan=$17, instances=$18, updated_at=NOW() WHERE id=$19",
		s.ProjectID, s.EnvironmentID, s.Name, s.Branch, s.BuildCommand, s.StartCommand, s.DockerfilePath, s.DockerContext, s.ImageURL, s.HealthCheckPath, s.Port, s.AutoDeploy, s.MaxShutdownDelay, s.PreDeployCommand, s.StaticPublishPath, s.Schedule, s.Plan, s.Instances, s.ID,
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
	_, err := database.DB.Exec("DELETE FROM services WHERE id=$1", id)
	return err
}
