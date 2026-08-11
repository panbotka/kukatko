# OCR: read the text on a photo and make it searchable

The embeddings sidecar gained `POST /ocr/image` (PP-OCRv5 via RapidOCR, merged in
[image-embeddings#2](https://github.com/kozaktomas/image-embeddings/pull/2)). Run every
photo through it, store the text, and let the library's search find it.

## The endpoint

`POST /ocr/image`, multipart `file` (image/*), optional `min_confidence` (default `0.5`).
Answers `200` with `text` (blocks joined by newlines, reading order), `blocks[]`
(`text`, `bbox` as `[x_min, y_min, x_max, y_max]`, `confidence`), `blocks_count`, `lang`,
`model`. A photo with no text is a normal `200` with an empty result, not an error. Latin
model, Czech diacritics included. `/health` reports an `ocr` section.

Try it by hand: `/tmp/ocr-test.sh <file|URL> [min_confidence]`, `OCR_API` defaults to the
box at `http://100.127.79.1:8000` (wake it with `boxon`; it is often offline).

## Requirements

- Extend `internal/embedding` with the OCR call: multipart `file` + `min_confidence` to
  `/ocr/image`, returning the text and the blocks. Same offline-aware typed errors and
  instrumentation as `/embed/image`; add it to the `Client` interface and the test fake.
- Store the text on `photos` (migration `0057`): the recognised text plus the provenance
  needed to tell "OCR ran and found nothing" from "OCR never ran" (a timestamp and the
  model tag). Bounding boxes are **not** persisted — no consumer yet, keep the schema small.
- Rebuild the generated `fts` tsvector to include the OCR text at the lowest weight (`D`),
  following the drop-and-re-add pattern of `0027_photos_iptc_metadata.sql`. Plain search
  ("veselice") must then find a photo whose only match is a sign in the picture — but rank
  it below a real title. Note in the migration that this rewrites the whole `photos` table.
- New job type `ocr` with its own handler package (mirror `internal/embedjob`): loads the
  photo's `fit_1920` preview via the thumbnailer (generating it if missing), calls the
  sidecar, writes the result. Register it in `cmd/kukatko/jobs.go` with its own worker pool.
- Enqueue an `ocr` job on upload in `internal/ingest`, next to `image_embed`/`face_detect`.
  **Photos only** — videos are skipped, no poster-frame OCR.
- An empty result is a success: record that OCR ran with empty text so the photo is not
  retried on every backfill. A sidecar that is down must behave exactly like `image_embed`
  today — the job waits in the queue, upload and browsing are unaffected.
- New query key `text:` in `internal/query` + its SQL compilation in `internal/photos`,
  matching only the OCR text (accent-insensitive, like the existing `notes:`/`keywords:`
  keys). Mirror it in the frontend: the key registry, the union type and the help entry in
  `web/src/lib/queryLanguage.ts`, plus the cs/en description strings.
- New backfill `POST /process/ocr` (RequireMaintainer), following the siblings in
  `internal/processapi`: by default only photos that were never OCR'd, `?all=true` forces a
  full re-run, answers `{"enqueued": N}`.
- Config under `embedding.ocr.*` (same service, so the URL is reused): an on/off switch,
  `min_confidence` and the preview size, defaulting to `fit_1920`. Add them to the `Config`
  struct, `setDefaults`, `config.example.yaml`, the config tests and `docs/OPERATIONS.md`.
- The metadata sidecar (`internal/sidecarexport`) deliberately does **not** carry the OCR
  text — it is re-derivable, and adding it would trigger a library-wide sidecar rewrite.
- Tests: client against `httptest` (success, empty result, sidecar down), the job handler
  against a fake client, a parser test for `text:`, and an integration test proving a photo
  with OCR text is found both by free text and by `text:` and that a photo without it is not.

## Implementation notes

- The preview size differs from `image_embed` on purpose: `fit_720` is too small for the
  small print on signs and newspapers, `fit_1920` was verified by hand on real photos.
- Do not measure OCR quality against handwriting — no engine here reads Czech cursive, and
  the PR calls it out of scope.
- Throughput on the box is ~4.4 photos/s on CUDA, so a full-library backfill is a long
  queue drain, not a request. The box being offline is the normal case.
- Definition of Done per `CLAUDE.md`: docs first (`PACKAGES.md` + the `## Package map` line,
  `API.md`, `OPERATIONS.md`, `README.md` for the user-visible search change), then
  `make check`, then commit and push.