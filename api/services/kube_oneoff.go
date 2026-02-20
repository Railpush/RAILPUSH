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
	requests, limits := kubeResourcesForPlan(svc.Plan)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(0),
			ActiveDeadlineSeconds:   int64Ptr(15 * 60),
			TTLSecondsAfterFinished: int32Ptr(3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					PriorityClassName: "railpush-critical",
					RestartPolicy:     corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "oneoff",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command: []string{"sh", "-lc", cmd},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: envSecretName},
										Optional:             &optional,
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
						},
					},
				},
			},
		},
	}

	applyTenantSecurityContext(&job.Spec.Template.Spec, &job.Spec.Template.Spec.Containers[0], k.strictTenantPodSecurity())

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

// RunPreDeployJob executes a pre-deploy command (e.g. database migrations) as a Kubernetes Job
// using the just-built image. Unlike RunOneOffJob, the image is provided directly (not resolved
// from the current deployment) and logs are streamed via appendLog rather than returned as a string.
func (k *KubeDeployer) RunPreDeployJob(deployID string, svc *models.Service, image string, env map[string]string, command string, appendLog func(string)) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	deployID = strings.TrimSpace(deployID)
	if deployID == "" {
		return fmt.Errorf("missing deploy id")
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("missing image")
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return fmt.Errorf("missing command")
	}

	ns := k.namespace()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation.
	if err := k.EnsureTenantNetworkPolicies(ctx, svc.WorkspaceID); err != nil {
		return fmt.Errorf("ensure tenant networkpolicies: %w", err)
	}

	// Create a temporary Secret with the service's env vars so the pre-deploy command
	// has access to DATABASE_URL and other connection strings.
	envSecretName := "rp-predeploy-" + strings.ToLower(deployID) + "-env"
	envSecretName = kubeNameInvalidChars.ReplaceAllString(envSecretName, "-")
	envSecretName = strings.Trim(envSecretName, "-")
	if len(envSecretName) > 63 {
		envSecretName = envSecretName[:63]
		envSecretName = strings.Trim(envSecretName, "-")
	}

	secretData := map[string][]byte{}
	for key, val := range env {
		secretData[key] = []byte(val)
	}
	envSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      envSecretName,
			Namespace: ns,
		},
		Data: secretData,
	}
	if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, envSecretName, metav1.GetOptions{}); err == nil && existing != nil {
		if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, envSecret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update predeploy env secret: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, envSecret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create predeploy env secret: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get predeploy env secret: %w", err)
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_ = k.Client.CoreV1().Secrets(ns).Delete(cctx, envSecretName, metav1.DeleteOptions{})
	}()

	// Build the Job.
	jobName := "rp-predeploy-" + strings.ToLower(deployID)
	jobName = kubeNameInvalidChars.ReplaceAllString(jobName, "-")
	jobName = strings.Trim(jobName, "-")
	if len(jobName) > 63 {
		jobName = jobName[:63]
		jobName = strings.Trim(jobName, "-")
	}

	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "predeploy"

	optional := true
	requests, limits := kubeResourcesForPlan(svc.Plan)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            int32Ptr(0),
			ActiveDeadlineSeconds:   int64Ptr(10 * 60),
			TTLSecondsAfterFinished: int32Ptr(300),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					PriorityClassName: "railpush-critical",
					RestartPolicy:     corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "predeploy",
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
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
						},
					},
				},
			},
		},
	}

	applyTenantSecurityContext(&job.Spec.Template.Spec, &job.Spec.Template.Spec.Containers[0], k.strictTenantPodSecurity())

	if _, err := k.Client.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if appendLog != nil {
				appendLog("==> Pre-deploy job already exists; waiting for completion...")
			}
		} else {
			return fmt.Errorf("create predeploy job: %w", err)
		}
	}

	// Wait for completion, then stream logs.
	for {
		select {
		case <-ctx.Done():
			k.appendPredeployLogs(ns, jobName, appendLog)
			return fmt.Errorf("timeout waiting for pre-deploy job %s", jobName)
		default:
		}

		j, err := k.Client.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get predeploy job: %w", err)
		}
		if j.Status.Succeeded > 0 {
			k.appendPredeployLogs(ns, jobName, appendLog)
			return nil
		}
		if j.Status.Failed > 0 {
			k.appendPredeployLogs(ns, jobName, appendLog)
			return fmt.Errorf("pre-deploy command failed")
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *KubeDeployer) appendPredeployLogs(namespace string, jobName string, appendLog func(string)) {
	if k == nil || k.Client == nil || appendLog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return
	}
	podName := pods.Items[0].Name
	logs, err := k.Client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "predeploy",
	}).DoRaw(ctx)
	if err != nil || len(logs) == 0 {
		return
	}
	for _, line := range strings.Split(string(logs), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		appendLog("    " + line)
	}
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
