APP_NAME := kukatko
PKG      := github.com/panbotka/kukatko
VERSION  ?= dev
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

# Frontend (Vite + React) lives in web/ and builds into the Go embed directory.
WEB_DIR  := web

# Stamp file marking the last successful `npm ci`. It sits inside the (gitignored)
# node_modules tree and is newer than the lockfile, so `web-deps` is a no-op unless
# the lockfile actually changed. `npm ci` wipes node_modules, taking the stamp with
# it, which is exactly the invalidation we want.
WEB_DEPS_STAMP := $(WEB_DIR)/node_modules/.kukatko-npm-ci-stamp

.PHONY: help fmt fmt-check vet lint lint-fix typecheck test test-race test-integration \
        check build clean docs-budget \
        web-deps web-build web-fmt web-fmt-check web-lint web-test web-typecheck

## help: List available make targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## fmt: Format Go sources (golangci-lint formatters) and the frontend (Prettier).
## This target rewrites files. The quality gate uses the read-only fmt-check instead.
fmt: web-deps
	golangci-lint fmt
	cd $(WEB_DIR) && npm run format

## fmt-check: Verify Go + frontend formatting without touching a single file.
fmt-check: web-deps
	golangci-lint fmt --diff
	cd $(WEB_DIR) && npm run format:check

## vet: Run go vet static analysis. Not part of `check`: .golangci.yml uses
## `default: standard`, so `golangci-lint run` already runs govet.
vet:
	go vet ./...

## lint: Run golangci-lint plus the frontend ESLint (strict).
## Prettier lives in fmt-check so the gate never runs it twice.
lint: web-deps
	golangci-lint run ./...
	cd $(WEB_DIR) && npm run lint

## lint-fix: Run golangci-lint and ESLint applying available autofixes.
lint-fix: web-deps
	golangci-lint run --fix ./...
	cd $(WEB_DIR) && npm run lint -- --fix

## typecheck: Type-check the frontend with the TypeScript compiler (no emit).
typecheck: web-typecheck

## test: Run Go unit tests and the frontend Vitest suite.
## CGO is off and the race detector is disabled so these tests reuse the same Go
## build cache as the production `build` target. Race coverage lives in test-race.
test: web-deps
	CGO_ENABLED=0 go test ./...
	cd $(WEB_DIR) && npm run test

## test-race: Run Go unit tests under the race detector (requires CGO).
## Kept out of `check`: the cgo toolchain recompiles the whole tree, which makes
## the pre-commit gate several minutes slower. CI runs this on every push.
test-race:
	CGO_ENABLED=1 go test -race ./...

## test-integration: Run integration tests (requires KUKATKO_TEST_DATABASE_URL).
## -p 1 serialises package execution: integration packages share one test
## database, so running them concurrently would let one package's TruncateAll
## wipe another's rows mid-test.
test-integration:
	CGO_ENABLED=1 go test -race -p 1 -tags=integration ./...

## check: Quality gate — docs budget, formatting, lint, frontend types, unit tests.
## Non-mutating by construction: a successful run leaves `git status --short` empty.
## Ordered cheapest-first so the fastest check fails fastest.
check: docs-budget fmt-check lint web-typecheck test

## docs-budget: Fail if CLAUDE.md grew beyond its rules+index budget.
docs-budget:
	@lines=$$(wc -l < CLAUDE.md); \
	if [ "$$lines" -gt 300 ]; then \
	  echo "CLAUDE.md má $$lines řádků (limit 300). Detaily patří do docs/."; \
	  echo "Popisné detaily patří do docs/PACKAGES.md, docs/API.md, docs/FRONTEND.md nebo docs/OPERATIONS.md."; \
	  exit 1; \
	fi

## web-deps: Install frontend dependencies from the lockfile (no-op when up to date).
web-deps: $(WEB_DEPS_STAMP)

$(WEB_DEPS_STAMP): $(WEB_DIR)/package-lock.json $(WEB_DIR)/package.json
	cd $(WEB_DIR) && npm ci
	@touch $@

## web-build: Build the frontend into internal/web/static/dist for embedding.
web-build: web-deps
	cd $(WEB_DIR) && npm run build
	printf '' > internal/web/static/dist/.gitkeep

## web-fmt: Format frontend sources with Prettier.
web-fmt: web-deps
	cd $(WEB_DIR) && npm run format

## web-fmt-check: Verify frontend formatting with Prettier (read-only).
web-fmt-check: web-deps
	cd $(WEB_DIR) && npm run format:check

## web-lint: Lint the frontend with ESLint (strict).
web-lint: web-deps
	cd $(WEB_DIR) && npm run lint

## web-typecheck: Type-check the frontend (tsc -b --noEmit).
web-typecheck: web-deps
	cd $(WEB_DIR) && npm run typecheck

## web-test: Run the frontend Vitest suite.
web-test: web-deps
	cd $(WEB_DIR) && npm run test

## build: Build the frontend, then compile the static kukatko binary (CGO off).
## The frontend is built first so go:embed captures the SPA into the binary.
build: web-build
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

## clean: Remove build artifacts (binary, coverage, embedded dist, web build).
clean:
	rm -rf bin/ coverage.out
	rm -rf $(WEB_DIR)/dist $(WEB_DIR)/coverage
	find internal/web/static/dist -mindepth 1 ! -name .gitkeep -delete
