#!/usr/bin/env bash
# Deploys landing/ as a bare Node process behind systemd - no Docker, no
# compose, no build platform. Builds the standalone Next.js output plus its
# public/ and .next/static assets directly on the host: one folder, one
# process, restarted by systemd.
#
# Run this ON the host that serves the landing page (e.g. the Lenovo), after
# `git pull`. First-time setup: install the systemd unit once (see the
# heredoc at the bottom of this file), then this script just rebuilds and
# restarts it on every subsequent deploy.
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

if systemctl --user is-enabled fragforge-landing >/dev/null 2>&1; then
	systemctl --user restart fragforge-landing
	echo "deploy-landing: rebuilt and restarted fragforge-landing.service"
else
	echo "deploy-landing: built $standalone_dir/server.js"
	echo "deploy-landing: fragforge-landing.service is not installed yet; one-time setup:"
	cat <<'EOF'

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

EOF
fi
