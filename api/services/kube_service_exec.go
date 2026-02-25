package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/railpush/api/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

func servicePodReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (k *KubeDeployer) selectServiceExecPod(ctx context.Context, serviceID string) (string, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return "", fmt.Errorf("missing service id")
	}

	ns := k.namespace()
	pods, err := k.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "railpush.com/service-id=" + serviceID,
	})
	if err != nil {
		return "", fmt.Errorf("list service pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no running pod found for service")
	}

	readyPods := make([]corev1.Pod, 0, len(pods.Items))
	runningPods := make([]corev1.Pod, 0, len(pods.Items))
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		runningPods = append(runningPods, p)
		if servicePodReady(&p) {
			readyPods = append(readyPods, p)
		}
	}

	target := readyPods
	if len(target) == 0 {
		target = runningPods
	}
	if len(target) == 0 {
		return "", fmt.Errorf("service has no Running pod ready for exec")
	}

	sort.Slice(target, func(i, j int) bool {
		return target[i].CreationTimestamp.Time.After(target[j].CreationTimestamp.Time)
	})

	return target[0].Name, nil
}

func (k *KubeDeployer) RunServiceExecCommand(svc *models.Service, command, runAsUser string, timeout time.Duration, maxOutputBytes int) (string, string, int, bool, bool, error) {
	if k == nil || k.Client == nil {
		return "", "", -1, false, false, fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return "", "", -1, false, false, fmt.Errorf("service is nil")
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return "", "", -1, false, false, fmt.Errorf("command is required")
	}
	if strings.TrimSpace(runAsUser) != "" {
		return "", "", -1, false, false, fmt.Errorf("user override is not supported for kubernetes exec")
	}

	timeout = normalizeServiceExecTimeout(timeout)
	maxOutputBytes = normalizeServiceExecOutputSize(maxOutputBytes)

	podName, err := k.selectServiceExecPod(context.Background(), svc.ID)
	if err != nil {
		return "", "", -1, false, false, err
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return "", "", -1, false, false, fmt.Errorf("in-cluster config: %w", err)
	}

	req := k.Client.CoreV1().RESTClient().Post().
		Namespace(k.namespace()).
		Resource("pods").
		Name(podName).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "service",
		Command:   []string{"sh", "-lc", command},
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	streamer, err := remotecommand.NewSPDYExecutor(restCfg, http.MethodPost, req.URL())
	if err != nil {
		return "", "", -1, false, false, fmt.Errorf("create exec stream: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stdout := newLimitedOutputBuffer(maxOutputBytes)
	stderr := newLimitedOutputBuffer(maxOutputBytes)
	streamErr := streamer.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if streamErr != nil {
		if timedOut {
			exitCode = 124
			streamErr = nil
		} else {
			var statusErr utilexec.ExitError
			if errors.As(streamErr, &statusErr) {
				exitCode = statusErr.ExitStatus()
				streamErr = nil
			} else {
				return stdout.String(), stderr.String(), -1, false, stdout.Truncated() || stderr.Truncated(), fmt.Errorf("exec stream failed: %w", streamErr)
			}
		}
	}

	if timedOut && exitCode == 0 {
		exitCode = 124
	}

	return stdout.String(), stderr.String(), exitCode, timedOut, stdout.Truncated() || stderr.Truncated(), streamErr
}
