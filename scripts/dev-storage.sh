#!/usr/bin/env bash
#
# Local object store for development — one MinIO container, two consumers.
#
#   ./scripts/dev-storage.sh          # start (or adopt) the container, create the buckets
#   ./scripts/dev-storage.sh --env    # print the .secrets/db.env block and exit
#
# Why this exists: the dev runtime used to point at the production R2 bucket, and
# after that was taken away it ran on the local-disk backend — which meant the S3
# code path (signed keys, PUT/HEAD semantics, listing, the orphan sweep) was not
# exercised anywhere but CI. This container gives development an S3 endpoint of
# its own, so the backend that runs in production is the backend that runs here.
#
# One container serves both the dev runtime (bucket kukatko-dev) and the
# integration tests (kukatko-test*, emptied between cases). It is deliberately
# modest: a single process, a hard memory cap, loopback-only ports outside every
# range this host reserves (5080, 5100-5999, 9000-9999, 12345, 18789 — see
# docs.panbotka.cz/cs/rpi/port-allocation).
#
# The data lives in a NAMED volume, so `docker rm` does not take the library with
# it. The credentials are dev credentials and are not the production ones; they
# are in this file on purpose, because a secret that only ever guards a loopback
# port on the developer's own box is documentation, not a secret.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

CONTAINER="${KUKATKO_MINIO_CONTAINER:-kukatko-minio}"
VOLUME="${KUKATKO_MINIO_VOLUME:-kukatko-minio-data}"
IMAGE="${KUKATKO_MINIO_IMAGE:-quay.io/minio/minio:latest}"
API_PORT="${KUKATKO_MINIO_API_PORT:-18100}"
CONSOLE_PORT="${KUKATKO_MINIO_CONSOLE_PORT:-18101}"
ROOT_USER="${KUKATKO_MINIO_ROOT_USER:-kukatko}"
ROOT_PASSWORD="${KUKATKO_MINIO_ROOT_PASSWORD:-kukatko-dev-secret}"
MEMORY="${KUKATKO_MINIO_MEMORY:-1g}"

# The dev runtime's bucket, then the three the integration tests use:
# internal/storage and internal/storagemigrate share KUKATKO_TEST_S3_BUCKET,
# internal/backup derives a primary/backup pair from it by suffix.
DEV_BUCKET="kukatko-dev"
TEST_BUCKET="kukatko-test"
BUCKETS=("$DEV_BUCKET" "$TEST_BUCKET" "$TEST_BUCKET-primary" "$TEST_BUCKET-backup")

# The container this replaces: created ad hoc for one test run with --restart=no,
# an anonymous volume and a different port. It has been exited for weeks and its
# volume holds nothing anybody wants.
LEGACY_CONTAINER="kukatko-minio-test"

# envBlock prints the environment the dev runtime and the integration tests need,
# in the form .secrets/db.env takes.
envBlock() {
  cat <<ENV
# --- Object storage: LOCAL MinIO (scripts/dev-storage.sh) ---------------------
KUKATKO_STORAGE_BACKEND=r2
KUKATKO_STORAGE_R2_ENDPOINT=http://127.0.0.1:$API_PORT
KUKATKO_STORAGE_R2_REGION=us-east-1
KUKATKO_STORAGE_R2_BUCKET=$DEV_BUCKET
KUKATKO_STORAGE_R2_ACCESS_KEY=$ROOT_USER
KUKATKO_STORAGE_R2_SECRET_KEY=$ROOT_PASSWORD
KUKATKO_STORAGE_CACHE_PATH=$REPO_ROOT/.devdata/cache
KUKATKO_STORAGE_TEMP_PATH=$REPO_ROOT/.devdata/tmp

# Integration tests (same endpoint, their own buckets — emptied between cases).
KUKATKO_TEST_S3_ENDPOINT=http://127.0.0.1:$API_PORT
KUKATKO_TEST_S3_REGION=us-east-1
KUKATKO_TEST_S3_BUCKET=$TEST_BUCKET
KUKATKO_TEST_S3_ACCESS_KEY=$ROOT_USER
KUKATKO_TEST_S3_SECRET_KEY=$ROOT_PASSWORD
ENV
}

if [[ "${1:-}" == "--env" ]]; then
  envBlock
  exit 0
fi
if [[ -n "${1:-}" ]]; then
  echo "dev-storage.sh: unknown flag: $1" >&2
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  echo "dev-storage.sh: docker is not available (is the daemon running?)" >&2
  exit 1
fi

# --- the container ----------------------------------------------------------

# containerExists reports whether a container of the given name is known to
# docker, running or not.
containerExists() {
  docker inspect --type container "$1" >/dev/null 2>&1
}

# durable reports whether the existing container is the one this script would
# create: restarted with the host, and backed by the named volume. A container
# that fails this was made by hand (or by an older version of this script) and is
# recreated — safe precisely because the data is in the volume, not in it.
durable() {
  local policy mounted
  policy=$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$CONTAINER")
  mounted=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$CONTAINER")
  [[ "$policy" == "unless-stopped" && "$mounted" == "$VOLUME" ]]
}

if containerExists "$LEGACY_CONTAINER"; then
  echo "dev-storage.sh: removing the ad-hoc $LEGACY_CONTAINER (anonymous volume, no restart policy)"
  docker rm -f "$LEGACY_CONTAINER" >/dev/null
fi

if containerExists "$CONTAINER" && ! durable; then
  echo "dev-storage.sh: $CONTAINER is not durable (restart policy or volume), recreating it"
  docker rm -f "$CONTAINER" >/dev/null
fi

if containerExists "$CONTAINER"; then
  if [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" == "true" ]]; then
    echo "dev-storage.sh: $CONTAINER already running"
  else
    echo "dev-storage.sh: starting $CONTAINER"
    docker start "$CONTAINER" >/dev/null
  fi
else
  echo "dev-storage.sh: creating $CONTAINER (volume $VOLUME, api :$API_PORT, console :$CONSOLE_PORT)"
  docker volume create "$VOLUME" >/dev/null
  docker run -d \
    --name "$CONTAINER" \
    --restart unless-stopped \
    --memory "$MEMORY" \
    -p "127.0.0.1:$API_PORT:9000" \
    -p "127.0.0.1:$CONSOLE_PORT:9001" \
    -e "MINIO_ROOT_USER=$ROOT_USER" \
    -e "MINIO_ROOT_PASSWORD=$ROOT_PASSWORD" \
    -v "$VOLUME:/data" \
    "$IMAGE" server /data --console-address ":9001" >/dev/null
fi

# --- readiness --------------------------------------------------------------
# A real deadline rather than a probe count: the first start has to format the
# volume, a restart answers in under a second.
deadline=$((SECONDS + 60))
until curl -fsS --connect-timeout 1 --max-time 2 -o /dev/null \
  "http://127.0.0.1:$API_PORT/minio/health/live" 2>/dev/null; do
  if ((SECONDS >= deadline)); then
    echo "dev-storage.sh: MinIO did not become healthy in 60s. Last 20 log lines:" >&2
    docker logs --tail 20 "$CONTAINER" >&2
    exit 1
  fi
  if [[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" != "true" ]]; then
    echo "dev-storage.sh: $CONTAINER died during startup. Last 20 log lines:" >&2
    docker logs --tail 20 "$CONTAINER" >&2
    exit 1
  fi
  sleep 1
done

# --- buckets ----------------------------------------------------------------
# mc ships inside the server image, so this needs no second container. The alias
# lives in the container's own (ephemeral) config; re-setting it every run is
# what makes the script idempotent after a recreation.
docker exec "$CONTAINER" mc alias set local \
  "http://127.0.0.1:9000" "$ROOT_USER" "$ROOT_PASSWORD" >/dev/null
for bucket in "${BUCKETS[@]}"; do
  docker exec "$CONTAINER" mc mb --ignore-existing "local/$bucket" >/dev/null
done
echo "dev-storage.sh: buckets: ${BUCKETS[*]}"

echo "dev-storage.sh: ready — S3 API http://127.0.0.1:$API_PORT, console http://127.0.0.1:$CONSOLE_PORT"
echo "dev-storage.sh: put this in .secrets/db.env (./scripts/dev-storage.sh --env):"
envBlock | sed 's/^/    /'
