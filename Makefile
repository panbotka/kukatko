APP_NAME := kukatko
PKG      := github.com/panbotka/kukatko
VERSION  ?= dev
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

# Frontend (Vite + React) lives in web/ and builds into the Go embed directory.
WEB_DIR  := web

.PHONY: help fmt vet lint lint-fix test test-integration check build clean docs-budget \
        web-deps web-build web-fmt web-lint web-test

## help: List available make targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## fmt: Format Go sources (golangci-lint formatters) and the frontend (Prettier).
fmt: web-deps
	golangci-lint fmt
	cd $(WEB_DIR) && npm run format

## vet: Run go vet static analysis.
vet:
	go vet ./...

## lint: Run golangci-lint plus the frontend ESLint (strict) and Prettier check.
lint: web-deps
	golangci-lint run ./...
	cd $(WEB_DIR) && npm run lint && npm run format:check

## lint-fix: Run golangci-lint and ESLint applying available autofixes.
lint-fix: web-deps
	golangci-lint run --fix ./...
	cd $(WEB_DIR) && npm run lint -- --fix

## test: Run Go unit tests (race detector) and the frontend Vitest suite.
## CGO is enabled here only so the race detector works; the production binary
## is still built with CGO_ENABLED=0 (see the build target).
test: web-deps
	CGO_ENABLED=1 go test -race ./...
	cd $(WEB_DIR) && npm run test

## test-integration: Run integration tests (requires KUKATKO_TEST_DATABASE_URL).
## -p 1 serialises package execution: integration packages share one test
## database, so running them concurrently would let one package's TruncateAll
## wipe another's rows mid-test.
test-integration:
	CGO_ENABLED=1 go test -race -p 1 -tags=integration ./...

## check: Full quality gate — docs budget, fmt, vet, lint, and unit tests (Go + frontend).
## docs-budget runs first: it is the cheapest check, so it should fail fastest.
check: docs-budget fmt vet lint test

## docs-budget: Fail if CLAUDE.md grew beyond its rules+index budget.
docs-budget:
	@lines=$$(wc -l < CLAUDE.md); \
	if [ "$$lines" -gt 300 ]; then \
	  echo "CLAUDE.md má $$lines řádků (limit 300). Detaily patří do docs/."; \
	  echo "Popisné detaily patří do docs/PACKAGES.md, docs/API.md, docs/FRONTEND.md nebo docs/OPERATIONS.md."; \
	  exit 1; \
	fi

## web-deps: Install frontend dependencies from the lockfile (idempotent).
web-deps:
	cd $(WEB_DIR) && npm ci

## web-build: Build the frontend into internal/web/static/dist for embedding.
web-build: web-deps
	cd $(WEB_DIR) && npm run build
	printf '' > internal/web/static/dist/.gitkeep

## web-fmt: Format frontend sources with Prettier.
web-fmt: web-deps
	cd $(WEB_DIR) && npm run format

## web-lint: Lint the frontend (ESLint strict + Prettier check).
web-lint: web-deps
	cd $(WEB_DIR) && npm run lint && npm run format:check

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
