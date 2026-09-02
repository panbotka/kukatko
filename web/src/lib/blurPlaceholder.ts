/**
 * The blurred stand-in a photograph paints while its real image is on the way.
 *
 * Every photo the API returns carries a **BlurHash** (`internal/blurhash`): a few
 * dozen characters describing the picture's low frequencies — roughly what you
 * see through frosted glass. Decoding it costs a few thousand additions and needs
 * no network, so a tile can be the colours of its photograph the moment the row
 * arrives, instead of a grey well that fills in seconds later.
 *
 * This module is the whole decoding side of that: a hash in, a `data:` URL out,
 * ready to be handed to `background-image`. Two rules keep it off the scrolling
 * critical path:
 *
 *  - **Tiny.** A BlurHash holds at most a 9×9 grid of frequencies, so decoding it
 *    larger than {@link BLUR_PLACEHOLDER_RESOLUTION} buys no detail whatsoever —
 *    it only spends time, and the time is linear in the pixel count. The result is
 *    stretched to the tile by CSS, which costs the compositor nothing and is
 *    exactly what the blur wants anyway.
 *  - **Decoded once.** The result is memoised by hash, so a tile scrolled out of
 *    the wall and back again is free, and a hash that cannot be painted is
 *    remembered as such rather than retried on every render.
 *
 * A hash that is malformed, or an environment with no 2D canvas, yields
 * `undefined` — the caller then draws whatever neutral surface it drew before.
 * The absence is ordinary: a photo catalogued before placeholders existed carries
 * no hash at all.
 */

import { decode, isBlurhashValid } from 'blurhash'

/**
 * The square, in pixels, a hash is decoded into. Deliberately small: it is the
 * blur's whole resolution, not the tile's, and CSS stretches it to whatever shape
 * the tile turned out to be.
 *
 * Below the common default of 32 on purpose. Decoding is linear in pixels, and
 * measured on this project's Pi 5 (Node 22, the same engine a browser runs) one
 * hash costs 0.168 ms at 16², **0.264 ms at 20²**, 0.381 ms at 24² and 0.658 ms
 * at 32². A first screen of the wall is around forty tiles, so 32² would spend
 * ~26 ms — more than a frame — where 20² spends ~10 ms, and neither the extra
 * detail nor its absence survives being stretched across a 300-pixel tile.
 */
export const BLUR_PLACEHOLDER_RESOLUTION = 20

/**
 * How many decoded placeholders are kept. A scroll through the library passes
 * thousands of photographs and each data URL is a couple of kilobytes; the cache
 * exists to make a tile that comes back into view free, not to remember the whole
 * catalogue, so the oldest entries are dropped once it is full.
 */
export const BLUR_PLACEHOLDER_CACHE_LIMIT = 512

/**
 * Decoded hashes, in insertion order (which is what lets the oldest be evicted).
 * A `null` value is a hash that could not be painted — cached on purpose, so a
 * broken one is decoded once rather than on every render of its tile.
 */
const cache = new Map<string, string | null>()

/**
 * Decodes one hash into a PNG data URL, or `null` when it cannot be painted:
 * a malformed hash, or a browser (or jsdom) that hands out no 2D context.
 */
function paint(hash: string): string | null {
  if (typeof document === 'undefined' || !isBlurhashValid(hash).result) {
    return null
  }
  const size = BLUR_PLACEHOLDER_RESOLUTION
  try {
    const canvas = document.createElement('canvas')
    canvas.width = size
    canvas.height = size
    const context = canvas.getContext('2d')
    if (context === null) {
      return null
    }
    const image = context.createImageData(size, size)
    image.data.set(decode(hash, size, size))
    context.putImageData(image, 0, 0)
    return canvas.toDataURL('image/png')
  } catch {
    // A tainted or unsupported canvas throws rather than returning; a photo
    // without its blur is a cosmetic loss, never a broken page.
    return null
  }
}

/**
 * The `data:` URL of `hash`'s blurred stand-in, or `undefined` when there is
 * nothing to paint — no hash (the backend has not computed one), or one that
 * does not decode. Safe to call on every render: the answer is memoised.
 */
export function blurPlaceholderUrl(hash: string | undefined): string | undefined {
  if (hash === undefined || hash === '') {
    return undefined
  }
  const cached = cache.get(hash)
  if (cached !== undefined) {
    return cached ?? undefined
  }
  const url = paint(hash)
  if (cache.size >= BLUR_PLACEHOLDER_CACHE_LIMIT) {
    // Insertion order: the first key is the least recently *decoded* one. A plain
    // FIFO is enough here — the working set is the handful of screens around the
    // scroll position, and re-decoding an evicted hash costs a millisecond.
    const oldest = cache.keys().next()
    if (!oldest.done) {
      cache.delete(oldest.value)
    }
  }
  cache.set(hash, url)
  return url ?? undefined
}

/**
 * Forgets every decoded placeholder. Only for tests, which need each case to
 * start from an empty cache; nothing in the application invalidates it, because
 * a hash describes a rendering that cannot change without becoming another hash.
 */
export function clearBlurPlaceholderCache(): void {
  cache.clear()
}
