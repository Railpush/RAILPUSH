package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server       ServerConfig
	ControlPlane ControlPlaneConfig
	Database     DatabaseConfig
	Redis        RedisConfig
	BlueprintAI  BlueprintAIConfig
	GitHub       GitHubConfig
	Docker       DockerConfig
	Deploy       DeployConfig
	Kubernetes   KubernetesConfig
	Worker       WorkerConfig
	Logging      LoggingConfig
	Email        EmailConfig
	JWT          JWTConfig
	Crypto       CryptoConfig
	Stripe       StripeConfig
	Ops          OpsConfig
	Registrar    RegistrarConfig
	CORS         CORSConfig
}

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	PriceStarter  string
	PriceStandard string
	PricePro      string
	// Metered prices for per-minute billing. When set, new resources use metered
	// subscription items instead of flat-rate. Each price should be created in Stripe
	// with usage_type=metered, aggregate_usage=sum, and unit_amount_decimal set to
	// the per-minute cost in cents (e.g. 0.016203703704 for $7/mo ÷ 43200 min).
	MeteredPriceStarter  string
	MeteredPriceStandard string
	MeteredPricePro      string
}

type OpsConfig struct {
	// AlertWebhookToken authenticates incoming Alertmanager webhook deliveries.
	// Set this and configure Alertmanager to send `Authorization: Bearer <token>`.
	AlertWebhookToken string

	// AlertmanagerURL is the base URL for talking to Alertmanager's v2 API (for silences).
	// In-cluster default assumes kube-prometheus-stack release name "monitoring".
	AlertmanagerURL string
}

type ServerConfig struct {
	Host string
	Port int
}

type ControlPlaneConfig struct {
	Domain string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

type GitHubConfig struct {
	ClientID      string
	ClientSecret  string
	CallbackURL   string
	WebhookSecret string
}

type BlueprintAIConfig struct {
	Enabled bool

	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenRouterURL    string

	RequestTimeoutSeconds int

	MaxScanFiles   int
	MaxFileBytes   int
	MaxPromptBytes int
}

type DockerConfig struct {
	Host      string
	Network   string
	Registry  string
	BuildPath string
}

type DeployConfig struct {
	Domain         string
	CaddyAPIURL    string
	CaddyAdminPort int
	DataDir        string
	BackupDir      string
	DisableRouter  bool
}

type KubernetesConfig struct {
	Enabled      bool
	Namespace    string
	IngressClass string
	TLSSecret    string
	// StorageClass is used for RailPush-managed PVC templates (managed Postgres / Redis).
	// Default: longhorn-r2.
	StorageClass string
	// CustomDomainIssuer is the cert-manager ClusterIssuer to use for custom domains.
	// In practice this should be an HTTP-01 issuer (customer domains are not in our Cloudflare zone).
	CustomDomainIssuer string
	// TenantPodSecurityProfile controls tenant workload hardening mode.
	// "strict" (default): drop ALL caps, runAsNonRoot, readOnlyRootFilesystem + writable /tmp.
	// "compat": keep compatibility-first behavior for legacy images.
	TenantPodSecurityProfile string
}

type WorkerConfig struct {
	Enabled        bool
	Concurrency    int
	PollIntervalMS int
	LeaseSeconds   int
	MaxAttempts    int
}

type LoggingConfig struct {
	Level  string
	Format string

	// LokiURL is the base URL for Loki queries (server-side log retrieval).
	// Example (in-cluster): http://loki-gateway.logging.svc.cluster.local
	LokiURL string
}

type EmailOutboxConfig struct {
	PollIntervalMS int
	BatchSize      int
	LeaseSeconds   int
	MaxAttempts    int
}

type EmailConfig struct {
	Provider string // "smtp" (MVP); empty disables email.

	SMTPHost         string
	SMTPPort         int
	SMTPUser         string
	SMTPPassword     string
	SMTPFrom         string
	SMTPEnvelopeFrom string
	SMTPReplyTo      string

	Outbox EmailOutboxConfig
}

func (c *EmailConfig) Enabled() bool {
	if c == nil {
		return false
	}
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" || p == "none" || p == "disabled" {
		return false
	}
	if p != "smtp" {
		return false
	}
	return strings.TrimSpace(c.SMTPHost) != "" && strings.TrimSpace(c.SMTPFrom) != ""
}

type JWTConfig struct {
	Secret     string
	Expiration int
}

type CryptoConfig struct {
	EncryptionKey string
}

type RegistrarConfig struct {
	Provider     string
	NamecomUser  string
	NamecomToken string
}

type CORSConfig struct {
	AllowedOrigins []string
}

func Load() *Config {
	deployDomain := getEnv("DEPLOY_DOMAIN", "localhost")
	controlPlaneDomain := getEnv("CONTROL_PLANE_DOMAIN", deployDomain)

	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "127.0.0.1"),
			Port: getEnvInt("SERVER_PORT", 8080),
		},
		ControlPlane: ControlPlaneConfig{
			Domain: controlPlaneDomain,
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvInt("DB_PORT", 5432),
			User:     getEnv("DB_USER", "railpush"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "railpush"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		BlueprintAI: BlueprintAIConfig{
			Enabled:          getEnvBool("BLUEPRINT_AI_ENABLED", true),
			OpenRouterAPIKey: strings.TrimSpace(getEnv("OPENROUTER_API_KEY", "")),
			OpenRouterModel:  strings.TrimSpace(getEnv("OPENROUTER_MODEL", "minimax/minimax-m2.5")),
			OpenRouterURL:    strings.TrimSpace(getEnv("OPENROUTER_URL", "https://openrouter.ai/api/v1/chat/completions")),

			RequestTimeoutSeconds: getEnvInt("OPENROUTER_TIMEOUT_SECONDS", 120),

			MaxScanFiles:   getEnvInt("BLUEPRINT_AI_MAX_SCAN_FILES", 120),
			MaxFileBytes:   getEnvInt("BLUEPRINT_AI_MAX_FILE_BYTES", 20000),
			MaxPromptBytes: getEnvInt("BLUEPRINT_AI_MAX_PROMPT_BYTES", 180000),
		},
		GitHub: GitHubConfig{
			ClientID:      getEnv("GITHUB_CLIENT_ID", ""),
			ClientSecret:  getEnv("GITHUB_CLIENT_SECRET", ""),
			CallbackURL:   getEnv("GITHUB_CALLBACK_URL", "http://localhost:8080/api/v1/auth/github/callback"),
			WebhookSecret: getEnv("GITHUB_WEBHOOK_SECRET", ""),
		},
		Docker: DockerConfig{
			Host:      getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
			Network:   getEnv("DOCKER_NETWORK", "railpush"),
			Registry:  getEnv("DOCKER_REGISTRY", ""),
			BuildPath: getEnv("DOCKER_BUILD_PATH", "/tmp/railpush/builds"),
		},
		Deploy: DeployConfig{
			Domain:         deployDomain,
			CaddyAPIURL:    getEnv("CADDY_API_URL", "http://localhost:2019"),
			CaddyAdminPort: getEnvInt("CADDY_ADMIN_PORT", 2019),
			DataDir:        getEnv("DATA_DIR", "/var/lib/railpush"),
			BackupDir:      getEnv("BACKUP_DIR", "/var/lib/railpush/backups"),
			DisableRouter:  getEnvBool("DEPLOY_DISABLE_ROUTER", false),
		},
		Kubernetes: KubernetesConfig{
			Enabled:                  getEnvBool("KUBE_ENABLED", false),
			Namespace:                getEnv("KUBE_NAMESPACE", "railpush"),
			IngressClass:             getEnv("KUBE_INGRESS_CLASS", "nginx"),
			TLSSecret:                getEnv("KUBE_TLS_SECRET", "apps-wildcard-tls"),
			StorageClass:             getEnv("KUBE_STORAGE_CLASS", "longhorn-r2"),
			CustomDomainIssuer:       getEnv("KUBE_CUSTOM_DOMAIN_ISSUER", "letsencrypt-http01-prod"),
			TenantPodSecurityProfile: strings.ToLower(strings.TrimSpace(getEnv("KUBE_TENANT_POD_SECURITY_PROFILE", "strict"))),
		},
		Worker: WorkerConfig{
			Enabled:        getEnvBool("WORKER_ENABLED", true),
			Concurrency:    getEnvInt("WORKER_CONCURRENCY", 4),
			PollIntervalMS: getEnvInt("WORKER_POLL_INTERVAL_MS", 1000),
			LeaseSeconds:   getEnvInt("WORKER_LEASE_SECONDS", 900),
			MaxAttempts:    getEnvInt("WORKER_MAX_ATTEMPTS", 3),
		},
		Logging: LoggingConfig{
			Level:   getEnv("LOG_LEVEL", "info"),
			Format:  getEnv("LOG_FORMAT", "json"),
			LokiURL: strings.TrimSuffix(strings.TrimSpace(getEnv("LOKI_URL", "")), "/"),
		},
		Email: EmailConfig{
			Provider: strings.ToLower(strings.TrimSpace(getEnv("EMAIL_PROVIDER", ""))),

			SMTPHost:         getEnv("SMTP_HOST", ""),
			SMTPPort:         getEnvInt("SMTP_PORT", 587),
			SMTPUser:         getEnv("SMTP_USER", ""),
			SMTPPassword:     getEnv("SMTP_PASSWORD", ""),
			SMTPFrom:         getEnv("SMTP_FROM", ""),
			SMTPEnvelopeFrom: getEnv("SMTP_ENVELOPE_FROM", ""),
			SMTPReplyTo:      getEnv("SMTP_REPLY_TO", ""),

			Outbox: EmailOutboxConfig{
				PollIntervalMS: getEnvInt("EMAIL_OUTBOX_POLL_INTERVAL_MS", 2000),
				BatchSize:      getEnvInt("EMAIL_OUTBOX_BATCH_SIZE", 10),
				LeaseSeconds:   getEnvInt("EMAIL_OUTBOX_LEASE_SECONDS", 120),
				MaxAttempts:    getEnvInt("EMAIL_OUTBOX_MAX_ATTEMPTS", 10),
			},
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", ""),
			Expiration: getEnvInt("JWT_EXPIRATION_HOURS", 72),
		},
		Crypto: CryptoConfig{
			EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		},
		Stripe: StripeConfig{
			SecretKey:            getEnv("STRIPE_SECRET_KEY", ""),
			WebhookSecret:        getEnv("STRIPE_WEBHOOK_SECRET", ""),
			PriceStarter:         getEnv("STRIPE_PRICE_STARTER", ""),
			PriceStandard:        getEnv("STRIPE_PRICE_STANDARD", ""),
			PricePro:             getEnv("STRIPE_PRICE_PRO", ""),
			MeteredPriceStarter:  getEnv("STRIPE_METERED_PRICE_STARTER", ""),
			MeteredPriceStandard: getEnv("STRIPE_METERED_PRICE_STANDARD", ""),
			MeteredPricePro:      getEnv("STRIPE_METERED_PRICE_PRO", ""),
		},
		Ops: OpsConfig{
			AlertWebhookToken: getEnv("ALERT_WEBHOOK_TOKEN", ""),
			AlertmanagerURL:   getEnv("ALERTMANAGER_URL", "http://monitoring-kube-prometheus-alertmanager.monitoring:9093"),
		},
		Registrar: RegistrarConfig{
			Provider:     getEnv("REGISTRAR_PROVIDER", "mock"),
			NamecomUser:  getEnv("NAMECOM_API_USER", ""),
			NamecomToken: getEnv("NAMECOM_API_TOKEN", ""),
		},
		CORS: CORSConfig{
			AllowedOrigins: getEnvCSV("CORS_ALLOWED_ORIGINS"),
		},
	}
}

func (c *DatabaseConfig) DSN() string {
	return "host=" + c.Host + " port=" + strconv.Itoa(c.Port) + " user=" + c.User + " password=" + c.Password + " dbname=" + c.DBName + " sslmode=" + c.SSLMode
}

func (c *RedisConfig) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getEnvCSV(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
