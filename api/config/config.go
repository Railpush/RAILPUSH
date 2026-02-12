package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	GitHub    GitHubConfig
	Docker    DockerConfig
	Deploy    DeployConfig
	Logging   LoggingConfig
	JWT       JWTConfig
	Crypto    CryptoConfig
	Stripe    StripeConfig
	Registrar RegistrarConfig
	CORS      CORSConfig
}

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	PriceStarter  string
	PriceStandard string
	PricePro      string
}

type ServerConfig struct {
	Host string
	Port int
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
}

type LoggingConfig struct {
	Level  string
	Format string
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
	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "127.0.0.1"),
			Port: getEnvInt("SERVER_PORT", 8080),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvInt("DB_PORT", 5432),
			User:     getEnv("DB_USER", "railpush"),
			Password: getEnv("DB_PASSWORD", "railpush"),
			DBName:   getEnv("DB_NAME", "railpush"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
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
			Domain:         getEnv("DEPLOY_DOMAIN", "localhost"),
			CaddyAPIURL:    getEnv("CADDY_API_URL", "http://localhost:2019"),
			CaddyAdminPort: getEnvInt("CADDY_ADMIN_PORT", 2019),
			DataDir:        getEnv("DATA_DIR", "/var/lib/railpush"),
			BackupDir:      getEnv("BACKUP_DIR", "/var/lib/railpush/backups"),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", ""),
			Expiration: getEnvInt("JWT_EXPIRATION_HOURS", 72),
		},
		Crypto: CryptoConfig{
			EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		},
		Stripe: StripeConfig{
			SecretKey:     getEnv("STRIPE_SECRET_KEY", ""),
			WebhookSecret: getEnv("STRIPE_WEBHOOK_SECRET", ""),
			PriceStarter:  getEnv("STRIPE_PRICE_STARTER", ""),
			PriceStandard: getEnv("STRIPE_PRICE_STANDARD", ""),
			PricePro:      getEnv("STRIPE_PRICE_PRO", ""),
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
