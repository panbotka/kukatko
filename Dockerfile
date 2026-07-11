# syntax=docker/dockerfile:1

# Kukátko container image — multi-stage, producing a small static amd64 image.
# Modeled on the sibling photo-sorter Dockerfile, trimmed to Kukátko's runtime
# needs: there is no LaTeX/font stack here — the only external dependencies are
# the media shell-outs the upload/ingest pipeline uses.

# ---------------------------------------------------------------------------
# Stage 1 — Frontend: build the Vite/React SPA into internal/web/static/dist.
# The Go binary embeds that directory via //go:embed (internal/web/static),
# so it MUST exist before `go build` runs in the backend stage.
# ---------------------------------------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /app
# Install deps against the lockfile first so this layer caches across source edits.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
# vite.config.ts writes to ../internal/web/static/dist (relative to web/).
RUN mkdir -p internal/web/static/dist && cd web && npm run build

# ---------------------------------------------------------------------------
# Stage 2 — Backend: compile the single static, CGO-free Go binary.
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS backend
ENV CGO_ENABLED=0
WORKDIR /app
# Download modules first (cached unless go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The embedded SPA must be present before `go build`, otherwise the
# `//go:embed all:dist/*` directive in internal/web/static fails to compile.
# Overlay the freshly built dist from the frontend stage on top of the source
# tree (which only carries the .gitkeep placeholder).
COPY --from=frontend /app/internal/web/static/dist/ ./internal/web/static/dist/
ARG VERSION=dev
ARG COMMIT_SHA=none
# -s -w strips the symbol table and DWARF debug info to shrink the binary;
# -X injects the build info into internal/version (surfaced by `kukatko version`
# and the /healthz payload). Module path is github.com/panbotka/kukatko.
RUN go build \
      -ldflags "-s -w \
        -X github.com/panbotka/kukatko/internal/version.Version=${VERSION} \
        -X github.com/panbotka/kukatko/internal/version.Commit=${COMMIT_SHA}" \
      -o /kukatko ./cmd/kukatko

# ---------------------------------------------------------------------------
# Stage 3 — Runtime: Alpine plus ONLY the external tools the media pipeline
# actually shells out to. The set is determined from the code, not assumed:
#   - ffmpeg        : internal/video shells out to ffprobe (container/stream
#                     metadata) and ffmpeg (poster frame + optional on-the-fly
#                     transcode). The Alpine `ffmpeg` package ships both.
#   - exiftool      : internal/exif (EXIF read + XMP sidecar) AND internal/imgconvert
#                     RAW handling — RAW originals are decoded by extracting the
#                     camera's embedded JPEG preview via `exiftool -b`. There is
#                     deliberately NO full-demosaic path (see imgconvert/raw.go),
#                     so dcraw/libraw is NOT needed. exiftool is also the ffprobe
#                     metadata fallback in internal/video.
#   - libheif-tools : provides heif-convert for HEIC/HEIF originals (imgconvert).
# libvips is intentionally omitted: thumb.engine defaults to the pure-Go engine
# (internal/thumb), so the container never shells out to vipsthumbnail.
# ---------------------------------------------------------------------------
FROM alpine:3
RUN apk add --no-cache \
        ca-certificates \
        tzdata \
        ffmpeg \
        exiftool \
        libheif-tools \
    && rm -rf /var/cache/apk/*

WORKDIR /app
COPY --from=backend /kukatko /app/kukatko
RUN chown nobody /app/kukatko && chmod 0500 /app/kukatko

# Run unprivileged. The library/cache/temp paths are supplied at runtime as
# mounted volumes owned by this user (see .env.example / docs/OPERATIONS.md).
USER nobody

# Default HTTP listen port (web.port default in internal/config).
EXPOSE 8080

# Deliver a clean SIGTERM so the HTTP server can drain in-flight requests and
# the pgx pool closes cleanly on graceful shutdown.
STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/kukatko"]
CMD ["serve"]
