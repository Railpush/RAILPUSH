package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

func (k *KubeDeployer) storageClassName() *string {
	// Default to longhorn-r2 to avoid PVs falling back to local-path if the cluster default changes.
	if k == nil || k.Config == nil {
		v := "longhorn-r2"
		return &v
	}
	sc := strings.TrimSpace(k.Config.Kubernetes.StorageClass)
	if sc == "" {
		sc = "longhorn-r2"
	}
	return &sc
}

func (k *KubeDeployer) strictTenantPodSecurity() bool {
	if k == nil || k.Config == nil {
		return true
	}
	mode := strings.ToLower(strings.TrimSpace(k.Config.Kubernetes.TenantPodSecurityProfile))
	switch mode {
	case "compat", "compatibility", "legacy":
		return false
	default:
		return true
	}
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

func kubeServiceSelectorLabels(svc *models.Service) map[string]string {
	// Keep selectors immutable and minimal for long-term compatibility.
	labels := map[string]string{
		"railpush.com/workload": "service",
	}
	if svc != nil && strings.TrimSpace(svc.ID) != "" {
		labels["railpush.com/service-id"] = svc.ID
	}
	return labels
}

func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeLabels(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func kubeServicePDBName(serviceID string) string {
	name := kubeServiceName(serviceID) + "-pdb"
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if name == "" {
		return "rp-svc-pdb"
	}
	return name
}

func (k *KubeDeployer) reconcileServicePDB(ctx context.Context, svc *models.Service, ns string, selectorLabels map[string]string, labels map[string]string, replicas int32) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	pdbName := kubeServicePDBName(svc.ID)
	if replicas <= 1 {
		if err := k.Client.PolicyV1().PodDisruptionBudgets(ns).Delete(ctx, pdbName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pdb: %w", err)
		}
		return nil
	}

	maxUnavailable := intstr.FromInt(1)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdbName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
		},
	}

	if existing, err := k.Client.PolicyV1().PodDisruptionBudgets(ns).Get(ctx, pdbName, metav1.GetOptions{}); err == nil && existing != nil {
		pdb.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.PolicyV1().PodDisruptionBudgets(ns).Update(ctx, pdb, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update pdb: %w", err)
		}
		return nil
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.PolicyV1().PodDisruptionBudgets(ns).Create(ctx, pdb, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create pdb: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("get pdb: %w", err)
	}

	return nil
}

func ingressHasHost(ing *networkingv1.Ingress, host string) bool {
	if ing == nil {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(host))
	if want == "" {
		return false
	}
	for _, rule := range ing.Spec.Rules {
		if strings.ToLower(strings.TrimSpace(rule.Host)) == want {
			return true
		}
	}
	return false
}

func (k *KubeDeployer) ensureIngressHostAvailable(ctx context.Context, ns, ingressName, host string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil
	}
	list, err := k.Client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ingresses: %w", err)
	}
	for _, ing := range list.Items {
		if ing.Name == ingressName {
			continue
		}
		if ingressHasHost(&ing, host) {
			return fmt.Errorf("ingress host %q already used by %s", host, ing.Name)
		}
	}
	return nil
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
	selectorLabels := kubeServiceSelectorLabels(svc)
	podLabels := mergeLabels(labels, selectorLabels)
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

	// This path performs multiple API calls (Secret/Deployment/Service/Ingress/custom domains), so keep a
	// single reasonably-sized budget rather than a tiny shared 30s timeout that can expire mid-way.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (best-effort upsert; fails deploy if it can't be applied).
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
	// Static sites are served by the image (nginx) and must not override the container command.
	if serviceType == "static" {
		startCmd = ""
	}
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
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGrace,
					Containers:                    []corev1.Container{container},
				},
			},
		},
	}

	applyTenantSecurityContext(&dep.Spec.Template.Spec, &dep.Spec.Template.Spec.Containers[0], k.strictTenantPodSecurity())

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

	deploymentSelectorLabels := cloneLabels(selectorLabels)
	if existing, err := k.Client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		dep.ResourceVersion = existing.ResourceVersion
		// Deployment selectors are immutable. Keep existing selectors to avoid forced delete/recreate
		// for already-running services, while using stable selector labels for new deployments.
		if existing.Spec.Selector != nil {
			dep.Spec.Selector = existing.Spec.Selector.DeepCopy()
			if len(existing.Spec.Selector.MatchLabels) > 0 {
				deploymentSelectorLabels = cloneLabels(existing.Spec.Selector.MatchLabels)
				for selectorKey, selectorValue := range existing.Spec.Selector.MatchLabels {
					dep.Spec.Template.Labels[selectorKey] = selectorValue
				}
			}
		}
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

	if err := k.reconcileServicePDB(ctx, svc, ns, deploymentSelectorLabels, labels, replicas); err != nil {
		return "", fmt.Errorf("reconcile pdb: %w", err)
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
				Selector: deploymentSelectorLabels,
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
		if err := k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			log.Printf("WARNING: delete stale Service service_id=%s name=%s: %v", svc.ID, name, err)
		}
	}

	// 4) Ingress (public service types only)
	deployDomain := strings.ToLower(strings.TrimSpace(k.Config.Deploy.Domain))
	wantIngress := needsIngress && deployDomain != "" && deployDomain != "localhost"
	if wantIngress {
		host := utils.ServiceDefaultHost(svc.Type, svc.Name, svc.Subdomain, deployDomain)
		controlPlaneDomain := strings.ToLower(strings.TrimSpace(k.Config.ControlPlane.Domain))
		if controlPlaneDomain != "" {
			if host == controlPlaneDomain || host == "www."+controlPlaneDomain {
				return "", fmt.Errorf("service host %q conflicts with reserved control-plane host", host)
			}
		}
		if err := k.ensureIngressHostAvailable(ctx, ns, name, host); err != nil {
			return "", err
		}
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
		if err := k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			log.Printf("WARNING: delete stale Ingress service_id=%s name=%s: %v", svc.ID, name, err)
		}
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

	// If the Pod is failing (CrashLoopBackOff, ImagePullBackOff, etc), surface that quickly.
	// Otherwise workers tend to time out after minutes with an unhelpful "ready=0" message.
	missingEnvVarRe := regexp.MustCompile(`Missing required environment variable:\s*([A-Za-z_][A-Za-z0-9_]*)`)

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

		// Only treat a failing pod as fatal when it's running the desired image for this rollout.
		// During a rollout, old pods may still be CrashLooping (the whole reason we're redeploying);
		// failing fast on those old pods makes "fixing a crashloop" impossible.
		desiredImage := ""
		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.Name == "service" {
				desiredImage = strings.TrimSpace(c.Image)
				break
			}
		}
		if desiredImage == "" && len(dep.Spec.Template.Spec.Containers) > 0 {
			desiredImage = strings.TrimSpace(dep.Spec.Template.Spec.Containers[0].Image)
		}

		// Best-effort diagnostics: detect crashloops / image pull failures and return a clearer error.
		if svc != nil && strings.TrimSpace(svc.ID) != "" && replicas > 0 {
			dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
			pods, perr := k.Client.CoreV1().Pods(ns).List(dctx, metav1.ListOptions{
				LabelSelector: "railpush.com/service-id=" + strings.TrimSpace(svc.ID),
			})
			dcancel()
			if perr == nil {
				for _, pod := range pods.Items {
					// Ignore old/terminating pods.
					if pod.DeletionTimestamp != nil {
						continue
					}
					if desiredImage != "" {
						podImage := ""
						for _, pc := range pod.Spec.Containers {
							if pc.Name == "service" {
								podImage = strings.TrimSpace(pc.Image)
								break
							}
						}
						if podImage != "" && podImage != desiredImage {
							continue
						}
					}
					for _, st := range pod.Status.ContainerStatuses {
						// Only inspect the primary service container.
						if st.Name != "service" {
							continue
						}
						if st.State.Waiting == nil {
							continue
						}
						reason := strings.TrimSpace(st.State.Waiting.Reason)
						msg := strings.TrimSpace(st.State.Waiting.Message)

						isFatal := reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" || reason == "CreateContainerConfigError" || reason == "InvalidImageName"
						if !isFatal {
							continue
						}

						// Try to include a helpful snippet of logs (especially for crashloops).
						var detail string
						if reason == "CrashLoopBackOff" {
							tail := int64(120)
							limit := int64(64 * 1024)
							lctx, lcancel := context.WithTimeout(context.Background(), 5*time.Second)
							raw, lerr := k.Client.CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{
								Container:  "service",
								TailLines:  &tail,
								LimitBytes: &limit,
							}).DoRaw(lctx)
							lcancel()
							if lerr == nil && len(raw) > 0 {
								lines := strings.Split(string(raw), "\n")
								// Extract a common "missing env var" pattern if present.
								for _, ln := range lines {
									m := missingEnvVarRe.FindStringSubmatch(ln)
									if len(m) == 2 && strings.TrimSpace(m[1]) != "" {
										return fmt.Errorf("service pod is crashing: missing required environment variable %s (set it in Environment Variables and redeploy)", strings.TrimSpace(m[1]))
									}
								}

								// Otherwise, try to pick a useful error line (avoid noisy tails like "Node.js vX.Y.Z").
								isNoise := func(s string) bool {
									if s == "" {
										return true
									}
									if strings.HasPrefix(s, "Node.js v") {
										return true
									}
									if s == "}" || s == "{" || s == ")" || s == "(" {
										return true
									}
									// Common stack trace frames (Node, Go, etc).
									if strings.HasPrefix(s, "at ") || strings.HasPrefix(s, "    at ") {
										return true
									}
									return false
								}
								looksLikeError := func(s string) bool {
									l := strings.ToLower(s)
									if strings.HasPrefix(s, "Error:") {
										return true
									}
									if strings.Contains(l, "cannot find module") {
										return true
									}
									if strings.Contains(l, "missing script:") {
										return true
									}
									if strings.Contains(l, "module_not_found") || strings.Contains(l, "err_module_not_found") {
										return true
									}
									if strings.Contains(l, "panic:") || strings.Contains(l, "fatal") || strings.Contains(l, "exception") {
										return true
									}
									// Python/Node stack heads; the actual error is usually near the bottom.
									if strings.Contains(l, "traceback (most recent call last)") {
										return true
									}
									return false
								}

								candidates := []string{}
								for _, ln := range lines {
									t := strings.TrimSpace(ln)
									if t == "" || isNoise(t) {
										continue
									}
									if looksLikeError(t) {
										candidates = append(candidates, t)
									}
								}
								if len(candidates) > 0 {
									detail = candidates[len(candidates)-1]
								} else {
									for i := len(lines) - 1; i >= 0; i-- {
										t := strings.TrimSpace(lines[i])
										if t == "" || isNoise(t) {
											continue
										}
										detail = t
										break
									}
								}
								if len(detail) > 420 {
									detail = detail[:420] + "…"
								}
							}
						}

						if detail != "" {
							return fmt.Errorf("service pod is failing (%s): %s", reason, detail)
						}
						if msg != "" {
							return fmt.Errorf("service pod is failing (%s): %s", reason, msg)
						}
						return fmt.Errorf("service pod is failing (%s)", reason)
					}
				}
			}
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
	_ = k.Client.PolicyV1().PodDisruptionBudgets(ns).Delete(ctx, kubeServicePDBName(svc.ID), metav1.DeleteOptions{})
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

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"railpush.com/restarted-at": ts,
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshal restart patch: %w", err)
	}
	if _, err := k.Client.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch deployment: %w", err)
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

	switch strings.ToLower(strings.TrimSpace(svc.Type)) {
	case "cron", "cron_job":
		suspend := replicas <= 0
		cj, err := k.Client.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get cronjob: %w", err)
		}
		cj.Spec.Suspend = &suspend
		if _, err := k.Client.BatchV1().CronJobs(ns).Update(ctx, cj, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update cronjob: %w", err)
		}
		return nil
	}
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

	serviceType := strings.ToLower(strings.TrimSpace(svc.Type))
	startCmd := strings.TrimSpace(svc.StartCommand)
	// Static sites are served by the image (nginx) and must not override the container command.
	if serviceType == "static" {
		startCmd = ""
	}
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
