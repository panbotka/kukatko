# Play HLS in the video player

The player uses HLS when the photo has a rendition and falls back to today's behaviour when it
does not.

Depends on the serving task, which exposes the endpoints and the payload field this reads.

## Requirements

- Add hls.js as a bundled dependency. It must ship from the binary like every other asset — no
  CDN reference may survive into the build.
- When a photo reports an available HLS rendition, the player attaches hls.js to the existing
  video element and points it at the master playlist. On a browser with native HLS support, use
  the native path and do not load the library at all.
- When there is no rendition, or the library fails to initialise, the player keeps using the
  current video endpoint, unchanged.
- Everything already built on top of the video element keeps working untouched: the custom
  control bar, the storyboard scrub preview, the keyboard shortcuts, fullscreen and the remembered
  playback rate.
- The download action always points at the original file, never at a rendition. The original stays
  the thing you can keep; the rendition is only for playing.
- A fatal playback error still ends in the existing "cannot play, download instead" state rather
  than a blank player.
- Tests: the player takes the HLS path when a rendition is reported, the native path when the
  browser claims support, and the legacy path otherwise; and the fallback state is reached on a
  fatal error. Mock the library — do not decode real media.

## Implementation notes

- Import the light build; the full one carries DRM and subtitle handling this app has no use for.
- Cross-origin reads from the media CDN are **already live** — kozaktomas/infra#33 was merged and
  applied on 2026-09-08 and verified against production: `photos-cdn.kotrzina.cz` grants
  `access-control-allow-origin` to both `https://fotky.kotrzina.cz` and
  `https://kukatko.kotrzina.cz`, answers preflight with 204, and carries the grant on rejections
  too. Nothing on the CDN side blocks this task.
- Verifying this for real needs a browser against a running instance rather than jsdom; the
  virtualised-grid and viewer work in this repo has established how to do that.
- Docs: `docs/FRONTEND.md` for the player's new behaviour and the dependency, and `README.md` if
  the user-visible playback story changes.