APP_NAME := kukatko
PKG      := github.com/panbotka/kukatko
VERSION  ?= dev
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

.PHONY: help fmt vet lint lint-fix test test-integration check build clean

## help: List available make targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## fmt: Format Go sources (gofmt + goimports via golangci-lint formatters).
fmt:
	golangci-lint fmt

## vet: Run go vet static analysis.
vet:
	go vet ./...

## lint: Run golangci-lint with the strict project config.
lint:
	golangci-lint run ./...

## lint-fix: Run golangci-lint applying available autofixes.
lint-fix:
	golangci-lint run --fix ./...

## test: Run unit tests with the race detector (integration tests excluded).
## CGO is enabled here only so the race detector works; the production binary
## is still built with CGO_ENABLED=0 (see the build target).
test:
	CGO_ENABLED=1 go test -race ./...

## test-integration: Run integration tests (requires KUKATKO_TEST_DATABASE_URL).
test-integration:
	CGO_ENABLED=1 go test -race -tags=integration ./...

## check: Full quality gate — fmt, vet, lint, and unit tests.
check: fmt vet lint test

## build: Compile the static kukatko binary into bin/ (CGO disabled).
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

## clean: Remove build artifacts.
clean:
	rm -rf bin/ coverage.out
