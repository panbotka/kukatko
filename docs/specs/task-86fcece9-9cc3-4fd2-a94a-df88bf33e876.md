# HLS groundwork: object layout, MIME types and lifecycle

Kukátko is gaining HLS video playback. Before any encoding exists, the bucket layout it will use
has to be named in one place, wired into the two systems that sweep the bucket, and the storage
layer has to stop mislabelling playlist and segment files.

This is the first of five HLS tasks. Stay inside it — the encoder, the table and the endpoints
arrive later and will build on what you name here.

## Contract (shared with the follow-up tasks — do not invent your own)

Segments live in the object store under `hls/<file_hash>/<rendition>/`, holding `init.mp4` and
zero-padded five-digit segments `00000.m4s`. Rendition names are lowercase, the first one being
`1080p`. Playlists are **never** stored in the bucket — they are generated per request.

## Requirements

- A new package `internal/hls` owns the layout: the prefix constant, a function building the
  object key from file hash, rendition name and file name, and a validator accepting only
  `init.mp4` or exactly five digits followed by `.m4s`. That validator is the only thing standing
  between a client-supplied path segment and an object key, so it must reject everything else —
  traversal, empty input, wrong padding, a stray extension.
- `internal/storage` returns `application/vnd.apple.mpegurl` for `.m3u8` and `video/iso.segment`
  for `.m4s`. Content sniffing runs first today and would classify a playlist as `text/plain`, so
  these extensions must resolve by extension *before* sniffing.
- `internal/reset` classifies the `hls/` prefix as Kukátko's own rather than foreign, so
  `maintenance reset` wipes it instead of leaving it behind, and counts it in the prefix summary.
- Purging an archived photo deletes its `hls/<file_hash>/` objects, alongside the storyboard
  cleanup that already happens on that path.
- Tests: unit tests for the key builder and the validator including its rejection cases, a MIME
  test per new extension, a reset classification test, and an integration test proving purge
  removes the objects.

## Implementation notes

- `internal/storyboard` and `internal/thumb` already shard by hash, and `internal/reset` already
  classifies `thumb/` and `sidecars/`. This is the same shape — follow it rather than inventing.
- Nothing here runs ffmpeg or touches the database schema. Keep it that way.
- Docs: a line in `docs/PACKAGES.md` and the package map in `CLAUDE.md` for the new package.