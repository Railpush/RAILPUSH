package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

var kubeEnvKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var kubeNameInvalidChars = regexp.MustCompile(`[^a-z0-9-]+`)

type KubeDeployer struct {
	Config *config.Config
	Client kubernetes.Interface
}

func NewKubeDeployer(cfg *config.Config) (*KubeDeployer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing config")
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return &KubeDeployer{Config: cfg, Client: client}, nil
}

func (k *KubeDeployer) namespace() string {
	if k == nil || k.Config == nil {
		return "railpush"
	}
	ns := strings.TrimSpace(k.Config.Kubernetes.Namespace)
	if ns == "" {
		return "railpush"
	}
	return ns
}

func kubeServiceName(serviceID string) string {
	id := strings.ToLower(strings.TrimSpace(serviceID))
	if id == "" {
		id = "unknown"
	}
	// metadata.name is a DNS-1123 subdomain; UUIDs are safe. For anything else, be conservative.
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = kubeNameInvalidChars.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	return "rp-svc-" + id
}

func kubeServiceLabels(svc *models.Service) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "railpush",
		"app.kubernetes.io/component":  "service",
	}
	if svc != nil {
		if strings.TrimSpace(svc.ID) != "" {
			labels["railpush.com/service-id"] = svc.ID
		}
		if strings.TrimSpace(svc.WorkspaceID) != "" {
			labels["railpush.com/workspace-id"] = svc.WorkspaceID
		}
	}
	return labels
}

func kubeResourcesForPlan(plan string) (corev1.ResourceList, corev1.ResourceList) {
	plan = strings.ToLower(strings.TrimSpace(plan))
	switch plan {
	case "free":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			}
	case "starter":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
	case "standard":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}
	case "pro":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}
	default:
		// Conservative default.
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			}
	}
}

func (k *KubeDeployer) DeployService(deployID string, svc *models.Service, image string, env map[string]string) (string, error) {
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
	serviceType := strings.ToLower(strings.TrimSpace(svc.Type))
	ns := k.namespace()
	name := kubeServiceName(svc.ID)
	labels := kubeServiceLabels(svc)
	envSecretName := name + "-env"

	// Validate and normalize env var keys (required for envFrom).
	cleanEnv := map[string]string{}
	for k, v := range env {
		key := strings.TrimSpace(k)
		if key == "" || !kubeEnvKeyRegex.MatchString(key) {
			continue
		}
		cleanEnv[key] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	// 2) Deployment
	replicas := int32(1)
	if svc.Instances > 0 {
		replicas = int32(svc.Instances)
	}
	if svc.IsSuspended {
		replicas = 0
	}

	needsService := false
	switch serviceType {
	case "web", "static", "pserv":
		needsService = true
	}
	needsIngress := false
	switch serviceType {
	case "web", "static":
		needsIngress = true
	}

	port := int32(svc.Port)
	if port <= 0 {
		port = 10000
	}
	rawHealthPath := strings.TrimSpace(svc.HealthCheckPath)
	useHTTPProbes := rawHealthPath != ""
	healthPath := rawHealthPath
	if useHTTPProbes && !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}

	requests, limits := kubeResourcesForPlan(svc.Plan)

	podAnnotations := map[string]string{
		"railpush.com/deploy-id": strings.TrimSpace(deployID),
	}

	startCmd := strings.TrimSpace(svc.StartCommand)
	terminationGrace := int64(svc.MaxShutdownDelay)
	if terminationGrace < 0 {
		terminationGrace = 0
	}
	if terminationGrace > 3600 {
		terminationGrace = 3600
	}

	container := corev1.Container{
		Name:            "service",
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
	if startCmd != "" {
		// Note: this requires the image to contain `sh`.
		container.Command = []string{"sh", "-lc", startCmd}
	}
	if needsService || needsIngress {
		container.Ports = []corev1.ContainerPort{{Name: "http", ContainerPort: port}}
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGrace,
					Containers:                    []corev1.Container{container},
				},
			},
		},
	}

	// Probes for HTTP-ish service types only.
	switch serviceType {
	case "web", "static", "pserv":
		var probeHandler corev1.ProbeHandler
		if useHTTPProbes {
			probeHandler = corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: healthPath,
					Port: intstr.FromString("http"),
					// Many apps force HTTPS redirects when these headers are missing.
					HTTPHeaders: []corev1.HTTPHeader{
						{Name: "X-Forwarded-Proto", Value: "https"},
						{Name: "X-Forwarded-SSL", Value: "on"},
					},
				},
			}
		} else {
			// Default: TCP probes are more compatible for apps that redirect HTTP->HTTPS
			// but do not actually speak TLS on the container port.
			probeHandler = corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("http")},
			}
		}

		dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler:     probeHandler,
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 6,
		}

		// Many apps run migrations/seed data on boot. Without a startupProbe, the default
		// liveness probe can kill the container before it ever binds the port.
		// This keeps the platform "it just works" for real-world apps.
		dep.Spec.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{
			ProbeHandler:     probeHandler,
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 60, // 5 minutes max startup time
		}

		dep.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			ProbeHandler:        probeHandler,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
		}
	}

	if existing, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		dep.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update deployment: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.AppsV1().Deployments(ns).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create deployment: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get deployment: %w", err)
	}

	// 3) Service (only for service types with network endpoints)
	if needsService {
		svcObj := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
				Labels:    labels,
			},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Port:       port,
						TargetPort: intstr.FromInt(int(port)),
					},
				},
			},
		}

		if existing, err := k.Client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
			// ClusterIP is immutable; preserve it on update.
			svcObj.ResourceVersion = existing.ResourceVersion
			svcObj.Spec.ClusterIP = existing.Spec.ClusterIP
			svcObj.Spec.ClusterIPs = existing.Spec.ClusterIPs
			if _, err := k.Client.CoreV1().Services(ns).Update(ctx, svcObj, metav1.UpdateOptions{}); err != nil {
				return "", fmt.Errorf("update service: %w", err)
			}
		} else if apierrors.IsNotFound(err) {
			if _, err := k.Client.CoreV1().Services(ns).Create(ctx, svcObj, metav1.CreateOptions{}); err != nil {
				return "", fmt.Errorf("create service: %w", err)
			}
		} else if err != nil {
			return "", fmt.Errorf("get service: %w", err)
		}
	}
	if !needsService {
		// Ensure we don't leave behind stale Services when switching types (or upgrading from older versions).
		_ = k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	}

	// 4) Ingress (public service types only)
	deployDomain := strings.ToLower(strings.TrimSpace(k.Config.Deploy.Domain))
	wantIngress := needsIngress && deployDomain != "" && deployDomain != "localhost"
	if wantIngress {
		host := utils.ServiceDefaultHost(svc.Type, svc.Name, svc.Subdomain, deployDomain)
			ing := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns,
					Labels:    labels,
					Annotations: map[string]string{
						"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
						"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
						"nginx.ingress.kubernetes.io/proxy-body-size":    "50m",
					},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &k.Config.Kubernetes.IngressClass,
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{host},
							SecretName: strings.TrimSpace(k.Config.Kubernetes.TLSSecret),
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: host,
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypePrefix; return &pt }(),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: name,
													Port: networkingv1.ServiceBackendPort{Number: port},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}

			if existing, err := k.Client.NetworkingV1().Ingresses(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
				ing.ResourceVersion = existing.ResourceVersion
				if _, err := k.Client.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{}); err != nil {
					return "", fmt.Errorf("update ingress: %w", err)
				}
			} else if apierrors.IsNotFound(err) {
				if _, err := k.Client.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
					return "", fmt.Errorf("create ingress: %w", err)
				}
			} else if err != nil {
				return "", fmt.Errorf("get ingress: %w", err)
			}
	}
	if !wantIngress {
		// Ensure we don't leave behind stale Ingresses when switching types (or upgrading from older versions).
		_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{})
	}

	// Best-effort: keep any custom-domain ingresses in sync with the Service port.
	if err := k.ReconcileCustomDomainIngresses(svc); err != nil {
		log.Printf("WARNING: reconcile custom domains service_id=%s: %v", svc.ID, err)
	}

	return name, nil
}

func (k *KubeDeployer) WaitForServiceReady(deploymentName string, svc *models.Service) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := k.namespace()
	name := strings.TrimSpace(deploymentName)
	if name == "" {
		return fmt.Errorf("missing deployment name")
	}
	replicas := int32(1)
	if svc != nil && svc.Instances > 0 {
		replicas = int32(svc.Instances)
	}
	if svc != nil && svc.IsSuspended {
		replicas = 0
	}

	deadline := time.Now().Add(5 * time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		cancel()
		if err != nil {
			return fmt.Errorf("get deployment: %w", err)
		}

		if dep.Status.ObservedGeneration >= dep.Generation && dep.Status.ReadyReplicas >= replicas {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for deployment %s to be ready (ready=%d desired=%d)", name, dep.Status.ReadyReplicas, replicas)
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *KubeDeployer) DeleteServiceResources(svc *models.Service) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	ns := k.namespace()
	name := kubeServiceName(svc.ID)
	envSecretName := name + "-env"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Best-effort deletes (ignore not found).
	// Delete all ingresses for this service (default + custom domains).
	if svc != nil && strings.TrimSpace(svc.ID) != "" {
		if list, err := k.Client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "railpush.com/service-id=" + svc.ID,
		}); err == nil {
			for _, ing := range list.Items {
				_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, ing.Name, metav1.DeleteOptions{})
			}
		} else {
			_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{})
		}
	} else {
		_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{})
	}
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, envSecretName, metav1.DeleteOptions{})

	// Best-effort cleanup for custom domain TLS secrets (cert-manager secrets aren't labeled reliably).
	if svc != nil && strings.TrimSpace(svc.ID) != "" {
		if domains, err := models.ListCustomDomains(svc.ID); err == nil {
			for _, d := range domains {
				secretName := kubeCustomDomainTLSSecretName(svc.ID, d.Domain)
				_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{})
			}
		}
	}
	return nil
}

func (k *KubeDeployer) RestartService(svc *models.Service) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	ns := k.namespace()
	name := kubeServiceName(svc.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations["railpush.com/restarted-at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := k.Client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}
	return nil
}

func (k *KubeDeployer) ScaleService(svc *models.Service, replicas int32) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	ns := k.namespace()
	name := kubeServiceName(svc.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}
	dep.Spec.Replicas = &replicas
	if _, err := k.Client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}
	return nil
}

// UpdateServiceDeploymentResources updates a service Deployment's resource limits/requests and
// a few related runtime fields (command, termination grace). This is used for "Scaling" updates
// that should take effect immediately without a full RailPush deploy.
func (k *KubeDeployer) UpdateServiceDeploymentResources(svc *models.Service) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	switch strings.ToLower(strings.TrimSpace(svc.Type)) {
	case "cron", "cron_job":
		// CronJobs are handled by DeployCronJob.
		return nil
	}

	ns := k.namespace()
	name := kubeServiceName(svc.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}
	if dep == nil || len(dep.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("deployment has no containers")
	}

	// Prefer the standard container name.
	idx := 0
	for i := range dep.Spec.Template.Spec.Containers {
		if dep.Spec.Template.Spec.Containers[i].Name == "service" {
			idx = i
			break
		}
	}

	requests, limits := kubeResourcesForPlan(svc.Plan)
	dep.Spec.Template.Spec.Containers[idx].Resources = corev1.ResourceRequirements{
		Requests: requests,
		Limits:   limits,
	}

	startCmd := strings.TrimSpace(svc.StartCommand)
	if startCmd != "" {
		// Note: this requires the image to contain `sh`.
		dep.Spec.Template.Spec.Containers[idx].Command = []string{"sh", "-lc", startCmd}
	} else {
		dep.Spec.Template.Spec.Containers[idx].Command = nil
	}

	terminationGrace := int64(svc.MaxShutdownDelay)
	if terminationGrace < 0 {
		terminationGrace = 0
	}
	if terminationGrace > 3600 {
		terminationGrace = 3600
	}
	dep.Spec.Template.Spec.TerminationGracePeriodSeconds = &terminationGrace

	if _, err := k.Client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment: %w", err)
	}
	return nil
}
