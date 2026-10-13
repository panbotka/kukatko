import type { RekeyRun, RekeyState } from '../services/rekey'

/**
 * The pure arithmetic of the storage-move progress card: how far a run is, how
 * fast it goes and how long it still has. The rate is measured by the browser
 * from two consecutive polls — the API reports counts, not speed — so the
 * helpers take samples stamped with the client's clock.
 */

/** How often the card polls a running move, in ms. */
export const REKEY_POLL_RUNNING_MS = 3_000

/** How often it polls anything else (paused, failed, no run yet), in ms. */
export const REKEY_POLL_IDLE_MS = 15_000

/**
 * The poll interval for a run in `state` (`null` = no run yet), or `null` when
 * there is nothing left to watch: a done run never changes again.
 */
export function rekeyPollInterval(state: RekeyState | null): number | null {
  if (state === 'done') {
    return null
  }
  return state === 'running' ? REKEY_POLL_RUNNING_MS : REKEY_POLL_IDLE_MS
}

/** True while a run is in progress — running or paused, i.e. not settled. */
export function rekeyInProgress(state: RekeyState): boolean {
  return state === 'running' || state === 'paused'
}

/**
 * The share of photos moved, 0…1. A run with nothing to move is complete; a
 * count past the total (it cannot grow, but never trust arithmetic on the
 * wire) is clamped.
 */
export function rekeyFraction(run: Pick<RekeyRun, 'done_photos' | 'total_photos'>): number {
  if (run.total_photos <= 0) {
    return 1
  }
  return Math.min(Math.max(run.done_photos / run.total_photos, 0), 1)
}

/**
 * The share as a whole percent, rounded down so the bar reads 100 % only when
 * every photo has moved.
 */
export function rekeyPercent(run: Pick<RekeyRun, 'done_photos' | 'total_photos'>): number {
  return Math.floor(rekeyFraction(run) * 100)
}

/** One observation of a run: its moved-photo count and when the browser saw it. */
export interface RekeySample {
  donePhotos: number
  /** Client clock, ms (`Date.now()`). */
  at: number
}

/**
 * The rate in photos per minute between two consecutive polls, or `null` when
 * the pair says nothing: no earlier sample, no time between them, or a count
 * that went down (a reset). A pair with no progress is a rate of 0.
 */
export function rekeyRate(prev: RekeySample | null, next: RekeySample): number | null {
  if (prev === null) {
    return null
  }
  const elapsed = next.at - prev.at
  const moved = next.donePhotos - prev.donePhotos
  if (elapsed <= 0 || moved < 0) {
    return null
  }
  return (moved / elapsed) * 60_000
}

/**
 * The seconds left for `run` at `ratePerMinute`, or `null` when the rate
 * cannot predict anything (unknown or zero). Nothing left is 0.
 */
export function rekeyEtaSeconds(
  run: Pick<RekeyRun, 'done_photos' | 'total_photos'>,
  ratePerMinute: number | null,
): number | null {
  const left = Math.max(0, run.total_photos - run.done_photos)
  if (left === 0) {
    return 0
  }
  if (ratePerMinute === null || ratePerMinute <= 0) {
    return null
  }
  return (left / ratePerMinute) * 60
}

/**
 * Shortens `text` to at most `max` characters by cutting out its middle, so a
 * long object key keeps both its prefix (where it lives) and its file name
 * (what it is): `2019/07/IMG_…_0042.jpg`.
 */
export function truncateMiddle(text: string, max: number): string {
  const chars = Array.from(text)
  if (chars.length <= max) {
    return text
  }
  if (max <= 1) {
    return '…'
  }
  const keep = max - 1
  const head = Math.ceil(keep / 2)
  const tail = keep - head
  return `${chars.slice(0, head).join('')}…${tail > 0 ? chars.slice(-tail).join('') : ''}`
}
