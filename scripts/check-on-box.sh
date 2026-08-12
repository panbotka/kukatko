#!/usr/bin/env bash
#
# Run the quality gate on the build box instead of on this Pi.
#
# The Pi has 4 cores and 16 GB shared by every stack running on it; the box
# (`ssh box`) has 24 threads and 62 GB and is idle most of the time. This script
# syncs the current WORKING TREE — uncommitted work included — to the box and
# runs `make check` there, streaming the output back.
#
#   ./scripts/check-on-box.sh      # or: make check-box
#
# It runs the very same `make check` target, never a subset, and exits with the
# remote exit code: a red gate on the box is a red gate here. `make check` stays
# the binding gate; this is only a faster way to execute it.
#
# NOT synced: `.secrets/` (the gate is unit tests only, so it needs no
# credentials, and the box has no business holding this instance's database DSNs
# or API keys), `.git/`, `bin/`, `node_modules/`, and the build outputs.
#
# NOT available remotely: the integration suite. Its database lives on the Pi, so
# `make test-integration` stays a local command.
#
# Environment:
#   KUKATKO_BOX_HOST          ssh target (default `box`; ~/.ssh/config sets the user)
#   KUKATKO_BOX_ROOT          remote cache dir, relative to the remote $HOME
#                             (default `.cache/kukatko-check`)
#   KUKATKO_BOX_SLOTS         how many runs may share the box (default 4)
#   KUKATKO_BOX_WAKE_TIMEOUT  seconds to wait for SSH after the wake packet (default 300)
#   KUKATKO_BOX_LOCK_WAIT     seconds to wait for a busy workspace (default 1800)

set -euo pipefail

HOST="${KUKATKO_BOX_HOST:-box}"
REMOTE_ROOT="${KUKATKO_BOX_ROOT:-.cache/kukatko-check}"
SLOTS="${KUKATKO_BOX_SLOTS:-4}"
WAKE_TIMEOUT="${KUKATKO_BOX_WAKE_TIMEOUT:-300}"
LOCK_WAIT="${KUKATKO_BOX_LOCK_WAIT:-1800}"

log() { echo "check-on-box: $*" >&2; }
die() {
  log "$*"
  exit 1
}

# ---------------------------------------------------------------------------
# Remote half.
#
# The script travels with the tree, so the copy that runs on the box is always
# the copy that was just synced from here — there is no second file that could
# drift out of step. `--remote` is that entry point; nobody calls it by hand.
# ---------------------------------------------------------------------------

# remote_go_arch maps uname to the suffix of the official Go tarball.
remote_go_arch() {
  case "$(uname -m)" in
    x86_64) echo amd64 ;;
    aarch64) echo arm64 ;;
    *) die "unsupported architecture $(uname -m) — no Go tarball to fetch" ;;
  esac
}

# remote_ensure_go installs the Go minor version required by go.mod into
# <toolchain>/go, unless a matching one is already there. Called under the
# bootstrap lock.
remote_ensure_go() {
  local toolchain="$1" want installed ver stage arch

  want="$(awk '/^go [0-9]/ { print $2; exit }' go.mod)"
  [[ -n "$want" ]] || die "no 'go' directive in go.mod"

  # `|| true`: on the first run there is no go to ask, and a failing command
  # substitution takes `set -e` down with it before anything is even reported.
  installed="$("$toolchain/go/bin/go" version 2>/dev/null | awk '{ print $3 }' || true)"
  if [[ "$installed" == "go$want" || "$installed" == "go$want."* ]]; then
    log "Go $installed already installed on the box"
    return 0
  fi

  # go.mod pins a minor version ("go 1.26"), the download needs a full one.
  # go.dev lists newest first, so the first match is the current patch release.
  ver="$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' |
    grep -oE "\"go${want//./\\.}(\.[0-9]+)?\"" | tr -d '"' | head -1 || true)"
  [[ -n "$ver" ]] || die "could not resolve a Go $want release from go.dev/dl"

  arch="$(remote_go_arch)"
  stage="$toolchain/.stage"
  log "installing $ver (linux-$arch) into $toolchain/go — one-time, takes a moment"
  rm -rf "$stage"
  mkdir -p "$stage"
  curl -fsSL "https://go.dev/dl/${ver}.linux-${arch}.tar.gz" -o "$stage/go.tar.gz"
  # The tarball unpacks a top-level `go/`, so extract one level above it.
  rm -rf "$toolchain/go"
  tar -C "$toolchain" -xzf "$stage/go.tar.gz"
  rm -rf "$stage"
}

# remote_ensure_golangci_lint installs the golangci-lint version CI pins into
# <toolchain>/bin, unless it is already there. Called under the bootstrap lock.
remote_ensure_golangci_lint() {
  local toolchain="$1" want installed

  # Single source of truth for the version: the CI workflow. Keeping the gate
  # honest means linting with exactly what CI lints with.
  want="$(awk -F'"' '/GOLANGCI_LINT_VERSION:/ { print $2; exit }' .github/workflows/ci.yml)"
  [[ -n "$want" ]] || die "no GOLANGCI_LINT_VERSION in .github/workflows/ci.yml"

  installed="$("$toolchain/bin/golangci-lint" version 2>/dev/null |
    grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  if [[ "$installed" == "${want#v}" ]]; then
    log "golangci-lint $installed already installed on the box"
    return 0
  fi

  log "installing golangci-lint $want into $toolchain/bin — one-time"
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh |
    sh -s -- -b "$toolchain/bin" "$want"
}

# remote_gate bootstraps the toolchain if needed and runs the gate. It runs on
# the box, in the synced copy of the tree, with <cache> shared by every workspace.
remote_gate() {
  local cache="$1"
  local toolchain="$cache/toolchain"
  mkdir -p "$toolchain/bin"

  # Everything the run downloads lives under the one cache root, so re-runs reuse
  # it and `rm -rf ~/.cache/kukatko-check` is a complete uninstall. The Go module
  # and build caches and npm's cacache are all safe to share between concurrent
  # workspaces; node_modules is not, so it stays inside the workspace.
  export GOPATH="$cache/gopath"
  export GOMODCACHE="$cache/gomodcache"
  export GOCACHE="$cache/gobuild"
  export npm_config_cache="$cache/npm"
  export PATH="$toolchain/go/bin:$toolchain/bin:$PATH"

  # Two workspaces starting at once would both find the toolchain missing and
  # race each other into a half-extracted GOROOT. The loser waits here instead.
  (
    flock 9
    remote_ensure_go "$toolchain"
    remote_ensure_golangci_lint "$toolchain"
  ) 9>"$cache/toolchain.lock"

  # node_modules is excluded from the sync, so a fresh workspace has none. The
  # target is stamped against the lockfile: on every later run this is a no-op.
  [[ -d web/node_modules ]] || log "installing frontend dependencies (npm ci) — one-time per workspace"
  make web-deps

  log "$(go version), node $(node --version), golangci-lint $(golangci-lint version 2>/dev/null | head -1)"
  log "running make check on $(hostname) ($(nproc) threads)"
  make check
}

if [[ "${1:-}" == "--remote" ]]; then
  remote_gate "${2:?--remote needs the cache root}"
  exit 0
fi

# ---------------------------------------------------------------------------
# Local half.
# ---------------------------------------------------------------------------

[[ $# -eq 0 ]] || die "unknown argument: $1 (this script takes none)"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

command -v rsync >/dev/null 2>&1 || die "rsync is not installed"
command -v flock >/dev/null 2>&1 || die "flock is not installed"

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=8)

# --- claim a workspace ------------------------------------------------------
# The Pi hosts several Claude sessions and they run this concurrently, often
# from the very same checkout — so the remote directory cannot be one shared
# path. Each run claims one of KUKATKO_BOX_SLOTS workspaces and holds it for its
# whole life through an open file descriptor, which the kernel releases however
# the script dies.
#
# The lock lives here rather than on the box because every run originates here;
# the workspace name carries this host, so a run started elsewhere gets its own
# set of directories and never collides with ours.
ORIGIN="$(hostname -s)"
LOCK_DIR="${TMPDIR:-/tmp}/kukatko-check-on-box"
mkdir -p "$LOCK_DIR"

slot=""
for ((i = 1; i <= SLOTS; i++)); do
  exec {lock_fd}>"$LOCK_DIR/$ORIGIN-ws$i.lock"
  if flock -n "$lock_fd"; then
    slot="$i"
    break
  fi
  exec {lock_fd}>&-
done
if [[ -z "$slot" ]]; then
  log "all $SLOTS remote workspaces are busy; waiting up to ${LOCK_WAIT}s for one"
  exec {lock_fd}>"$LOCK_DIR/$ORIGIN-ws1.lock"
  flock -w "$LOCK_WAIT" "$lock_fd" ||
    die "no free workspace after ${LOCK_WAIT}s — raise KUKATKO_BOX_SLOTS or run 'make check' locally"
  slot=1
fi

WORKSPACE="$ORIGIN-ws$slot"
REMOTE_SRC="$REMOTE_ROOT/$WORKSPACE/src"

# --- reach the box ----------------------------------------------------------
box_reachable() { ssh "${SSH_OPTS[@]}" "$HOST" true >/dev/null 2>&1; }

if ! box_reachable; then
  if command -v boxon >/dev/null 2>&1; then
    log "$HOST is not answering — sending a wake-on-LAN packet (boxon)"
    boxon >/dev/null 2>&1 || true
  else
    log "$HOST is not answering and boxon is not installed — probing anyway"
  fi
  # Bounded: a box that stays down must fail loudly, never hang. Each probe
  # already carries its own ConnectTimeout, so the loop cannot block either.
  deadline=$((SECONDS + WAKE_TIMEOUT))
  until box_reachable; do
    if ((SECONDS >= deadline)); then
      log "could not reach the build box ($HOST) within ${WAKE_TIMEOUT}s — the gate did NOT run."
      die "run the gate locally instead: make check"
    fi
    sleep 5
  done
  log "$HOST is up"
fi

# --- sync the working tree --------------------------------------------------
# Excluded: secrets (never leave this machine), the history and the build
# outputs (the box rebuilds what it needs), and the caches that must survive
# between runs — with --delete, an excluded path is also protected from deletion,
# which is exactly what keeps the remote node_modules alive.
#
# .gitkeep is re-included ahead of the dist exclusion: `//go:embed all:dist/*` in
# internal/web/static needs a file there or the package does not compile.
RSYNC_FILTERS=(
  --exclude=/.git/
  --exclude=/.secrets/
  --exclude=/bin/
  --exclude=/.devdata/
  --exclude=/.claude/
  --exclude=/.shots/
  --exclude=/kukatko.log
  --exclude=/coverage.out
  --exclude=/web/node_modules/
  --exclude=/web/dist/
  --exclude=/web/coverage/
  --include=/internal/web/static/dist/.gitkeep
  --exclude=/internal/web/static/dist/**
  --exclude=*.local.yaml
  --exclude=*.local.yml
  --exclude=.env
  --exclude=.env.*
)

log "workspace $WORKSPACE on $HOST"
ssh "${SSH_OPTS[@]}" "$HOST" "mkdir -p \"\$HOME/$REMOTE_SRC\"" ||
  die "could not create the remote workspace on $HOST"

rsync -a --delete --info=stats1 -e "ssh ${SSH_OPTS[*]}" \
  "${RSYNC_FILTERS[@]}" ./ "$HOST:$REMOTE_SRC/" ||
  die "syncing the working tree to $HOST failed — the gate did NOT run; use: make check"

# --- run the gate -----------------------------------------------------------
# $HOME is escaped so the remote shell expands it; the workspace path is ours.
started=$SECONDS
status=0
ssh "${SSH_OPTS[@]}" "$HOST" \
  "cd \"\$HOME/$REMOTE_SRC\" && exec ./scripts/check-on-box.sh --remote \"\$HOME/$REMOTE_ROOT\"" ||
  status=$?
elapsed=$((SECONDS - started))

if [[ "$status" -eq 0 ]]; then
  log "gate passed on $HOST in ${elapsed}s (integration tests stay local: make test-integration)"
else
  log "gate FAILED on $HOST after ${elapsed}s (exit $status)"
fi
exit "$status"
