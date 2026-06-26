#!/bin/sh
set -e

# Dedicated, unprivileged system user/group that the service runs as.
groupadd --system kukatko || true
useradd --system -d /var/lib/kukatko -s /usr/sbin/nologin -g kukatko kukatko || true

# The env file may hold secrets (DB DSN, session secret): lock it down.
chown root:kukatko /etc/kukatko/kukatko.env
chmod 640 /etc/kukatko/kukatko.env

# Data directories: originals are immutable user data, cache is regenerable.
mkdir -p /var/lib/kukatko/originals /var/lib/kukatko/cache
chown -R kukatko:kukatko /var/lib/kukatko

systemctl daemon-reload
systemctl enable kukatko || true

cat <<'EOF'

─────────────────────────────────────────────────────────────
 kukatko installed.

 Next steps:
   1. Edit /etc/kukatko/kukatko.env and set at minimum:
        - KUKATKO_DATABASE_URL
        - KUKATKO_WEB_SESSION_SECRET
      (optionally KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME/PASSWORD)
   2. Start the service:
        sudo systemctl start kukatko
   3. Browse to http://<host>:8080
─────────────────────────────────────────────────────────────

EOF
