# Dev Server Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Kukátko a `scripts/dev.sh` that rebuilds and restarts the dev server, exits with a
pass/fail status, is driveable by Botka, and is required by the Definition of Done.

**Architecture:** A single bash script does stop → cached build → background start → health
gate, then exits. Exit `0` means the server answered `GET /healthz`; exit `1` prints the log
tail. Botka runs the same script as its `dev_command` and finds the real PID on `dev_port`.

**Tech Stack:** bash, `find -newer` for cache invalidation, `curl` for the health probe,
`pkill` for the stop phase, npm/Vite + `go build` for the build phase.

## Global Constraints

- Dev port: `${KUKATKO_DEV_PORT:-6480}`. Must stay outside the protected ranges (5080,
  5100–5999, 9000–9999, 12345, 18789).
- Health endpoint is `GET /healthz` at the root, **not** under `/api/v1`.
- `CGO_ENABLED=0` for the built binary.
- Go module path: `github.com/panbotka/kukatko`. Version ldflags target
  `internal/version.Version` and `internal/version.Commit`.
- The script must never call `make build`: `build → web-build → web-deps` runs `npm ci`
  unconditionally, and `npm ci` deletes and reinstalls `node_modules` every time.
- The host runs outside the Docker network, so the DSN must come from
  `KUKATKO_DATABASE_URL_HOST` in `.secrets/db.env`.
- `pkill -f` patterns must be anchored with `^` and use the absolute repo path. An unanchored
  pattern matches any shell whose command line merely *contains* the string — verified: a
  `pgrep -af "kukatko serve"` matched an unrelated interactive shell.
- Never commit secrets. `.secrets/` stays gitignored.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `scripts/dev.sh` | Rewrite. Stop, cached build, background start, health gate, exit code. |
| `.gitignore` | Add `/kukatko.log` and `/.devdata/`. |
| `CLAUDE.md` | Add the DoD step; renumber the two that follow. |
| `docs/DEVELOPMENT.md` | Document the dev loop, cache behaviour, `--force`. |
| Botka project config | `dev_command`, `dev_port` (set via MCP, not a file). |

---

### Task 1: Rewrite `scripts/dev.sh`

**Files:**
- Modify: `scripts/dev.sh` (full rewrite; currently untracked and ends with `exec`)

**Interfaces:**
- Consumes: `.secrets/db.env` (`KUKATKO_DATABASE_URL_HOST`, `MAPY_API_KEY`).
- Produces: exit `0` = server healthy on `$PORT`; exit `1` = start failed, log tail printed.
  Log file at `$REPO_ROOT/kukatko.log`. Binary at `$REPO_ROOT/bin/kukatko`.

- [ ] **Step 1: Write the script**

Key ordering decision: **stop before build.** `go build -o bin/kukatko` over a *running*
binary fails on Linux with `text file busy` (ETXTBSY), so the old process must die first.
This is why photo-sorter's `dev.sh` also kills first.

```bash
#!/usr/bin/env bash
#
# Local dev launcher for Kukátko — single-binary (embed) mode.
#
# Rebuilds only what changed, restarts the server in the background, and exits
# once it answers /healthz. Exit 0 = healthy, exit 1 = failed to start.
#
#   ./scripts/dev.sh           # smart rebuild (skips unchanged stages)
#   ./scripts/dev.sh --force   # rebuild everything
#
# Botka runs this as the project's dev_command: it must background the server
# and RETURN, so Botka can discover the real PID on dev_port.

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
    *) echo "dev.sh: unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# --- secrets & config -------------------------------------------------------
if [[ -f .secrets/db.env ]]; then
  # shellcheck disable=SC1091
  source .secrets/db.env
fi

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
# Anchored on the absolute path: an unanchored `pkill -f "kukatko serve"` also
# matches any shell whose command line contains that string.
echo "dev.sh: stopping any running instance…"
pkill -f "^$BINARY serve" 2>/dev/null || true
sleep 1

# --- build (only what changed) ----------------------------------------------
# Deliberately NOT `make build`: that target depends on web-deps, which runs
# `npm ci` unconditionally (wiping node_modules on every restart).

need_npm=true
if [[ "$FORCE" == false \
      && -f web/node_modules/.package-lock.json \
      && web/node_modules/.package-lock.json -nt web/package-lock.json ]]; then
  need_npm=false
  echo "dev.sh: dependencies unchanged, skipping npm ci"
fi
if [[ "$need_npm" == true ]]; then
  echo "dev.sh: installing frontend dependencies…"
  (cd web && npm ci)
fi

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
  printf '' > internal/web/static/dist/.gitkeep
fi

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

# --- start ------------------------------------------------------------------
# Absolute path so the stop phase's anchored pkill pattern matches next time.
echo "dev.sh: starting kukatko on :$PORT (API + embedded SPA)"
echo "dev.sh: logs: tail -f $LOGFILE"
"$BINARY" serve > "$LOGFILE" 2>&1 &
serve_pid=$!

# --- health gate ------------------------------------------------------------
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://localhost:$PORT/healthz" 2>/dev/null; then
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
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x scripts/dev.sh
```

- [ ] **Step 3: Clean run — must build, start, exit 0**

```bash
./scripts/dev.sh; echo "exit=$?"
```

Expected: builds (all three stages run on a cold cache), then
`dev.sh: server ready on :6480 (pid …)` and `exit=0`.

- [ ] **Step 4: Verify the server actually answers**

```bash
curl -fsS http://localhost:6480/healthz; echo
```

Expected: `{"status":"ok","version":{"version":"dev","commit":"…"}}`

- [ ] **Step 5: Second run — must skip all three stages**

```bash
time ./scripts/dev.sh; echo "exit=$?"
```

Expected: three lines — `skipping npm ci`, `skipping build`, `skipping build` (go) — then
`server ready`, `exit=0`, and a runtime of a few seconds (no Vite build).

- [ ] **Step 6: `--force` — must rebuild everything**

```bash
./scripts/dev.sh --force; echo "exit=$?"
```

Expected: no `skipping` lines; npm ci, Vite build, and go build all run. `exit=0`.

- [ ] **Step 7: Failure path — must exit 1, not hang**

```bash
KUKATKO_DATABASE_URL_HOST="postgres://nobody:nobody@127.0.0.1:1/nodb" ./scripts/dev.sh; echo "exit=$?"
```

Expected: `dev.sh: server died during startup.` plus the log tail on stderr, and `exit=1`.
If the server instead hangs for 30 s, the connection failure is not fatal at startup — note
it and keep the timeout branch as the safety net.

- [ ] **Step 8: Confirm the anchored pkill only kills the server**

```bash
./scripts/dev.sh >/dev/null && pgrep -f "^$PWD/bin/kukatko serve"
```

Expected: exactly one PID. The script's own shell must not appear.

---

### Task 2: Ignore the dev artifacts

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Add the two entries**

Under the existing `# Data` section:

```gitignore
# Data
/data/
*.sqlite
/.devdata/
/kukatko.log
```

- [ ] **Step 2: Verify nothing untracked leaks**

```bash
git status --porcelain
```

Expected: `.devdata/` and `kukatko.log` absent; `scripts/dev.sh` present as untracked.

---

### Task 3: Wire the rule into the Definition of Done

**Files:**
- Modify: `CLAUDE.md` (`## Definition of Done` section)
- Modify: `docs/DEVELOPMENT.md`

- [ ] **Step 1: Insert the DoD step**

The section currently numbers 1 (docs), 2 (`make check`), 3 (commit). Insert the restart
between `make check` and the commit, renumbering the commit step to 4:

```markdown
3. **`./scripts/dev.sh`** musí projít — server nastartuje a odpoví na `/healthz`.
   Zachytí, co `make check` nevidí: chybějící migraci, rozbité wiring v `cmd/kukatko`,
   panic při startu. Neúspěšný start = **necommituj**.
```

- [ ] **Step 2: Check the docs budget still passes**

```bash
make docs-budget
```

Expected: no output, exit 0 (CLAUDE.md stays under 300 lines).

- [ ] **Step 3: Document the script in `docs/DEVELOPMENT.md`**

Add a `## Dev server` section after `## CLI`:

````markdown
## Dev server

`scripts/dev.sh` rebuilds and restarts the local dev server in single-binary (embed) mode —
one process serving the API and the embedded SPA on one port, exactly as in production.

```bash
./scripts/dev.sh          # smart rebuild: skips npm ci / Vite / go build when nothing changed
./scripts/dev.sh --force  # rebuild all three stages
```

It stops any running instance, rebuilds only what changed, starts the server in the
background on `${KUKATKO_DEV_PORT:-6480}`, and waits for `GET /healthz`. It exits `0` once
the server is healthy, or `1` with the tail of `kukatko.log` if it never came up — so it
works as a gate before committing (see the Definition of Done in
[`CLAUDE.md`](../CLAUDE.md)) and as Botka's `dev_command`.

The DSN comes from `KUKATKO_DATABASE_URL_HOST` in the gitignored `.secrets/db.env`: the
script runs on the host, outside the Docker network, so it needs the localhost DSN. Uploads
and thumbnails land in the gitignored `.devdata/`.

For frontend-only iteration, `cd web && npm run dev` is still faster — the Vite dev server
proxies `/healthz` and `/api` to the backend.
````

- [ ] **Step 4: Full quality gate**

```bash
make check
```

Expected: PASS.

---

### Task 4: Register the script with Botka

**Files:** none (Botka project config, set over MCP)

- [ ] **Step 1: Set `dev_command` and `dev_port`**

Call `mcp__botka__update_project` with `project_name: kukatko`,
`dev_command: ./scripts/dev.sh`, `dev_port: 6480`.

Botka runs this as `bash -c "./scripts/dev.sh"` with cwd set to the project path, waits for
it to exit, then scans port 6480 for the real server PID
(`botka/internal/handlers/command.go`, `CommandTracker.Run`). This is exactly why the script
must background the server and return.

- [ ] **Step 2: Verify Botka sees the running server**

Call `mcp__botka__list_commands` with `project_name: kukatko`.

Expected: one `dev` command listed, with the PID of `bin/kukatko serve` (not a `bash` PID)
and port 6480.

- [ ] **Step 3: Commit everything**

```bash
git add scripts/dev.sh .gitignore CLAUDE.md docs/DEVELOPMENT.md \
        docs/superpowers/specs/2026-07-09-dev-server-restart-design.md \
        docs/superpowers/plans/2026-07-09-dev-server-restart.md
git commit -m "$(cat <<'EOF'
feat(dev): add restartable dev server script and require it before commit

scripts/dev.sh stops any running instance, rebuilds only the stages whose
inputs changed, starts the server in the background, and exits once it
answers /healthz. Exit 1 with the log tail if it never comes up.

Backgrounding and returning is what lets Botka track it: Botka runs the
dev_command, waits for it to exit, then finds the real PID on dev_port.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
EOF
)"
git push
```
