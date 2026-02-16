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

// DeployCronJob creates/updates a Kubernetes CronJob for a cron service.
// The image should already exist (either prebuilt or built via Kaniko).
func (k *KubeDeployer) DeployCronJob(deployID string, svc *models.Service, image string, env map[string]string) (string, error) {
	if k == nil || k.Client == nil || k.Config == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return "", fmt.Errorf("missing service")
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("missing image")
	}
	schedule := strings.TrimSpace(svc.Schedule)
	if schedule == "" {
		return "", fmt.Errorf("missing schedule")
	}

	ns := k.namespace()
	name := kubeServiceName(svc.ID)
	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "cronjob"
	envSecretName := name + "-env"

	// Validate and normalize env var keys (required for envFrom).
	cleanEnv := map[string]string{}
	for envKey, v := range env {
		key := strings.TrimSpace(envKey)
		if key == "" || !kubeEnvKeyRegex.MatchString(key) {
			continue
		}
		cleanEnv[key] = v
	}

	// Deploying a CronJob performs multiple API calls (Secret + CronJob), so avoid a tiny shared timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (default-deny ingress between workspaces).
	if err := k.EnsureTenantNetworkPolicies(ctx, svc.WorkspaceID); err != nil {
		return "", fmt.Errorf("ensure tenant networkpolicies: %w", err)
	}

	// 1) Secret (env)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      envSecretName,
			Namespace: ns,
			Labels:    labels,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: cleanEnv,
	}
	if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, envSecretName, metav1.GetOptions{}); err == nil && existing != nil {
		sec.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update env secret: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create env secret: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get env secret: %w", err)
	}

	// 2) CronJob
	suspend := svc.IsSuspended
	requests, limits := kubeResourcesForPlan(svc.Plan)

	container := corev1.Container{
		Name:            "cron",
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		EnvFrom: []corev1.EnvFromSource{
			{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: envSecretName}}},
		},
		Resources: corev1.ResourceRequirements{
			Requests: requests,
			Limits:   limits,
		},
	}
	startCmd := strings.TrimSpace(svc.StartCommand)
	if startCmd != "" {
		container.Command = []string{"sh", "-lc", startCmd}
	}

	podAnnotations := map[string]string{
		"railpush.com/deploy-id": strings.TrimSpace(deployID),
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			Suspend:           &suspend,
			SuccessfulJobsHistoryLimit: int32Ptr(1),
			FailedJobsHistoryLimit:     int32Ptr(3),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit: int32Ptr(0),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      labels,
							Annotations: podAnnotations,
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers:    []corev1.Container{container},
						},
					},
				},
			},
		},
	}

	// Compatibility-first hardening for customer pods.
	applyCompatSecurityContext(&cronJob.Spec.JobTemplate.Spec.Template.Spec, &cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0])

	if existing, err := k.Client.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		cronJob.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.BatchV1().CronJobs(ns).Update(ctx, cronJob, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update cronjob: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.BatchV1().CronJobs(ns).Create(ctx, cronJob, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create cronjob: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get cronjob: %w", err)
	}

	return name, nil
}
