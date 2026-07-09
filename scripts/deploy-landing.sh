#!/usr/bin/env bash
# Deploys landing/ as a bare Node process behind systemd - no Docker, no
# compose, no build platform. Builds the standalone Next.js output plus its
# public/ and .next/static assets directly on the host: one folder, one
# process, restarted by systemd. TLS/public exposure is a separate, host-
# specific concern (Tailscale Funnel needs nothing extra; a plain public VPS
# needs nginx+certbot - see the one-time setup block below for both).
#
# Run this ON the host that serves the landing page, after `git pull`.
# First-time setup: install the systemd unit once (see the heredoc printed
# below), then this script just rebuilds and restarts it on every deploy.
# Works as root (a system-wide unit under /etc/systemd/system, no linger
# needed) or as a regular user (a --user unit, needs `loginctl enable-linger`
# once so it survives logout/reboot) - detected automatically.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
landing_dir="$repo_root/landing"
standalone_dir="$landing_dir/.next/standalone"

cd "$landing_dir"
npm ci
NEXT_TELEMETRY_DISABLED=1 npm run build

mkdir -p "$standalone_dir/public" "$standalone_dir/.next"
rm -rf "$standalone_dir/.next/static"
if [ -d "$landing_dir/public" ]; then
	cp -r "$landing_dir/public/." "$standalone_dir/public/"
fi
cp -r "$landing_dir/.next/static" "$standalone_dir/.next/static"

if [ "$(id -u)" -eq 0 ]; then
	systemctl="systemctl"
	unit_path="/etc/systemd/system/fragforge-landing.service"
	working_dir="$standalone_dir"
else
	systemctl="systemctl --user"
	unit_path="$HOME/.config/systemd/user/fragforge-landing.service"
	working_dir="$standalone_dir"
fi

if $systemctl is-enabled fragforge-landing >/dev/null 2>&1; then
	$systemctl restart fragforge-landing
	echo "deploy-landing: rebuilt and restarted fragforge-landing.service"
else
	echo "deploy-landing: built $standalone_dir/server.js"
	echo "deploy-landing: fragforge-landing.service is not installed yet; one-time setup:"
	if [ "$(id -u)" -eq 0 ]; then
		cat <<UNITEOF

  cat > $unit_path <<'UNIT'
  [Unit]
  Description=FragForge landing page
  After=network.target

  [Service]
  WorkingDirectory=$working_dir
  Environment=NODE_ENV=production PORT=3100 HOSTNAME=127.0.0.1
  ExecStart=/usr/bin/node server.js
  Restart=always
  RestartSec=2

  [Install]
  WantedBy=multi-user.target
  UNIT
  systemctl daemon-reload
  systemctl enable --now fragforge-landing

UNITEOF
	else
		cat <<'UNITEOF'

  mkdir -p ~/.config/systemd/user
  cat > ~/.config/systemd/user/fragforge-landing.service <<'UNIT'
  [Unit]
  Description=FragForge landing page

  [Service]
  WorkingDirectory=%h/projects/fragforge/landing/.next/standalone
  Environment=NODE_ENV=production PORT=3100 HOSTNAME=127.0.0.1
  ExecStart=/usr/bin/node server.js
  Restart=always
  RestartSec=2

  [Install]
  WantedBy=default.target
  UNIT
  systemctl --user daemon-reload
  systemctl --user enable --now fragforge-landing
  loginctl enable-linger "$USER"   # keep it running after logout/reboot

UNITEOF
	fi
	cat <<'EOF'

  If this host has no Tailscale Funnel in front of it (a plain public VPS,
  not the Lenovo), it also needs its own TLS termination - one-time nginx
  setup, no Docker, matching the same "one file per site" pattern:

  apt-get install -y nginx certbot python3-certbot-nginx
  cat > /etc/nginx/sites-available/landing.example.sslip.io <<'NGINX'
  server {
      listen 80;
      server_name landing.example.sslip.io;
      location / { proxy_pass http://127.0.0.1:3100; }
  }
  NGINX
  ln -s /etc/nginx/sites-available/landing.example.sslip.io /etc/nginx/sites-enabled/
  nginx -t && systemctl reload nginx
  certbot --nginx -d landing.example.sslip.io   # adds the HTTPS server block and auto-renew

EOF
fi
