# Make `make check` a single, fast, non-mutating quality gate

`make check` is the gate before every commit, but today it does redundant work, mutates the
working tree, and is slow enough on the Pi that Botka task attempts time out inside it and
commit nothing. Make it run the work exactly once, keep it strict, and let it stay green as
the pre-commit gate.

## Requirements

- `make check` must not modify any file. After a successful run on a clean tree,
  `git status --short` must still be empty. It verifies formatting instead of applying it,
  for both Go and the frontend, and fails when a file is misformatted.
- `make fmt` stays as-is: a separate, explicitly mutating target that applies formatting.
- Drop the redundant `go vet` pass from `check`. `.golangci.yml` uses `default: standard`,
  which already enables `govet`, so `golangci-lint run ./...` covers it. Keep a standalone
  `vet` target if it is useful on its own.
- Unit tests in `check` run with `CGO_ENABLED=0` and without `-race`, so they share the Go
  build cache with the production `build` target instead of recompiling the whole tree
  through the cgo toolchain.
- Move the race detector to its own target (`test-race`, `CGO_ENABLED=1 go test -race ./...`).
  It must still run in CI and remain available locally; it just leaves the pre-commit gate.
- `npm ci` must not reinstall `node_modules` on every invocation. Guard it with a stamp file
  that depends on `web/package-lock.json`, so a run with an unchanged lockfile is a no-op.
- `check` must typecheck the frontend. `web/package.json` already has a `typecheck` script
  (`tsc -b --noEmit`) that no make target invokes, so TypeScript type errors currently pass
  the gate and only surface later during `make build`. Wire it in.
- Do not weaken `.golangci.yml`, the ESLint config, or delete any test. This task changes
  *when and how often* checks run, never *what* they enforce.
- If GitHub Actions invokes these make targets, update the workflow so CI still runs the
  race detector and the integration tests.

## Acceptance criteria

- `make check` on a clean tree exits 0 and leaves `git status --short` empty.
- Running `make check` twice in a row: the second run is substantially faster, and reruns
  neither `npm ci` nor a full Go recompile.
- `make check` fails on each of: a misformatted Go file, a misformatted frontend file, a
  lint violation, a TypeScript type error, a failing Go unit test, a failing Vitest test.
- `make test-race` and `make test-integration` still pass.
- Record the measured wall time of `make check` before and after in `docs/DEVELOPMENT.md`,
  and document the new/changed targets in `docs/OPERATIONS.md`.

## Implementation notes

- Measure first with `time` on each sub-target, so the before/after numbers are real rather
  than estimated. The suspected long poles are `npm ci`, `-race` with `CGO_ENABLED=1` over
  73 Go packages, and the duplicate `go vet` compile.
- `golangci-lint fmt --diff` and the existing `npm run format:check` are the non-mutating
  counterparts of the current `fmt` target.
- The whole task must fit in one ~30 minute Botka attempt. Since the thing being changed is
  the gate itself, run the slow verification in the background and commit once the targeted
  evidence is green rather than waiting serially.
