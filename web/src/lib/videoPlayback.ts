/**
 * The video player's pure logic: the speeds it offers, where a skip lands, how a
 * position reads on screen, and where in a storyboard sprite the frame for a
 * given moment sits. Kept free of React and of the DOM so the arithmetic the
 * player depends on is directly unit-testable, and so the same rules can be
 * asserted without mounting a `<video>` jsdom cannot play.
 */

/**
 * The playback speeds the player offers, slowest first. `1` is always present —
 * it is the way back to normal — and the set is deliberately short: a menu of
 * fifteen rates is a menu nobody reads.
 */
export const PLAYBACK_RATES = [0.5, 1, 1.25, 1.5, 2] as const

/** One of the offered playback rates. */
export type PlaybackRate = (typeof PLAYBACK_RATES)[number]

/** The rate a player starts at when nothing has been remembered. */
export const DEFAULT_PLAYBACK_RATE: PlaybackRate = 1

/** How far the skip buttons and the J/L keys jump, in seconds. */
export const SKIP_SECONDS = 10

/**
 * sessionStorage key holding the chosen playback rate. Session, not local: the
 * spec is "remembered for the session" — watching a batch of clips at 1.5× is a
 * mood, not a setting, and it should not silently follow the user into next
 * week's visit.
 */
const RATE_STORAGE_KEY = 'kukatko.video.rate'

/**
 * Reports whether value is one of the offered rates. It is the guard between
 * whatever storage hands back — a stale key, a hand-edited value — and the
 * player's state.
 */
export function isPlaybackRate(value: unknown): value is PlaybackRate {
  return typeof value === 'number' && PLAYBACK_RATES.some((rate) => rate === value)
}

/**
 * Reads the playback rate remembered for this session, falling back to
 * {@link DEFAULT_PLAYBACK_RATE} when storage is empty, unavailable (private
 * mode) or holds anything the player does not offer.
 */
export function readPlaybackRate(): PlaybackRate {
  try {
    const raw = window.sessionStorage.getItem(RATE_STORAGE_KEY)
    if (raw === null) {
      return DEFAULT_PLAYBACK_RATE
    }
    const parsed = Number.parseFloat(raw)
    return isPlaybackRate(parsed) ? parsed : DEFAULT_PLAYBACK_RATE
  } catch {
    // Storage unavailable — fall back to the default.
    return DEFAULT_PLAYBACK_RATE
  }
}

/**
 * Remembers the playback rate for the rest of the session. Failures (storage
 * disabled, quota) are swallowed: persistence is best-effort and must never
 * break playback.
 */
export function writePlaybackRate(rate: PlaybackRate): void {
  try {
    window.sessionStorage.setItem(RATE_STORAGE_KEY, String(rate))
  } catch {
    // Best-effort: ignore storage failures.
  }
}

/**
 * The rate one step away from `rate` in the offered list, clamped at both ends
 * so stepping past the fastest (or slowest) is a no-op rather than a wrap-around
 * to the other extreme. `direction` is +1 for faster, -1 for slower.
 */
export function stepPlaybackRate(rate: number, direction: 1 | -1): PlaybackRate {
  const current = PLAYBACK_RATES.findIndex((candidate) => candidate === rate)
  const from = current === -1 ? PLAYBACK_RATES.indexOf(DEFAULT_PLAYBACK_RATE) : current
  const next = Math.min(Math.max(from + direction, 0), PLAYBACK_RATES.length - 1)
  return PLAYBACK_RATES[next]
}

/**
 * Where a seek lands: `from + delta` seconds, held inside `[0, duration]` so a
 * skip near either end saturates instead of running off the timeline. A duration
 * that is not a finite positive number — which is what a `<video>` reports for a
 * stream whose metadata has not loaded — only clamps below zero, because there
 * is no known end to clamp against yet.
 */
export function seekTarget(from: number, delta: number, duration: number): number {
  const target = Math.max((Number.isFinite(from) ? from : 0) + delta, 0)
  if (!Number.isFinite(duration) || duration <= 0) {
    return target
  }
  return Math.min(target, duration)
}

/**
 * Formats a playback position as `m:ss`, or `h:mm:ss` once the clip runs an
 * hour. A non-finite or negative input — an unloaded duration, a seek before the
 * start — reads as `0:00` rather than `NaN:aN`.
 */
export function formatPlaybackTime(seconds: number): string {
  const total = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60) % 60
  const secs = total % 60
  const pad = (value: number) => String(value).padStart(2, '0')
  return hours > 0
    ? `${String(hours)}:${pad(minutes)}:${pad(secs)}`
    : `${String(minutes)}:${pad(secs)}`
}

/**
 * The fraction of a clip a position sits at, as a number in `[0, 1]`. An unknown
 * duration reads as 0 — nothing has played yet as far as the UI can tell — which
 * keeps the scrubber at its start instead of jumping to a random offset.
 */
export function playbackFraction(position: number, duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0 || !Number.isFinite(position)) {
    return 0
  }
  return Math.min(Math.max(position / duration, 0), 1)
}

/**
 * The layout of a video's storyboard sprite, mirroring the backend
 * `storyboard.Spec`: a row-major grid of `count` frames, one every
 * `interval_ms` of playback.
 */
export interface StoryboardSpec {
  columns: number
  rows: number
  count: number
  tile_width: number
  tile_height: number
  interval_ms: number
}

/**
 * Which sprite tile shows the frame at `positionSeconds`, clamped into the grid.
 * It mirrors `storyboard.Spec.TileIndex` on the server: tile i covers
 * `[i·interval, (i+1)·interval)`, and a position past the last tile stays on it
 * rather than reading off the sprite.
 */
export function storyboardTileIndex(spec: StoryboardSpec, positionSeconds: number): number {
  if (spec.interval_ms <= 0 || spec.count <= 0 || !Number.isFinite(positionSeconds)) {
    return 0
  }
  const index = Math.floor((positionSeconds * 1000) / spec.interval_ms)
  return Math.min(Math.max(index, 0), spec.count - 1)
}

/**
 * The CSS that shows one tile of the sprite in a `tile_width × tile_height` box:
 * the sprite as the background, scaled to the whole grid, offset so the wanted
 * tile is the part on screen. Returning a style object (rather than a class per
 * tile) is what lets the preview follow the cursor frame by frame.
 */
export function storyboardTileStyle(
  spec: StoryboardSpec,
  spriteUrl: string,
  index: number,
): {
  width: string
  height: string
  backgroundImage: string
  backgroundSize: string
  backgroundPosition: string
} {
  const column = spec.columns > 0 ? index % spec.columns : 0
  const row = spec.columns > 0 ? Math.floor(index / spec.columns) : 0
  return {
    width: `${String(spec.tile_width)}px`,
    height: `${String(spec.tile_height)}px`,
    backgroundImage: `url(${spriteUrl})`,
    backgroundSize: `${String(spec.columns * spec.tile_width)}px ${String(spec.rows * spec.tile_height)}px`,
    backgroundPosition: `-${String(column * spec.tile_width)}px -${String(row * spec.tile_height)}px`,
  }
}

/**
 * Where the hover preview sits above the timeline: the cursor's x within the
 * track, held far enough from both edges that a `width`-wide preview centred on
 * it stays inside the track instead of spilling out of the player.
 */
export function previewOffset(cursorX: number, trackWidth: number, previewWidth: number): number {
  const half = previewWidth / 2
  if (trackWidth <= previewWidth) {
    return trackWidth / 2
  }
  return Math.min(Math.max(cursorX, half), trackWidth - half)
}

/**
 * The playback position a pointer at `cursorX` over a `trackWidth`-wide timeline
 * points at, in seconds. Coordinates outside the track (a drag that left it)
 * clamp to its ends, so a scrub never seeks past either edge.
 */
export function positionFromPointer(cursorX: number, trackWidth: number, duration: number): number {
  if (trackWidth <= 0 || !Number.isFinite(duration) || duration <= 0) {
    return 0
  }
  const fraction = Math.min(Math.max(cursorX / trackWidth, 0), 1)
  return fraction * duration
}
