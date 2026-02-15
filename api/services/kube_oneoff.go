package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
)

func findContainerImage(containers []corev1.Container, preferredName string) string {
	preferredName = strings.TrimSpace(preferredName)
	if preferredName != "" {
		for _, c := range containers {
			if c.Name == preferredName && strings.TrimSpace(c.Image) != "" {
				return strings.TrimSpace(c.Image)
			}
		}
	}
	for _, c := range containers {
		if strings.TrimSpace(c.Image) != "" {
			return strings.TrimSpace(c.Image)
		}
	}
	return ""
}

// RunOneOffJob executes a command as a Kubernetes Job using the service's currently deployed image.
// It returns the container logs, exit code, and an error if the job failed.
func (k *KubeDeployer) RunOneOffJob(oneOffJobID string, svc *models.Service, command string) (string, int, error) {
	if k == nil || k.Client == nil || k.Config == nil {
		return "kube deployer not initialized", 1, fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return "missing service", 1, fmt.Errorf("missing service")
	}
	oneOffJobID = strings.TrimSpace(oneOffJobID)
	if oneOffJobID == "" {
		return "missing job id", 1, fmt.Errorf("missing job id")
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return "command is required", 1, fmt.Errorf("missing command")
	}

	ns := k.namespace()
	svcName := kubeServiceName(svc.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (default-deny ingress between workspaces).
	if err := k.EnsureTenantNetworkPolicies(ctx, svc.WorkspaceID); err != nil {
		return "ensure tenant networkpolicies failed", 1, err
	}

	// Resolve the image from the current Deployment (web/worker/etc) or CronJob (cron services).
	image := ""
	if dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, svcName, metav1.GetOptions{}); err == nil && dep != nil {
		image = findContainerImage(dep.Spec.Template.Spec.Containers, "service")
	} else if apierrors.IsNotFound(err) {
		if cj, err := k.Client.BatchV1().CronJobs(ns).Get(ctx, svcName, metav1.GetOptions{}); err == nil && cj != nil {
			image = findContainerImage(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, "cron")
		} else if err != nil && !apierrors.IsNotFound(err) {
			return "failed to get cronjob", 1, err
		}
	} else if err != nil {
		return "failed to get deployment", 1, err
	}
	if image == "" {
		return "service is not deployed yet (no image found)", 1, fmt.Errorf("no deployed image found for service")
	}

	jobName := "rp-oneoff-" + strings.ToLower(oneOffJobID)
	jobName = kubeNameInvalidChars.ReplaceAllString(jobName, "-")
	jobName = strings.Trim(jobName, "-")
	if len(jobName) > 63 {
		jobName = jobName[:63]
		jobName = strings.Trim(jobName, "-")
	}
	if jobName == "" {
		return "invalid job name", 1, fmt.Errorf("invalid job name")
	}

	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "oneoff"
	labels["railpush.com/oneoff-id"] = oneOffJobID

	envSecretName := svcName + "-env"
	optional := true

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(0),
			TTLSecondsAfterFinished: int32Ptr(3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "oneoff",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"sh", "-lc", cmd},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: envSecretName},
										Optional:             &optional,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := k.Client.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// If the job already exists, attach to it.
		} else {
			return "failed to create job", 1, err
		}
	}

	// Wait for completion.
	for {
		select {
		case <-ctx.Done():
			logs, _ := k.readOneOffLogs(ns, jobName)
			if logs == "" {
				logs = "timeout waiting for job to complete"
			}
			return logs, 124, fmt.Errorf("timeout waiting for one-off job %s", jobName)
		default:
		}

		j, err := k.Client.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return "failed to get job", 1, err
		}
		if j.Status.Succeeded > 0 || j.Status.Failed > 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	logs, _ := k.readOneOffLogs(ns, jobName)
	exitCode, _ := k.readOneOffExitCode(ns, jobName)
	if exitCode == 0 {
		return logs, 0, nil
	}
	if exitCode < 0 {
		exitCode = 1
	}
	return logs, exitCode, fmt.Errorf("one-off job failed (exit=%d)", exitCode)
}

func (k *KubeDeployer) readOneOffLogs(namespace string, jobName string) (string, error) {
	if k == nil || k.Client == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", nil
	}
	podName := pods.Items[0].Name
	tail := int64(2000)
	limit := int64(1024 * 1024) // 1MiB
	raw, err := k.Client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  "oneoff",
		TailLines:  &tail,
		LimitBytes: &limit,
	}).DoRaw(ctx)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (k *KubeDeployer) readOneOffExitCode(namespace string, jobName string) (int, error) {
	if k == nil || k.Client == nil {
		return -1, fmt.Errorf("kube deployer not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return -1, err
	}
	if len(pods.Items) == 0 {
		return -1, nil
	}
	pod := pods.Items[0]
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name != "oneoff" || st.State.Terminated == nil {
			continue
		}
		return int(st.State.Terminated.ExitCode), nil
	}
	return -1, nil
}
