package services

import (
	"fmt"
	"strings"

	"github.com/railpush/api/models"
)

type OneOffExecutor struct {
	Deployer *Deployer
}

func NewOneOffExecutor(deployer *Deployer) *OneOffExecutor {
	return &OneOffExecutor{Deployer: deployer}
}

func (e *OneOffExecutor) RunForService(svc *models.Service, command string) (string, int, error) {
	if svc == nil || svc.ContainerID == "" {
		return "service has no running container", 1, fmt.Errorf("service has no container")
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return "command is required", 1, fmt.Errorf("missing command")
	}
	// Execute inside running service container.
	return e.Deployer.ExecCommandWithExitCode("docker", "exec", svc.ContainerID, "sh", "-lc", cmd)
}
