#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

RAILPUSH_ENV_DIR=/etc/railpush
RAILPUSH_ENV_FILE="${RAILPUSH_ENV_DIR}/railpush.env"

generate_secret() {
  local bytes="${1:-32}"
  openssl rand -hex "${bytes}"
}

ensure_railpush_env() {
  mkdir -p "${RAILPUSH_ENV_DIR}"
  chmod 700 "${RAILPUSH_ENV_DIR}"

  if [ -f "${RAILPUSH_ENV_FILE}" ]; then
    set -a
    # shellcheck source=/dev/null
    . "${RAILPUSH_ENV_FILE}"
    set +a
  fi

  DB_HOST="${DB_HOST:-localhost}"
  DB_PORT="${DB_PORT:-5432}"
  DB_NAME="${DB_NAME:-railpush}"
  DB_USER="${DB_USER:-railpush}"
  DB_PASSWORD="${DB_PASSWORD:-$(generate_secret 18)}"
  DB_SSLMODE="${DB_SSLMODE:-disable}"

  SERVER_PORT="${SERVER_PORT:-8080}"
  SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
  JWT_SECRET="${JWT_SECRET:-$(generate_secret 32)}"
  DEPLOY_DOMAIN="${DEPLOY_DOMAIN:-railpush.com}"
  CONTROL_PLANE_DOMAIN="${CONTROL_PLANE_DOMAIN:-${DEPLOY_DOMAIN}}"
  CORS_ALLOWED_ORIGINS="${CORS_ALLOWED_ORIGINS:-https://${CONTROL_PLANE_DOMAIN},https://www.${CONTROL_PLANE_DOMAIN},https://${DEPLOY_DOMAIN},https://www.${DEPLOY_DOMAIN},http://localhost:5173,http://127.0.0.1:5173}"
  DOCKER_NETWORK="${DOCKER_NETWORK:-railpush}"
  ENCRYPTION_KEY="${ENCRYPTION_KEY:-$(generate_secret 16)}"

  GITHUB_CLIENT_ID="${GITHUB_CLIENT_ID:-}"
  GITHUB_CLIENT_SECRET="${GITHUB_CLIENT_SECRET:-}"
  GITHUB_CALLBACK_URL="${GITHUB_CALLBACK_URL:-https://${CONTROL_PLANE_DOMAIN}/api/v1/auth/github/callback}"
  GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-$(generate_secret 32)}"

  STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY:-}"
  STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET:-}"
  STRIPE_PRICE_STARTER="${STRIPE_PRICE_STARTER:-}"
  STRIPE_PRICE_STANDARD="${STRIPE_PRICE_STANDARD:-}"
  STRIPE_PRICE_PRO="${STRIPE_PRICE_PRO:-}"

  cat > "${RAILPUSH_ENV_FILE}" <<EOF
# RailPush runtime environment.
# Keep this file root-owned with mode 600.
DB_HOST=${DB_HOST}
DB_PORT=${DB_PORT}
DB_NAME=${DB_NAME}
DB_USER=${DB_USER}
DB_PASSWORD=${DB_PASSWORD}
DB_SSLMODE=${DB_SSLMODE}

SERVER_PORT=${SERVER_PORT}
SERVER_HOST=${SERVER_HOST}
JWT_SECRET=${JWT_SECRET}
CONTROL_PLANE_DOMAIN=${CONTROL_PLANE_DOMAIN}
DEPLOY_DOMAIN=${DEPLOY_DOMAIN}
CORS_ALLOWED_ORIGINS=${CORS_ALLOWED_ORIGINS}
DOCKER_NETWORK=${DOCKER_NETWORK}
ENCRYPTION_KEY=${ENCRYPTION_KEY}

GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
GITHUB_CALLBACK_URL=${GITHUB_CALLBACK_URL}
GITHUB_WEBHOOK_SECRET=${GITHUB_WEBHOOK_SECRET}

STRIPE_SECRET_KEY=${STRIPE_SECRET_KEY}
STRIPE_WEBHOOK_SECRET=${STRIPE_WEBHOOK_SECRET}
STRIPE_PRICE_STARTER=${STRIPE_PRICE_STARTER}
STRIPE_PRICE_STANDARD=${STRIPE_PRICE_STANDARD}
STRIPE_PRICE_PRO=${STRIPE_PRICE_PRO}
EOF

  chmod 600 "${RAILPUSH_ENV_FILE}"
}

ensure_railpush_env

echo "=== RailPush PaaS - Server Setup ==="

# Step 1: Update
echo "[1/9] Updating system..."
apt update -y && apt upgrade -y

# Step 2: Docker
echo "[2/9] Installing Docker..."
if ! command -v docker &>/dev/null; then
  apt install -y ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --batch --yes --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
  apt update -y
  apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
systemctl enable docker && systemctl start docker
docker --version

# Step 3: PostgreSQL 16
echo "[3/9] Installing PostgreSQL 16..."
if ! command -v psql &>/dev/null; then
  apt install -y wget lsb-release
  wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --batch --yes --dearmor -o /etc/apt/keyrings/postgresql.gpg
  echo "deb [signed-by=/etc/apt/keyrings/postgresql.gpg] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | tee /etc/apt/sources.list.d/pgdg.list > /dev/null
  apt update -y
  apt install -y postgresql-16 postgresql-client-16
fi
systemctl enable postgresql && systemctl start postgresql
sudo -u postgres psql <<SQL >/dev/null 2>&1 || true
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '${DB_USER}') THEN
    EXECUTE 'CREATE ROLE ${DB_USER} LOGIN PASSWORD ''${DB_PASSWORD}''';
  ELSE
    EXECUTE 'ALTER ROLE ${DB_USER} WITH LOGIN PASSWORD ''${DB_PASSWORD}''';
  END IF;
END
\$\$;
SQL
sudo -u postgres psql -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>/dev/null || true
sudo -u postgres psql -c "ALTER USER ${DB_USER} CREATEDB;" 2>/dev/null || true
psql --version

# Step 4: Redis 7
echo "[4/9] Installing Redis..."
if ! command -v redis-server &>/dev/null; then
  apt install -y redis-server
fi
systemctl enable redis-server && systemctl start redis-server
redis-server --version

# Step 4b: Fail2ban
echo "[4b/9] Enabling fail2ban..."
if ! command -v fail2ban-client &>/dev/null; then
  apt install -y fail2ban
fi
systemctl enable fail2ban && systemctl restart fail2ban

# Step 5: Caddy 2
echo "[5/9] Installing Caddy..."
if ! command -v caddy &>/dev/null; then
  apt install -y debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt update -y
  apt install -y caddy
fi
systemctl enable caddy
caddy version

# Step 6: Go 1.22
echo "[6/9] Installing Go..."
if ! command -v /usr/local/go/bin/go &>/dev/null; then
  cd /tmp
  wget -q https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
  rm -rf /usr/local/go && tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
  rm -f go1.22.5.linux-amd64.tar.gz
  grep -q '/usr/local/go/bin' /etc/profile || echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
fi
export PATH=$PATH:/usr/local/go/bin
go version

# Step 7: Directories
echo "[7/9] Creating directories..."
mkdir -p /var/lib/railpush/{buildkit-cache,builds,images,static,disks,backups/{postgres,redis},logs,secrets}
mkdir -p /etc/railpush/caddy
mkdir -p /opt/railpush/{api,dashboard}

# Step 8: System user
echo "[8/9] Creating railpush user..."
useradd -r -m -s /usr/sbin/nologin -d /var/lib/railpush railpush 2>/dev/null || true
usermod -aG docker railpush 2>/dev/null || true
chown -R railpush:railpush /var/lib/railpush /opt/railpush

# Step 9: Docker network
echo "[9/9] Creating Docker network..."
docker network create "${DOCKER_NETWORK}" 2>/dev/null || true
if command -v ufw &>/dev/null; then
  ufw deny 8080/tcp >/dev/null 2>&1 || true
fi

echo ""
echo "=== VERIFICATION ==="
echo "Docker:     $(docker --version)"
echo "PostgreSQL: $(psql --version)"
echo "Redis:      $(redis-server --version)"
echo "Caddy:      $(caddy version)"
echo "Go:         $(/usr/local/go/bin/go version)"
echo "Node:       $(node --version)"
echo ""
echo "=== Server setup complete! ==="
