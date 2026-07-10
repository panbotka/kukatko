# Slideshow prefetch

On a slow connection the next slide is blank for roughly the first second: the show advances to an image the browser has not finished fetching and decoding. A shallow prefetch already exists; deepen it and gate the advance on readiness.

## Requirements

- Preload a window of upcoming photos — default 5 ahead and 1 behind — at the same resolution the slideshow actually displays.
- Before advancing, ensure the next image is fully decoded and ready to paint, not merely fetched. If it is not ready when the interval elapses, hold the current slide and advance the instant it becomes ready.
- Holding must never stall the show forever: after a bounded wait of about 10 seconds, advance regardless.
- Pause, resume and manual next/previous keep working. Manual navigation never waits for decode — it switches immediately.
- Preloaded images are released when leaving the slideshow so memory does not grow across a long show.
- Paging continues to load further photos ahead of the cursor before it reaches the end of the loaded set.

## Edge cases

- An image that fails to load is skipped rather than blocking the show.
- Changing the interval while a hold is in progress does not restart or double the timer.
- A show of a single photo neither holds nor advances.

## Tests

- Unit tests for the advance logic: does not advance while the next image is undecoded, advances as soon as it is ready, advances anyway after the bounded wait, skips an image that failed to load, and does not wait on manual navigation.
