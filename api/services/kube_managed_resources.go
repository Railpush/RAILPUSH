package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/railpush/api/models"
)

func generateSelfSignedCertPEM(commonName string, dnsNames []string) (crtPEM []byte, keyPEM []byte, _ error) {
	commonName = strings.TrimSpace(commonName)
	if commonName == "" {
		commonName = "postgres"
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	crtPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return crtPEM, keyPEM, nil
}

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

const (
	tcpServicesConfigMapName = "tcp-services"
	ingressNginxNamespace    = "ingress-nginx"
)

// SetTCPServiceEntry adds or updates an entry in the ingress-nginx tcp-services ConfigMap.
// The ConfigMap maps external ports to internal services: "<port>": "<namespace>/<svc>:<targetPort>".
// This enables nginx to TCP-proxy external connections to in-cluster database services.
func (k *KubeDeployer) SetTCPServiceEntry(ctx context.Context, externalPort int, internalSvc string, targetPort int) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := ingressNginxNamespace
	key := fmt.Sprintf("%d", externalPort)
	value := fmt.Sprintf("%s/%s:%d", k.namespace(), internalSvc, targetPort)

	cm, err := k.Client.CoreV1().ConfigMaps(ns).Get(ctx, tcpServicesConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tcpServicesConfigMapName,
				Namespace: ns,
			},
			Data: map[string]string{key: value},
		}
		_, err = k.Client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return fmt.Errorf("get tcp-services configmap: %w", err)
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[key] = value
	_, err = k.Client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// RemoveTCPServiceEntry removes an entry from the ingress-nginx tcp-services ConfigMap.
func (k *KubeDeployer) RemoveTCPServiceEntry(ctx context.Context, externalPort int) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := ingressNginxNamespace
	key := fmt.Sprintf("%d", externalPort)

	cm, err := k.Client.CoreV1().ConfigMaps(ns).Get(ctx, tcpServicesConfigMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil // nothing to remove
	}
	if err != nil {
		return fmt.Errorf("get tcp-services configmap: %w", err)
	}
	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[key]; !exists {
		return nil
	}
	delete(cm.Data, key)
	_, err = k.Client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	return err
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

	// 1b) Secret (TLS) - make Postgres accept SSL connections (Render-compatible).
	//
	// Many apps assume SSL in production (e.g. Render, Heroku). Enabling SSL on the server side
	// keeps us compatible even when clients don't explicitly add sslmode params.
	tlsSecName := name + "-tls"
	if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, tlsSecName, metav1.GetOptions{}); err == nil && existing != nil {
		// Do not rotate certs automatically; just ensure the secret exists.
	} else if apierrors.IsNotFound(err) {
		dns := []string{
			name,
			fmt.Sprintf("%s.%s", name, ns),
			fmt.Sprintf("%s.%s.svc", name, ns),
			fmt.Sprintf("%s.%s.svc.cluster.local", name, ns),
			// Headless service DNS (useful for direct pod connections / debugging).
			headlessSvcName,
			fmt.Sprintf("%s.%s", headlessSvcName, ns),
			fmt.Sprintf("%s.%s.svc", headlessSvcName, ns),
			fmt.Sprintf("%s.%s.svc.cluster.local", headlessSvcName, ns),
		}
		crt, key, genErr := generateSelfSignedCertPEM(name, dns)
		if genErr != nil {
			return "", fmt.Errorf("generate db tls cert: %w", genErr)
		}
		tlsSec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tlsSecName,
				Namespace: ns,
				Labels:    labels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"server.crt": crt,
				"server.key": key,
			},
		}
		if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, tlsSec, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create db tls secret: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get db tls secret: %w", err)
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
	rootUID := int64(0)

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
					PriorityClassName:             "railpush-critical",
					TerminationGracePeriodSeconds: func() *int64 { t := int64(60); return &t }(),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &fsg,
					},
					InitContainers: []corev1.Container{
						{
							Name:            "init-postgres-tls",
							Image:           "busybox:1.36.1",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command: []string{
								"sh",
								"-lc",
								"set -e; cp /tls-src/server.crt /tls/server.crt; cp /tls-src/server.key /tls/server.key; chown 70:70 /tls/server.key /tls/server.crt; chmod 600 /tls/server.key; chmod 644 /tls/server.crt",
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &rootUID,
								RunAsGroup: &rootUID,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "tls-src", MountPath: "/tls-src", ReadOnly: true},
								{Name: "tls", MountPath: "/tls"},
							},
						},
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
							Args: []string{
								"postgres",
								"-c", "wal_level=replica",
								"-c", "archive_mode=on",
								"-c", "archive_timeout=60",
								"-c", "archive_command=mkdir -p /var/lib/postgresql/data/wal-archive && test ! -f /var/lib/postgresql/data/wal-archive/%f && cp %p /var/lib/postgresql/data/wal-archive/%f",
								"-c", "ssl=on",
								"-c", "ssl_cert_file=/etc/postgres-ssl/server.crt",
								"-c", "ssl_key_file=/etc/postgres-ssl/server.key",
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/var/lib/postgresql/data"},
								{Name: "tls", MountPath: "/etc/postgres-ssl", ReadOnly: true},
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
					Volumes: []corev1.Volume{
						{
							Name: "tls-src",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  tlsSecName,
									DefaultMode: func() *int32 { m := int32(0644); return &m }(),
								},
							},
						},
						{
							Name: "tls",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
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
						StorageClassName: k.storageClassName(),
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
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
					PriorityClassName:             "railpush-critical",
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
							Args: []string{
								// Avoid `sh -c` here: use args so user-controlled inputs can't become shell syntax.
								"redis-server",
								"--appendonly",
								"yes",
								"--requirepass",
								"$(REDIS_PASSWORD)",
								"--maxmemory-policy",
								policy,
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
						StorageClassName: k.storageClassName(),
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
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
	authSecName := name + "-auth"
	tlsSecName := name + "-tls"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = k.Client.AppsV1().StatefulSets(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Services(ns).Delete(ctx, headlessSvcName, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, authSecName, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, tlsSecName, metav1.DeleteOptions{})

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

// RunDatabaseInitScript runs a one-time SQL init script against a managed database.
// It executes as a K8s Job using psql inside the database pod.
func (k *KubeDeployer) RunDatabaseInitScript(db *models.ManagedDatabase, password string, initScript string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := k.namespace()
	name := kubeManagedDatabaseName(db.ID)
	host := name // ClusterIP service name

	dbName := strings.TrimSpace(db.DBName)
	if dbName == "" {
		dbName = strings.TrimSpace(db.Name)
	}
	user := strings.TrimSpace(db.Username)
	if user == "" {
		user = strings.TrimSpace(db.Name)
	}

	// Use a short-lived pod that runs psql with the init script.
	// The password is injected via the existing auth Secret (never embedded in pod args).
	// The init script is passed as an env var and piped to psql via stdin to avoid
	// shell injection — no user content is ever interpreted by sh.
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	podName := name + "-init"
	authSecretName := name + "-auth"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Clean up any previous init pod (best-effort) and wait until it's gone.
	_ = k.Client.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})
	for i := 0; i < 15; i++ {
		if _, err := k.Client.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			break
		}
		time.Sleep(1 * time.Second)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "init",
					Image: fmt.Sprintf("postgres:%d-alpine", db.PGVersion),
					// Write the init script from env var to a temp file, then run via psql -f.
					// Using printenv avoids shell expansion of $, backticks, etc. in the SQL.
					Command: []string{"sh", "-c",
						fmt.Sprintf("printenv INIT_SQL > /tmp/init.sql && psql -h %s -p 5432 -f /tmp/init.sql", host),
					},
					Env: []corev1.EnvVar{
						{Name: "PGUSER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: authSecretName}, Key: "POSTGRES_USER",
						}}},
						{Name: "PGPASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: authSecretName}, Key: "POSTGRES_PASSWORD",
						}}},
						{Name: "PGDATABASE", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: authSecretName}, Key: "POSTGRES_DB",
						}}},
						{Name: "PGSSLMODE", Value: "require"},
						{Name: "INIT_SQL", Value: initScript},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	if _, err := k.Client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create init pod: %w", err)
	}

	// Wait for completion.
	for {
		select {
		case <-ctx.Done():
			_ = k.Client.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
			return fmt.Errorf("init script timed out")
		default:
		}

		p, err := k.Client.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get init pod: %w", err)
		}
		if p.Status.Phase == corev1.PodSucceeded {
			_ = k.Client.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})
			return nil
		}
		if p.Status.Phase == corev1.PodFailed {
			_ = k.Client.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})
			return fmt.Errorf("init script failed (pod phase=Failed)")
		}
		time.Sleep(2 * time.Second)
	}
}

// RunDatabaseQuery executes SQL in the target managed database pod and returns stdout/stderr.
// This path avoids cluster networking dependencies by using localhost access from inside the DB pod.
func (k *KubeDeployer) RunDatabaseQuery(db *models.ManagedDatabase, password string, query string, timeout time.Duration, readOnly bool) (string, string, error) {
	if k == nil || k.Client == nil {
		return "", "", fmt.Errorf("kube deployer not initialized")
	}
	if db == nil {
		return "", "", fmt.Errorf("database is nil")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	dbName := strings.TrimSpace(db.DBName)
	if dbName == "" {
		dbName = strings.TrimSpace(db.Name)
	}
	user := strings.TrimSpace(db.Username)
	if user == "" {
		user = strings.TrimSpace(db.Name)
	}
	if dbName == "" || user == "" {
		return "", "", fmt.Errorf("database connection metadata is incomplete")
	}

	ns := k.namespace()
	podName := kubeManagedDatabaseName(db.ID) + "-0"
	queryB64 := base64.StdEncoding.EncodeToString([]byte(query))
	pgOptionsPrefix := ""
	if readOnly {
		pgOptionsPrefix = "PGOPTIONS='-c default_transaction_read_only=on' "
	}
	script := "set -euo pipefail; " +
		"printf %s " + shellSingleQuote(queryB64) + " | base64 -d > /tmp/railpush-query.sql; " +
		"PGPASSWORD=" + shellSingleQuote(password) + " " +
		pgOptionsPrefix + "psql -v ON_ERROR_STOP=1 --no-psqlrc --csv -h 127.0.0.1 -p 5432 -U " + shellSingleQuote(user) + " -d " + shellSingleQuote(dbName) + " -f /tmp/railpush-query.sql"

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return "", "", fmt.Errorf("in-cluster config: %w", err)
	}

	req := k.Client.CoreV1().RESTClient().Post().
		Namespace(ns).
		Resource("pods").
		Name(podName).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "postgres",
		Command:   []string{"sh", "-lc", script},
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restCfg, http.MethodPost, req.URL())
	if err != nil {
		return "", "", fmt.Errorf("create exec stream: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		serr := strings.TrimSpace(stderr.String())
		if serr != "" {
			return stdout.String(), serr, fmt.Errorf("query execution failed: %w", err)
		}
		return stdout.String(), "", fmt.Errorf("query execution failed: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}

func shellSingleQuote(input string) string {
	if input == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(input, "'", "'\"'\"'") + "'"
}
