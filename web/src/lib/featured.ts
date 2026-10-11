/**
 * Pure helpers for featured albums: whether an album is on the homepage right
 * now, the date the feature dialog offers, and how the end of a feature reads.
 *
 * The dialog picks a *day*, and a feature ending "on 11 December" means through
 * the whole of that day where the curator lives, so a picked day is sent as the
 * last second of that local day.
 */

/** Fallback bounds, for when the server's own could not be read. */
export const FALLBACK_FEATURED_DEFAULT_DAYS = 61
export const FALLBACK_FEATURED_MAX_DAYS = 366

/** Anything carrying the album payload's `featured_until`. */
interface MaybeFeatured {
  featured_until?: string | null
}

/**
 * Whether the album is featured at `now`. The server already sends `null` for
 * a lapsed feature; comparing again here covers a page left open past the end.
 */
export function isFeatured(album: MaybeFeatured, now: number = Date.now()): boolean {
  const until = album.featured_until
  if (until === undefined || until === null || until === '') {
    return false
  }
  const end = Date.parse(until)
  return Number.isFinite(end) && end > now
}

/** Formats a date as a local `YYYY-MM-DD` — the value of an `<input type="date">`. */
export function toDateInputValue(date: Date): string {
  const y = String(date.getFullYear()).padStart(4, '0')
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

/** Returns `date` moved by `days` local calendar days. */
export function addDays(date: Date, days: number): Date {
  const out = new Date(date.getTime())
  out.setDate(out.getDate() + days)
  return out
}

/**
 * The instant a picked day ends: 23:59:59 local time on that day, as an RFC
 * 3339 string the API takes. `undefined` for a value that is not a date.
 */
export function endOfLocalDay(value: string): string | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (match === null) {
    return undefined
  }
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]), 23, 59, 59)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

/** What the feature dialog's date field allows and starts with. */
export interface FeatureDateBounds {
  /** The earliest day (today: a feature may end tonight). */
  min: string
  /** The latest day, one short of the server's cap so its end-of-day fits. */
  max: string
  /** The day the field is pre-filled with. */
  initial: string
}

/**
 * The feature dialog's date bounds. A new feature starts at today +
 * `defaultDays`; extending a running one offers the later of that and the day
 * it currently ends on, so "Prodloužit" never pre-fills a shorter feature.
 */
export function featureDateBounds(
  defaultDays: number,
  maxDays: number,
  now: Date,
  currentUntil?: string | null,
): FeatureDateBounds {
  const latest = addDays(now, Math.max(maxDays - 1, 0))
  let initial = addDays(now, defaultDays)
  if (currentUntil !== undefined && currentUntil !== null) {
    const current = new Date(currentUntil)
    if (!Number.isNaN(current.getTime()) && current > initial) {
      initial = current
    }
  }
  if (initial > latest) {
    initial = latest
  }
  return {
    min: toDateInputValue(now),
    max: toDateInputValue(latest),
    initial: toDateInputValue(initial),
  }
}

/**
 * The end of a feature as the badge reads it: day and month ("11. 12." in
 * Czech, "Dec 11" in English), with the year only when it is not this year's.
 */
export function formatFeaturedUntil(iso: string, locale: string, now: Date = new Date()): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const czech = locale.startsWith('cs')
  const options: Intl.DateTimeFormatOptions = czech
    ? { day: 'numeric', month: 'numeric' }
    : { day: 'numeric', month: 'short' }
  if (date.getFullYear() !== now.getFullYear()) {
    options.year = 'numeric'
  }
  return new Intl.DateTimeFormat(locale, options).format(date)
}
