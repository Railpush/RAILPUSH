package services

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type Deployer struct {
	Config *config.Config
}

func NewDeployer(cfg *config.Config) *Deployer {
	return &Deployer{Config: cfg}
}

func (d *Deployer) RunContainer(svc *models.Service, imageTag string) (string, int, error) {
	port := d.findFreePort()
	name := fmt.Sprintf("sr-%s", svc.ID[:8])

	// Remove any existing container with the same name
	exec.Command("docker", "rm", "-f", name).Run()

	args := []string{"run", "-d", "--name", name, "--network", d.Config.Docker.Network,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, svc.Port), imageTag}
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("%v: %s", err, string(out))
	}
	cid := strings.TrimSpace(string(out))
	if len(cid) > 12 {
		cid = cid[:12]
	}
	log.Printf("Started container %s on port %d", cid, port)
	return cid, port, nil
}

func (d *Deployer) RunContainerNamed(svc *models.Service, imageTag, name string, publishPort bool) (string, int, error) {
	port := 0
	// Remove any existing container with the same name
	exec.Command("docker", "rm", "-f", name).Run()

	args := []string{"run", "-d", "--name", name, "--network", d.Config.Docker.Network}
	if publishPort {
		port = d.findFreePort()
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, svc.Port))
	}
	args = append(args, imageTag)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("%v: %s", err, string(out))
	}
	cid := strings.TrimSpace(string(out))
	if len(cid) > 12 {
		cid = cid[:12]
	}
	log.Printf("Started container %s name=%s port=%d", cid, name, port)
	return cid, port, nil
}

func (d *Deployer) RunContainerWithEnv(svc *models.Service, imageTag string, envVars []models.EnvVar, encKey string) (string, int, error) {
	name := fmt.Sprintf("sr-%s", svc.ID[:8])
	return d.RunContainerWithEnvNamed(svc, imageTag, envVars, encKey, name, true)
}

func (d *Deployer) RunContainerWithEnvNamed(svc *models.Service, imageTag string, envVars []models.EnvVar, encKey, name string, publishPort bool) (string, int, error) {
	port := d.findFreePort()
	if !publishPort {
		port = 0
	}

	// Remove any existing container with the same name
	exec.Command("docker", "rm", "-f", name).Run()

	args := []string{"run", "-d", "--name", name, "--network", d.Config.Docker.Network}
	if publishPort {
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, svc.Port))
	}

	// Add environment variables
	for _, ev := range envVars {
		val := ev.Value
		if val == "" && ev.EncryptedValue != "" {
			decrypted, err := utils.Decrypt(ev.EncryptedValue, encKey)
			if err == nil {
				val = decrypted
			}
		}
		if val != "" {
			args = append(args, "-e", fmt.Sprintf("%s=%s", ev.Key, val))
		}
	}

	// Add PORT env var
	args = append(args, "-e", fmt.Sprintf("PORT=%d", svc.Port))

	args = append(args, imageTag)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("%v: %s", err, string(out))
	}
	cid := strings.TrimSpace(string(out))
	if len(cid) > 12 {
		cid = cid[:12]
	}
	log.Printf("Started container %s name=%s on port %d", cid, name, port)
	return cid, port, nil
}

func (d *Deployer) StopContainer(containerID string) error {
	return exec.Command("docker", "stop", containerID).Run()
}

func (d *Deployer) StartContainer(containerID string) error {
	return exec.Command("docker", "start", containerID).Run()
}

func (d *Deployer) RestartContainer(containerID string) error {
	return exec.Command("docker", "restart", containerID).Run()
}

func (d *Deployer) RemoveContainer(containerID string) error {
	exec.Command("docker", "stop", containerID).Run()
	return exec.Command("docker", "rm", "-f", containerID).Run()
}

// HealthCheck performs HTTP health checks against the service
func (d *Deployer) HealthCheck(host string, port int, path string) bool {
	if path == "" {
		path = "/"
	}

	url := fmt.Sprintf("http://%s:%d%s", host, port, path)
	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < 30; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		// Also try TCP as fallback for the first few attempts (container might not have HTTP yet)
		if i < 5 {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 2*time.Second)
			if err == nil {
				conn.Close()
				// TCP is up, give HTTP a bit more time
			}
		}
		time.Sleep(time.Second)
	}
	return false
}

func (d *Deployer) Deploy(svc *models.Service, imageTag string) error {
	if svc.ContainerID != "" {
		d.RemoveContainer(svc.ContainerID)
	}
	cid, port, err := d.RunContainer(svc, imageTag)
	if err != nil {
		return err
	}
	models.UpdateServiceStatus(svc.ID, "live", cid, port)
	return nil
}

func (d *Deployer) findFreePort() int {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 10000
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ExecCommand runs a command and returns the output as a string
func (d *Deployer) ExecCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// ExecCommandWithExitCode runs a command and returns stdout/stderr, exit code, and error.
func (d *Deployer) ExecCommandWithExitCode(name string, args ...string) (string, int, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode(), err
	}
	return string(out), -1, err
}

// ExecCommandNoOutput runs a command and discards the output
func (d *Deployer) ExecCommandNoOutput(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// GetContainerStats returns current CPU and memory stats for a container
func (d *Deployer) GetContainerStats(containerID string) (map[string]interface{}, error) {
	out, err := exec.Command("docker", "stats", "--no-stream", "--format",
		`{"cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","mem_perc":"{{.MemPerc}}","net":"{{.NetIO}}","block":"{{.BlockIO}}","pids":"{{.PIDs}}"}`,
		containerID).Output()
	if err != nil {
		return nil, err
	}
	// Parse the JSON-like output
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, fmt.Errorf("no stats available")
	}
	result := map[string]interface{}{
		"raw": s,
	}
	return result, nil
}
