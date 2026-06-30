# M7 — Video playback & streaming

Serve videos with HTTP range requests and play them in the app (detail view + posters in the
grid).

## Context
Read `docs/ARCHITECTURE.md` §13 (detail). Depends on video ingest (media_type, poster, duration)
and the photo detail page. Backend serves originals via the photos media endpoints; videos need
**range-request streaming**. react-bootstrap (Superhero), i18n, responsive/touch.

## Requirements
- Backend: video streaming endpoint supporting HTTP `Range` (partial content / 206) so browsers
  can seek without downloading the whole file; correct content-type; memory-bounded (stream from
  disk, no full buffering). Honor auth/download token.
- Optional **on-the-fly transcoding** (config-gated, default off) for non-web-friendly codecs
  (e.g. HEVC/mov) via `ffmpeg` to H.264/mp4 for playback; otherwise serve the original. If off and
  the codec is unplayable, show a download fallback. Document the tradeoff.
- Frontend: video poster tiles in the grid show a play badge + duration; the **detail page** plays
  the video (HTML5 player with controls, keyboard, fullscreen, touch). Live photos: show the still
  with a press-and-hold/hover to play the motion clip.
- Loading/buffering/error states; i18n (cs/en).

## Quality gate (mandatory)
- Use the **golang-developer** skill for backend; `make check` MUST pass (incl. frontend lint/test).
- Integration test: range request returns 206 with correct byte ranges and is memory-bounded;
  transcode path command construction (gated, skip actual transcode if ffmpeg absent in unit
  suite). Vitest: grid play badge + duration rendering, detail player renders and handles
  play/seek/fullscreen, live-photo hover-to-play. Mock API.