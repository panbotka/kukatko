#!/bin/sh
set -e
systemctl daemon-reload || true
if [ "$1" = "purge" ]; then
  # Remove regenerable state on purge, but never originals (user data).
  rm -rf /var/lib/kukatko/cache
  rm -rf /etc/kukatko
  userdel  kukatko 2>/dev/null || true
  groupdel kukatko 2>/dev/null || true
  echo "Note: /var/lib/kukatko/originals/ was preserved. Remove manually if no longer needed."
fi
