import type { TFunction } from 'i18next'

/**
 * Duration arithmetic and compact formatting, used by the slideshow to tell the
 * reader how long a show will run before they start it and how much of it is
 * left while it plays. Kept free of React and of i18next's instance: the split
 * is pure arithmetic and the formatter takes the caller's `t`, so both are
 * directly unit-testable in either language.
 */

const MS_PER_SECOND = 1000
const SECONDS_PER_MINUTE = 60
const MINUTES_PER_HOUR = 60

/** A duration broken into whole hours, minutes and seconds. */
export interface DurationParts {
  hours: number
  /** Minutes within the hour, 0–59. */
  minutes: number
  /** Seconds within the minute, 0–59. */
  seconds: number
}

/**
 * Splits a duration into whole hours, minutes and seconds, rounding to the
 * nearest second. A negative or non-finite input — a count that has not loaded,
 * a tampered setting — collapses to zero rather than producing nonsense parts.
 */
export function splitDuration(ms: number): DurationParts {
  const total = Number.isFinite(ms) && ms > 0 ? Math.round(ms / MS_PER_SECOND) : 0
  return {
    hours: Math.floor(total / (SECONDS_PER_MINUTE * MINUTES_PER_HOUR)),
    minutes: Math.floor(total / SECONDS_PER_MINUTE) % MINUTES_PER_HOUR,
    seconds: total % SECONDS_PER_MINUTE,
  }
}

/**
 * Formats a duration compactly, so it fits on one line beside a button: plain
 * seconds below a minute ("45 s"), minutes and seconds above it ("3 min 20 s"),
 * and hours and minutes once the duration runs that long ("1 h 5 min" — the
 * seconds are dropped, they carry no information at that scale). A part that is
 * zero is omitted: "2 min", not "2 min 0 s".
 */
export function formatDuration(ms: number, t: TFunction): string {
  const { hours, minutes, seconds } = splitDuration(ms)
  if (hours > 0) {
    return minutes === 0
      ? t('duration.hours', { hours })
      : t('duration.hoursMinutes', { hours, minutes })
  }
  if (minutes > 0) {
    return seconds === 0
      ? t('duration.minutes', { minutes })
      : t('duration.minutesSeconds', { minutes, seconds })
  }
  return t('duration.seconds', { seconds })
}

/**
 * How long a slideshow of `count` photos runs at the given auto-advance
 * interval: every photo is shown for exactly one interval.
 */
export function slideshowDurationMs(count: number, intervalMs: number): number {
  return Math.max(0, count) * Math.max(0, intervalMs)
}

/**
 * How much of a slideshow is left once the photo at `index` (0-based) is on
 * screen: the photos still to come, one interval each. The current photo does
 * not count — it is already being shown — so the last slide reports zero.
 * An index past the end (a set that shrank) also reports zero rather than a
 * negative duration.
 */
export function slideshowRemainingMs(index: number, total: number, intervalMs: number): number {
  return slideshowDurationMs(total - index - 1, intervalMs)
}
