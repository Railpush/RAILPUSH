package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/railpush/api/models"
)

const (
	defaultServiceExecTimeout    = 30 * time.Second
	maxServiceExecTimeout        = 120 * time.Second
	defaultServiceExecOutputSize = 64 * 1024
	maxServiceExecOutputSize     = 256 * 1024
)

type limitedOutputBuffer struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func newLimitedOutputBuffer(maxBytes int) *limitedOutputBuffer {
	if maxBytes <= 0 {
		maxBytes = defaultServiceExecOutputSize
	}
	return &limitedOutputBuffer{maxBytes: maxBytes}
}

func (b *limitedOutputBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	written := len(p)
	if b.maxBytes <= 0 {
		b.maxBytes = defaultServiceExecOutputSize
	}
	remaining := b.maxBytes - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return written, nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return written, nil
	}
	_, _ = b.buf.Write(p)
	return written, nil
}

func (b *limitedOutputBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.buf.String()
}

func (b *limitedOutputBuffer) Truncated() bool {
	if b == nil {
		return false
	}
	return b.truncated
}

func normalizeServiceExecTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultServiceExecTimeout
	}
	if timeout > maxServiceExecTimeout {
		return maxServiceExecTimeout
	}
	return timeout
}

func normalizeServiceExecOutputSize(maxBytes int) int {
	if maxBytes <= 0 {
		return defaultServiceExecOutputSize
	}
	if maxBytes < 1024 {
		return 1024
	}
	if maxBytes > maxServiceExecOutputSize {
		return maxServiceExecOutputSize
	}
	return maxBytes
}

func (w *Worker) RunServiceExecCommand(svc *models.Service, command, runAsUser string, timeout time.Duration, maxOutputBytes int) (string, string, int, bool, bool, error) {
	if svc == nil {
		return "", "", -1, false, false, fmt.Errorf("service is nil")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", "", -1, false, false, fmt.Errorf("command is required")
	}

	timeout = normalizeServiceExecTimeout(timeout)
	maxOutputBytes = normalizeServiceExecOutputSize(maxOutputBytes)

	if w != nil && w.Config != nil && w.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(svc.ContainerID), "k8s:") {
		kd, err := w.GetKubeDeployer()
		if err != nil || kd == nil {
			return "", "", -1, false, false, fmt.Errorf("failed to initialize kubernetes exec: %v", err)
		}
		return kd.RunServiceExecCommand(svc, command, runAsUser, timeout, maxOutputBytes)
	}

	return runServiceExecDocker(w, svc, command, runAsUser, timeout, maxOutputBytes)
}

func runServiceExecDocker(w *Worker, svc *models.Service, command, runAsUser string, timeout time.Duration, maxOutputBytes int) (string, string, int, bool, bool, error) {
	if w == nil || w.Deployer == nil {
		return "", "", -1, false, false, fmt.Errorf("docker exec unavailable")
	}
	containerID := strings.TrimSpace(svc.ContainerID)
	if containerID == "" {
		return "", "", -1, false, false, fmt.Errorf("service has no running container")
	}

	args := []string{"exec"}
	if user := strings.TrimSpace(runAsUser); user != "" {
		args = append(args, "-u", user)
	}
	args = append(args, containerID, "sh", "-lc", command)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stdout := newLimitedOutputBuffer(maxOutputBytes)
	stderr := newLimitedOutputBuffer(maxOutputBytes)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()

	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if err != nil {
		if timedOut {
			exitCode = 124
			err = nil
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
				err = nil
			} else {
				return stdout.String(), stderr.String(), -1, false, stdout.Truncated() || stderr.Truncated(), err
			}
		}
	}

	if timedOut && exitCode == 0 {
		exitCode = 124
	}

	return stdout.String(), stderr.String(), exitCode, timedOut, stdout.Truncated() || stderr.Truncated(), err
}
