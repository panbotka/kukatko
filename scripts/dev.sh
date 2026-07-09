#!/usr/bin/env bash
#
# Local dev launcher for Kukátko — single-binary (embed) mode.
#
# Builds the frontend into the Go embed dir and compiles one static binary, then
# runs `kukatko serve` so a SINGLE process serves both the API and the embedded
# SPA on one port. This mirrors production exactly (no Vite, no proxy).
#
#   ./scripts/dev.sh           # smart rebuild: skips stages whose inputs are unchanged
#   ./scripts/dev.sh --force   # rebuild everything
#
# Exits 0 once the server answers /healthz, or 1 with the tail of the log if it
# never came up. Backgrounding the server and returning is also what lets Botka
# track it: Botka runs this as the project's dev_command, waits for it to exit,
# then discovers the real server PID on dev_port.
#
# The port lives OUTSIDE the protected production range (5080, 5100-5999,
# 9xxx, 12345, 18789) because this is a throwaway dev instance — see
# docs.panbotka.cz/cs/rpi/port-allocation.
#
#   App (API + SPA): ${KUKATKO_DEV_PORT:-6480}

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PORT="${KUKATKO_DEV_PORT:-6480}"
BINARY="$REPO_ROOT/bin/kukatko"
LOGFILE="$REPO_ROOT/kukatko.log"
DIST_INDEX="$REPO_ROOT/internal/web/static/dist/index.html"

FORCE=false
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
    *)
      echo "dev.sh: unknown flag: $arg" >&2
      exit 2
      ;;
  esac
done

# --- secrets & config -------------------------------------------------------
# db.env carries KUKATKO_DATABASE_URL_HOST (localhost DSN) and MAPY_API_KEY.
if [[ -f .secrets/db.env ]]; then
  # shellcheck disable=SC1091
  source .secrets/db.env
fi

# Running on the host (outside the docker network), so use the localhost DSN.
export KUKATKO_DATABASE_URL="${KUKATKO_DATABASE_URL_HOST:-${KUKATKO_DATABASE_URL:-}}"
if [[ -z "${KUKATKO_DATABASE_URL:-}" ]]; then
  echo "dev.sh: no database URL (set KUKATKO_DATABASE_URL_HOST in .secrets/db.env)" >&2
  exit 1
fi

export CGO_ENABLED=0
export KUKATKO_WEB_PORT="$PORT"
export KUKATKO_STORAGE_ORIGINALS_PATH="${KUKATKO_STORAGE_ORIGINALS_PATH:-$REPO_ROOT/.devdata/originals}"
export KUKATKO_STORAGE_CACHE_PATH="${KUKATKO_STORAGE_CACHE_PATH:-$REPO_ROOT/.devdata/cache}"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME="${KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME:-admin}"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD="${KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD:-admin12345}"
mkdir -p "$KUKATKO_STORAGE_ORIGINALS_PATH" "$KUKATKO_STORAGE_CACHE_PATH"

# --- stop -------------------------------------------------------------------
# Stop before building: `go build -o bin/kukatko` over a running binary fails
# with "text file busy" (ETXTBSY) on Linux.
#
# The pattern is anchored on the absolute path. An unanchored `pkill -f
# "kukatko serve"` also matches any *shell* whose command line merely contains
# that string, which is a good way to kill an innocent bystander.
echo "dev.sh: stopping any running instance…"
pkill -f "^$BINARY serve" 2>/dev/null || true
sleep 1

# --- build (only the stages whose inputs changed) ---------------------------
# Deliberately NOT `make build`: that target depends on web-deps, which runs
# `npm ci` unconditionally — and `npm ci` wipes and reinstalls node_modules on
# every single restart.

need_npm=true
if [[ "$FORCE" == false &&
  -f web/node_modules/.package-lock.json &&
  web/node_modules/.package-lock.json -nt web/package-lock.json ]]; then
  need_npm=false
  echo "dev.sh: dependencies unchanged, skipping npm ci"
fi
if [[ "$need_npm" == true ]]; then
  echo "dev.sh: installing frontend dependencies…"
  (cd web && npm ci)
fi

# A fresh npm ci can change the build output, so it also forces a Vite rebuild.
need_web=true
if [[ "$FORCE" == false && "$need_npm" == false && -f "$DIST_INDEX" ]]; then
  changed=$(find web/src web/public web/index.html web/vite.config.ts \
    web/tsconfig.json web/tsconfig.app.json web/tsconfig.node.json \
    -newer "$DIST_INDEX" 2>/dev/null | head -1)
  if [[ -z "$changed" ]]; then
    need_web=false
    echo "dev.sh: frontend unchanged, skipping build"
  fi
fi
if [[ "$need_web" == true ]]; then
  echo "dev.sh: building frontend…"
  (cd web && npm run build)
  # `make web-build` re-creates this after Vite wipes dist/; keep the embed valid.
  printf '' >internal/web/static/dist/.gitkeep
fi

# A rebuilt SPA changes the embedded assets, so the binary must be rebuilt too.
need_go=true
if [[ "$FORCE" == false && "$need_web" == false && -f "$BINARY" ]]; then
  changed_go=$(find . -name '*.go' -newer "$BINARY" -not -path './web/*' 2>/dev/null | head -1)
  changed_mod=$(find . -maxdepth 1 \( -name go.mod -o -name go.sum \) -newer "$BINARY" 2>/dev/null | head -1)
  if [[ -z "$changed_go" && -z "$changed_mod" ]]; then
    need_go=false
    echo "dev.sh: go code unchanged, skipping build"
  fi
fi
if [[ "$need_go" == true ]]; then
  echo "dev.sh: building binary…"
  commit=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)
  ldflags="-X github.com/panbotka/kukatko/internal/version.Version=dev"
  ldflags+=" -X github.com/panbotka/kukatko/internal/version.Commit=$commit"
  go build -ldflags "$ldflags" -o "$BINARY" ./cmd/kukatko
fi

# --- run --------------------------------------------------------------------
# Started via the absolute path so the stop phase's anchored pkill matches it.
echo "dev.sh: starting kukatko on :$PORT (API + embedded SPA)"
echo "dev.sh: logs: tail -f $LOGFILE"
"$BINARY" serve >"$LOGFILE" 2>&1 &
serve_pid=$!

# --- health gate ------------------------------------------------------------
# A real 30s deadline, not a probe count: curl's own timeouts bound each attempt,
# so a socket that accepts the connection but never answers cannot stall the loop.
deadline=$((SECONDS + 30))
while ((SECONDS < deadline)); do
  if curl -fsS --connect-timeout 1 --max-time 2 -o /dev/null "http://localhost:$PORT/healthz" 2>/dev/null; then
    echo "dev.sh: server ready on :$PORT (pid $serve_pid)"
    exit 0
  fi
  if ! kill -0 "$serve_pid" 2>/dev/null; then
    echo "dev.sh: server died during startup. Last 20 log lines:" >&2
    tail -20 "$LOGFILE" >&2
    exit 1
  fi
  sleep 0.5
done

echo "dev.sh: timed out after 30s waiting for /healthz. Last 20 log lines:" >&2
tail -20 "$LOGFILE" >&2
kill "$serve_pid" 2>/dev/null || true
exit 1
