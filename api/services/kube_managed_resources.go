package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/railpush/api/models"
)

func kubeIDPrefix(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if len(id) >= 8 {
		id = id[:8]
	}
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = kubeNameInvalidChars.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	if len(id) > 40 {
		id = id[:40]
	}
	return id
}

func kubeManagedDatabaseName(dbID string) string {
	return "sr-db-" + kubeIDPrefix(dbID)
}

func kubeManagedKeyValueName(kvID string) string {
	return "sr-kv-" + kubeIDPrefix(kvID)
}

func kubeManagedDatabaseLabels(db *models.ManagedDatabase) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "railpush",
		"app.kubernetes.io/component":  "managed-database",
	}
	if db != nil {
		if strings.TrimSpace(db.ID) != "" {
			labels["railpush.com/database-id"] = db.ID
		}
		if strings.TrimSpace(db.WorkspaceID) != "" {
			labels["railpush.com/workspace-id"] = db.WorkspaceID
		}
	}
	return labels
}

func kubeManagedKeyValueLabels(kv *models.ManagedKeyValue) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "railpush",
		"app.kubernetes.io/component":  "managed-keyvalue",
	}
	if kv != nil {
		if strings.TrimSpace(kv.ID) != "" {
			labels["railpush.com/keyvalue-id"] = kv.ID
		}
		if strings.TrimSpace(kv.WorkspaceID) != "" {
			labels["railpush.com/workspace-id"] = kv.WorkspaceID
		}
	}
	return labels
}

func kubeManagedDatabaseStorage(plan string) resource.Quantity {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "free":
		return resource.MustParse("1Gi")
	case "starter":
		return resource.MustParse("5Gi")
	case "standard":
		return resource.MustParse("20Gi")
	case "pro":
		return resource.MustParse("100Gi")
	default:
		return resource.MustParse("5Gi")
	}
}

func kubeManagedKeyValueStorage(plan string) resource.Quantity {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "free":
		return resource.MustParse("1Gi")
	case "starter":
		return resource.MustParse("2Gi")
	case "standard":
		return resource.MustParse("5Gi")
	case "pro":
		return resource.MustParse("10Gi")
	default:
		return resource.MustParse("2Gi")
	}
}

func (k *KubeDeployer) EnsureManagedDatabase(db *models.ManagedDatabase, password string) (string, error) {
	if k == nil || k.Client == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	if db == nil || strings.TrimSpace(db.ID) == "" {
		return "", fmt.Errorf("missing database")
	}
	if strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("missing database password")
	}
	ns := k.namespace()
	name := kubeManagedDatabaseName(db.ID)
	headlessSvcName := name + "-headless"
	labels := kubeManagedDatabaseLabels(db)

	dbName := strings.TrimSpace(db.DBName)
	if dbName == "" {
		dbName = strings.TrimSpace(db.Name)
	}
	user := strings.TrimSpace(db.Username)
	if user == "" {
		user = strings.TrimSpace(db.Name)
	}
	if dbName == "" || user == "" {
		return "", fmt.Errorf("missing db_name/username")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (default-deny ingress between workspaces).
	if err := k.EnsureTenantNetworkPolicies(ctx, db.WorkspaceID); err != nil {
		return "", fmt.Errorf("ensure tenant networkpolicies: %w", err)
	}

	// 1) Secret (auth)
	secName := name + "-auth"
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secName,
			Namespace: ns,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"POSTGRES_DB":       dbName,
			"POSTGRES_USER":     user,
			"POSTGRES_PASSWORD": password,
		},
	}
	if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, secName, metav1.GetOptions{}); err == nil && existing != nil {
		sec.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update db secret: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create db secret: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get db secret: %w", err)
	}

	// 2) Services: one ClusterIP for clients, one headless for the StatefulSet.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       5432,
					TargetPort: intstr.FromInt(5432),
				},
			},
		},
	}
	if existing, err := k.Client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		svc.ResourceVersion = existing.ResourceVersion
		svc.Spec.ClusterIP = existing.Spec.ClusterIP
		svc.Spec.ClusterIPs = existing.Spec.ClusterIPs
		if _, err := k.Client.CoreV1().Services(ns).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update db service: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create db service: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get db service: %w", err)
	}

	headless := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessSvcName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       5432,
					TargetPort: intstr.FromInt(5432),
				},
			},
		},
	}
	if existing, err := k.Client.CoreV1().Services(ns).Get(ctx, headlessSvcName, metav1.GetOptions{}); err == nil && existing != nil {
		headless.ResourceVersion = existing.ResourceVersion
		// ClusterIP is immutable; preserve it (should already be None).
		headless.Spec.ClusterIP = existing.Spec.ClusterIP
		headless.Spec.ClusterIPs = existing.Spec.ClusterIPs
		if _, err := k.Client.CoreV1().Services(ns).Update(ctx, headless, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update db headless service: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Services(ns).Create(ctx, headless, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create db headless service: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get db headless service: %w", err)
	}

	// 3) StatefulSet
	replicas := int32(1)
	requests, limits := kubeResourcesForPlan(db.Plan)
	fsg := int64(70)

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: headlessSvcName,
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: func() *int64 { t := int64(60); return &t }(),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:      &fsg,
					},
					Containers: []corev1.Container{
						{
							Name:            "postgres",
							Image:           fmt.Sprintf("postgres:%d-alpine", db.PGVersion),
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{Name: "postgres", ContainerPort: 5432},
							},
							Env: []corev1.EnvVar{
								// Avoid initdb failures caused by the volume mount containing `lost+found`.
								{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
							},
							EnvFrom: []corev1.EnvFromSource{
								{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secName}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/var/lib/postgresql/data"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									// Pass args directly (avoid shell interpolation).
									Exec: &corev1.ExecAction{Command: []string{"pg_isready", "-U", user, "-d", dbName}},
								},
								PeriodSeconds:    5,
								TimeoutSeconds:   3,
								FailureThreshold: 6,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									// Pass args directly (avoid shell interpolation).
									Exec: &corev1.ExecAction{Command: []string{"pg_isready", "-U", user, "-d", dbName}},
								},
								PeriodSeconds:       10,
								TimeoutSeconds:      3,
								FailureThreshold:    3,
								InitialDelaySeconds: 10,
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "data",
						Labels: labels,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: kubeManagedDatabaseStorage(db.Plan),
							},
						},
					},
				},
			},
		},
	}

	if existing, err := k.Client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		ss.ResourceVersion = existing.ResourceVersion
		// Avoid immutable-field drift when reconciling.
		ss.Spec.ServiceName = existing.Spec.ServiceName
		ss.Spec.Selector = existing.Spec.Selector
		ss.Spec.VolumeClaimTemplates = existing.Spec.VolumeClaimTemplates
		if _, err := k.Client.AppsV1().StatefulSets(ns).Update(ctx, ss, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update db statefulset: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.AppsV1().StatefulSets(ns).Create(ctx, ss, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create db statefulset: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get db statefulset: %w", err)
	}

	return name, nil
}

func (k *KubeDeployer) EnsureManagedKeyValue(kv *models.ManagedKeyValue, password string) (string, error) {
	if k == nil || k.Client == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	if kv == nil || strings.TrimSpace(kv.ID) == "" {
		return "", fmt.Errorf("missing keyvalue")
	}
	if strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("missing keyvalue password")
	}
	ns := k.namespace()
	name := kubeManagedKeyValueName(kv.ID)
	headlessSvcName := name + "-headless"
	labels := kubeManagedKeyValueLabels(kv)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (default-deny ingress between workspaces).
	if err := k.EnsureTenantNetworkPolicies(ctx, kv.WorkspaceID); err != nil {
		return "", fmt.Errorf("ensure tenant networkpolicies: %w", err)
	}

	secName := name + "-auth"
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secName,
			Namespace: ns,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"REDIS_PASSWORD": password,
		},
	}
	if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, secName, metav1.GetOptions{}); err == nil && existing != nil {
		sec.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update kv secret: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create kv secret: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get kv secret: %w", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "redis",
					Port:       6379,
					TargetPort: intstr.FromInt(6379),
				},
			},
		},
	}
	if existing, err := k.Client.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		svc.ResourceVersion = existing.ResourceVersion
		svc.Spec.ClusterIP = existing.Spec.ClusterIP
		svc.Spec.ClusterIPs = existing.Spec.ClusterIPs
		if _, err := k.Client.CoreV1().Services(ns).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update kv service: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create kv service: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get kv service: %w", err)
	}

	headless := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessSvcName,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "redis",
					Port:       6379,
					TargetPort: intstr.FromInt(6379),
				},
			},
		},
	}
	if existing, err := k.Client.CoreV1().Services(ns).Get(ctx, headlessSvcName, metav1.GetOptions{}); err == nil && existing != nil {
		headless.ResourceVersion = existing.ResourceVersion
		headless.Spec.ClusterIP = existing.Spec.ClusterIP
		headless.Spec.ClusterIPs = existing.Spec.ClusterIPs
		if _, err := k.Client.CoreV1().Services(ns).Update(ctx, headless, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update kv headless service: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.CoreV1().Services(ns).Create(ctx, headless, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create kv headless service: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get kv headless service: %w", err)
	}

	replicas := int32(1)
	requests, limits := kubeResourcesForPlan(kv.Plan)
	policy, _ := NormalizeRedisMaxmemoryPolicy(kv.MaxmemoryPolicy)

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: headlessSvcName,
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: func() *int64 { t := int64(30); return &t }(),
					Containers: []corev1.Container{
						{
							Name:            "redis",
							Image:           "redis:7-alpine",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports:           []corev1.ContainerPort{{Name: "redis", ContainerPort: 6379}},
							EnvFrom: []corev1.EnvFromSource{
								{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secName}}},
							},
							Command: []string{"sh", "-lc"},
							Args: []string{
								// `policy` is validated/normalized (avoid shell injection via user input).
								fmt.Sprintf("exec redis-server --appendonly yes --requirepass \"$REDIS_PASSWORD\" --maxmemory-policy %s", policy),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/data"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{Command: []string{"sh", "-lc", "redis-cli -a \"$REDIS_PASSWORD\" ping | grep -q PONG"}},
								},
								PeriodSeconds:    5,
								TimeoutSeconds:   3,
								FailureThreshold: 6,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{Command: []string{"sh", "-lc", "redis-cli -a \"$REDIS_PASSWORD\" ping | grep -q PONG"}},
								},
								PeriodSeconds:       10,
								TimeoutSeconds:      3,
								FailureThreshold:    3,
								InitialDelaySeconds: 10,
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "data",
						Labels: labels,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: kubeManagedKeyValueStorage(kv.Plan),
							},
						},
					},
				},
			},
		},
	}

	if existing, err := k.Client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{}); err == nil && existing != nil {
		ss.ResourceVersion = existing.ResourceVersion
		ss.Spec.ServiceName = existing.Spec.ServiceName
		ss.Spec.Selector = existing.Spec.Selector
		ss.Spec.VolumeClaimTemplates = existing.Spec.VolumeClaimTemplates
		if _, err := k.Client.AppsV1().StatefulSets(ns).Update(ctx, ss, metav1.UpdateOptions{}); err != nil {
			return "", fmt.Errorf("update kv statefulset: %w", err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.AppsV1().StatefulSets(ns).Create(ctx, ss, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create kv statefulset: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get kv statefulset: %w", err)
	}

	return name, nil
}

func (k *KubeDeployer) WaitForStatefulSetReady(statefulSetName string, replicas int32) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := k.namespace()
	name := strings.TrimSpace(statefulSetName)
	if name == "" {
		return fmt.Errorf("missing statefulset name")
	}
	if replicas < 0 {
		replicas = 0
	}

	deadline := time.Now().Add(5 * time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ss, err := k.Client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		cancel()
		if err != nil {
			return fmt.Errorf("get statefulset: %w", err)
		}
		if ss.Status.ObservedGeneration >= ss.Generation && ss.Status.ReadyReplicas >= replicas {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for statefulset %s to be ready (ready=%d desired=%d)", name, ss.Status.ReadyReplicas, replicas)
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *KubeDeployer) DeleteManagedDatabaseResources(dbID string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	id := strings.TrimSpace(dbID)
	if id == "" {
		return fmt.Errorf("missing db id")
	}
	ns := k.namespace()
	name := kubeManagedDatabaseName(id)
	headlessSvcName := name + "-headless"
	secName := name + "-auth"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = k.Client.AppsV1().StatefulSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, headlessSvcName, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, secName, metav1.DeleteOptions{})

	// Best-effort: delete PVCs created by the StatefulSet.
	selector := "railpush.com/database-id=" + id
	if list, err := k.Client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{LabelSelector: selector}); err == nil {
		for _, pvc := range list.Items {
			_ = k.Client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
		}
	}
	return nil
}

func (k *KubeDeployer) DeleteManagedKeyValueResources(kvID string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	id := strings.TrimSpace(kvID)
	if id == "" {
		return fmt.Errorf("missing kv id")
	}
	ns := k.namespace()
	name := kubeManagedKeyValueName(id)
	headlessSvcName := name + "-headless"
	secName := name + "-auth"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = k.Client.AppsV1().StatefulSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, headlessSvcName, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, secName, metav1.DeleteOptions{})

	selector := "railpush.com/keyvalue-id=" + id
	if list, err := k.Client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{LabelSelector: selector}); err == nil {
		for _, pvc := range list.Items {
			_ = k.Client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
		}
	}
	return nil
}
