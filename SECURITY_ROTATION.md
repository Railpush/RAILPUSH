# Security Rotation Runbook

This project now loads runtime secrets from `/etc/railpush/railpush.env` (root-owned, mode `600`) instead of embedding them in `railpush-api.service`.

## 1. Rotate server-local secrets

Run on server:

```bash
sudo bash -lc '
set -euo pipefail
ENV=/etc/railpush/railpush.env
set -a; . "$ENV"; set +a
NEW_DB_PASSWORD=$(openssl rand -hex 24)
NEW_JWT_SECRET=$(openssl rand -hex 32)
sudo -u postgres psql -v ON_ERROR_STOP=1 -c "ALTER ROLE ${DB_USER} WITH PASSWORD '\''${NEW_DB_PASSWORD}'\'';"
sed -i "s|^DB_PASSWORD=.*|DB_PASSWORD=${NEW_DB_PASSWORD}|" "$ENV"
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=${NEW_JWT_SECRET}|" "$ENV"
chmod 600 "$ENV"
systemctl restart railpush-api.service
'
```

## 2. Rotate provider-managed secrets

These must be rotated in provider dashboards, then updated in `/etc/railpush/railpush.env`:

- `GITHUB_CLIENT_SECRET` (GitHub OAuth app)
- `GITHUB_WEBHOOK_SECRET` (all GitHub webhooks must be updated to match)
- `STRIPE_SECRET_KEY`
- `STRIPE_WEBHOOK_SECRET`

After updating:

```bash
sudo systemctl restart railpush-api.service
```

## 3. Git history scrub (required after previous secret exposure)

`git filter-repo` is recommended:

```bash
pipx install git-filter-repo
cd /path/to/repo
git filter-repo --path railpush-api.service --invert-paths
# Or replace sensitive strings with --replace-text if needed.
git push --force --all
git push --force --tags
```

Coordinate force-push with all collaborators before running this step.

## 4. Validation checklist

```bash
systemctl show railpush-api.service -p EnvironmentFiles
ls -l /etc/railpush/railpush.env
systemctl is-active railpush-api.service
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/api/v1/auth/user
```

Expected:

- `EnvironmentFiles=/etc/railpush/railpush.env`
- env file permissions `-rw-------`
- service status `active`
- `/api/v1/auth/user` returns `401` without token
