# Dev server restart — design

**Date:** 2026-07-09
**Status:** approved

## Problem

Every task that touches Kukátko leaves the running dev server on stale code. There is no
supported way to restart it, and the current `scripts/dev.sh` (untracked) cannot be driven
by Botka: it ends with `exec ./bin/kukatko serve`, so it never returns.

Botka runs a project's `dev_command` as `bash -c "<cmd>"` in the project directory, then
waits for the script to **exit** and looks up the real server PID by scanning `dev_port`
(`internal/handlers/command.go`, `CommandTracker.Run`). A foreground script hangs forever as
a tracked `bash` process and the real server is never discovered.

`make check` also cannot see a whole class of breakage: a missing migration, broken wiring in
`cmd/kukatko`, or a panic during startup all pass lint and unit tests.

## Goals

1. One script that restarts the dev server, usable both by hand and by Botka.
2. Cheap enough to run at the end of every task (no unconditional Vite build).
3. A failed start is a hard failure that blocks the commit.

## Non-goals

- Hot reload / file watching. The single-binary embed mode is deliberate (mirrors
  production); a frontend change requires a rebuild.
- Managing the dev server as a systemd unit.
- Replacing `npm run dev` for frontend-only iteration.

## Design

### `scripts/dev.sh`

Stays under `scripts/`. Botka's `dev_command` is an arbitrary shell string, so
`./scripts/dev.sh` works exactly as well as photo-sorter's root-level `./dev.sh`.

**Contract:** background the server, then exit. Exit `0` means the server is up and answered
`GET /healthz`; exit `1` means it is not, with the last 20 log lines on stdout.

Phases:

1. **Stop.** `pkill -f "$REPO_ROOT/bin/kukatko serve"`. Matching the absolute in-repo path
   rather than a bare `kukatko serve` avoids killing an unrelated instance.
2. **Build, with three independent `find -newer` cache checks:**
   - `npm ci` — skipped when `web/node_modules/.package-lock.json` is newer than
     `web/package-lock.json`.
   - Vite build — skipped when nothing under `web/src`, `web/public`, `web/index.html`,
     `web/vite.config.ts` or `web/tsconfig*.json` is newer than
     `internal/web/static/dist/index.html`. Must also re-create
     `internal/web/static/dist/.gitkeep` afterwards, as `make web-build` does.
   - `go build` — skipped when no `*.go`, `go.mod` or `go.sum` is newer than `bin/kukatko`,
     **and** the Vite build was skipped (a rebuilt SPA changes the embed).

   `--force` bypasses all three.

   The script does **not** call `make build`, because `build → web-build → web-deps` runs
   `npm ci` unconditionally, and `npm ci` wipes and reinstalls `node_modules` on every call.
   It reproduces the same commands with the cache guards in front, including the `-ldflags`
   version/commit injection.
3. **Start.** `./bin/kukatko serve` backgrounded, stdout and stderr into `kukatko.log` at the
   repo root.
4. **Health gate.** Poll `http://localhost:$PORT/healthz` for up to 30 s. Bail out early if
   the process dies. On failure print the last 20 log lines and `exit 1`.

Preserved from the current script: port `${KUKATKO_DEV_PORT:-6480}` (outside the protected
5080 / 5100-5999 / 9xxx / 12345 / 18789 ranges), sourcing `.secrets/db.env` and preferring
`KUKATKO_DATABASE_URL_HOST` (the host runs outside the Docker network, so it needs the
localhost DSN), `CGO_ENABLED=0`, the `.devdata/` storage paths, and the bootstrap admin.

### Botka registration

`dev_command = ./scripts/dev.sh`, `dev_port = 6480`. This gives the run button in Botka's web
UI and makes `mcp__botka__list_commands` report whether the dev server is up.

### CLAUDE.md rule

A new step in *Definition of Done*, between `make check` and the commit:

> **`./scripts/dev.sh`** must pass (the server starts and answers `/healthz`). A failed start
> means do not commit.

Only the rule goes into `CLAUDE.md`; the usage details go into `docs/DEVELOPMENT.md`, per the
existing docs-routing rule enforced by `make docs-budget`.

### Supporting changes

- `.gitignore`: add `/kukatko.log` and `/.devdata/`.
- `scripts/dev.sh` becomes tracked (it is currently untracked).
- `docs/DEVELOPMENT.md`: document the dev loop and the `--force` flag.

## Testing

A shell script has no natural unit-test seam, so it is verified by execution:

1. Clean run: builds, starts, exits `0`, `/healthz` returns `200`.
2. Immediate second run: prints all three "skipping" lines and completes fast.
3. `--force`: rebuilds all three stages.
4. Failure path: point `KUKATKO_DATABASE_URL` at a dead host and confirm the script exits `1`
   and prints the tail of the log rather than hanging for 30 s.

`make check` must stay green.
