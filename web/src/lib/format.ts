/** Binary unit suffixes for {@link formatBytes}, ascending by 1024×. */
const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/**
 * Formats a byte count as a short human-readable string using binary (1024)
 * units, e.g. `1536` → `"1.5 KB"`. Negative or non-finite inputs render as
 * `"0 B"`. Bytes show no decimals; larger units show one.
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = unit === 0 ? 0 : 1
  return `${value.toFixed(digits)} ${BYTE_UNITS[unit]}`
}

/**
 * Coerces a timestamp input (ISO string, epoch millis, or `Date`) to a `Date`,
 * returning `null` when the value cannot be parsed into a valid date.
 */
function toDate(value: string | number | Date): Date | null {
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

/**
 * Formats a timestamp as a locale-aware date (no time component) using the
 * given BCP-47 `locale` (e.g. the active i18next language `'cs'`/`'en'`).
 * Invalid inputs render as the original string (or empty for non-strings), so
 * callers never surface a literal `"Invalid Date"`.
 */
export function formatDate(value: string | number | Date, locale: string): string {
  const date = toDate(value)
  if (date === null) {
    return typeof value === 'string' ? value : ''
  }
  return date.toLocaleDateString(locale)
}

/**
 * Formats a timestamp as a locale-aware date and time using the given BCP-47
 * `locale` (e.g. the active i18next language). Invalid inputs render as the
 * original string (or empty for non-strings).
 */
export function formatDateTime(value: string | number | Date, locale: string): string {
  const date = toDate(value)
  if (date === null) {
    return typeof value === 'string' ? value : ''
  }
  return date.toLocaleString(locale)
}

/**
 * Formats a 1-based calendar month (`year`, `month` in 1–12) as a locale-aware
 * short month name plus the year, e.g. `2026, 1, 'en'` → `"Jan 2026"` and
 * `'cs'` → `"led 2026"`. Used by the timeline scrubber to label its month
 * ticks. An out-of-range month (outside 1–12) renders as an empty string so a
 * bad bucket never surfaces a wrong label.
 */
export function formatMonth(year: number, month: number, locale: string): string {
  if (!Number.isInteger(month) || month < 1 || month > 12) {
    return ''
  }
  // Build the date from parts (day 1, local midnight) so the short month name is
  // stable regardless of the host timezone; only the month name is localised,
  // the year is appended verbatim.
  const date = new Date(year, month - 1, 1)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return `${date.toLocaleDateString(locale, { month: 'short' })} ${year}`
}

/**
 * Formats the capture-time span of a collection (an album's `taken_from` /
 * `taken_to`) as a compact, single-line label that widens only as far as it must:
 *
 * - one calendar month: `"6/2007"`
 * - one calendar year:  `"2006"`
 * - several years:      `"1998–1999"` (en dash)
 *
 * A missing or unparseable bound — an album with no photos, or none with a known
 * capture time — renders as an empty string, which the caller drops rather than
 * showing an empty line. The bounds are read in the reader's timezone, the same
 * one every other date in the app is shown in.
 */
export function formatCaptureRange(from?: string, to?: string): string {
  const start = from === undefined ? null : toDate(from)
  const end = to === undefined ? null : toDate(to)
  if (start === null || end === null) {
    return ''
  }
  const startYear = start.getFullYear()
  const endYear = end.getFullYear()
  if (startYear !== endYear) {
    return `${startYear}–${endYear}`
  }
  if (start.getMonth() === end.getMonth()) {
    return `${start.getMonth() + 1}/${startYear}`
  }
  return `${startYear}`
}

/**
 * Formats a duration in milliseconds as a clock string: `M:SS` under an hour
 * (e.g. `154000` → `"2:34"`) and `H:MM:SS` from an hour up (e.g. `3754000` →
 * `"1:02:34"`). Non-finite or non-positive inputs render as `"0:00"`.
 */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '0:00'
  }
  const totalSeconds = Math.round(ms / 1000)
  const seconds = totalSeconds % 60
  const minutes = Math.floor(totalSeconds / 60) % 60
  const hours = Math.floor(totalSeconds / 3600)
  const ss = String(seconds).padStart(2, '0')
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${ss}`
  }
  return `${minutes}:${ss}`
}
