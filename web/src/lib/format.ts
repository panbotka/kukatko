/** Binary unit suffixes for {@link formatBytes}, ascending by 1024×. */
const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/**
 * Formats a byte count as a short human-readable string using binary (1024)
 * units, e.g. `1536` → `"1.5 KB"`. Negative or non-finite inputs render as
 * `"0 B"`. Bytes show no decimals; larger units show one.
 *
 * Passing the active `locale` (e.g. the i18next language) localises the decimal
 * separator — Czech writes `"1,5 KB"`. Omitting it keeps the plain dot, which is
 * what the callers that render a size inside an otherwise unlocalised technical
 * line already show.
 */
export function formatBytes(bytes: number, locale?: string): string {
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
  const formatted =
    locale === undefined
      ? value.toFixed(digits)
      : new Intl.NumberFormat(locale, {
          minimumFractionDigits: digits,
          maximumFractionDigits: digits,
        }).format(value)
  return `${formatted} ${BYTE_UNITS[unit]}`
}

/**
 * Formats an exact byte count with the locale's thousands grouping, e.g.
 * `3145728` → `"3 145 728 B"` (Czech). It is the precise counterpart of
 * {@link formatBytes}: the detail card shows the rounded, readable size and keeps
 * this one in the tooltip, so the exact number is a hover away without cluttering
 * the row. Negative or non-finite inputs render as `"0 B"`.
 */
export function formatByteCount(bytes: number, locale: string): string {
  const value = Number.isFinite(bytes) && bytes > 0 ? bytes : 0
  return `${new Intl.NumberFormat(locale).format(value)} B`
}

/**
 * Formats a whole count with the locale's thousands grouping, e.g. `20310` →
 * `"20 310"` (Czech) or `"20,310"` (English). Statistics are read at a glance,
 * and an ungrouped five-digit number is read wrong. Non-finite inputs render as
 * `"0"`; fractional inputs are rounded, since every count the app shows is whole.
 */
export function formatCount(value: number, locale: string): string {
  const count = Number.isFinite(value) ? Math.round(value) : 0
  return new Intl.NumberFormat(locale).format(count)
}

/**
 * Formats a `[0,1]` ratio as a locale-aware percentage with at most one decimal,
 * e.g. `0.0025` → `"0,3 %"` (Czech) or `"0.3%"` (English). Used for coverage
 * figures, where a tiny share must still read as tiny rather than round to `0 %`
 * — hence the decimal. Values outside the range are clamped and non-finite
 * inputs render as `"0 %"`, so a malformed number never shows as full coverage.
 */
export function formatPercent(ratio: number, locale: string): string {
  const value = Number.isFinite(ratio) ? Math.min(Math.max(ratio, 0), 1) : 0
  return new Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 1,
  }).format(value)
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
 * The one shape every "when was this taken" label is built from: a numeric date
 * plus the clock to the minute. Held in one place so {@link formatDateTimeMinutes}
 * and {@link formatCaptureParts} can never drift apart — the parts must join back
 * into exactly the string, or a caller that shortens the label would be shortening
 * a different one.
 */
const CAPTURE_FORMAT: Intl.DateTimeFormatOptions = {
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
}

/**
 * The narrow no-break space (U+202F) CLDR writes in front of an English AM/PM.
 * V8 replaces it with a plain space in `format` — and so in every other label the
 * app renders — but hands it through untouched in `formatToParts`, which would
 * leave {@link formatCaptureParts} joining back into a lookalike of
 * {@link formatDateTimeMinutes} rather than into the string itself.
 */
const NARROW_NO_BREAK_SPACE = /\u202f/g

/**
 * The `formatToParts` part types that belong to the clock rather than to the
 * calendar day. `dayPeriod` (AM/PM) and `timeZoneName` are on this side too: they
 * qualify the time, and an English label reading "5:17" without its "PM" would be
 * worse than no time at all.
 */
const TIME_PARTS = new Set<Intl.DateTimeFormatPartTypes>([
  'hour',
  'minute',
  'second',
  'dayPeriod',
  'timeZoneName',
])

/**
 * Formats a timestamp as a locale-aware date and time to the minute, dropping the
 * seconds `toLocaleString` includes by default: "10. 7. 2026 23:03" rather than
 * "10. 7. 2026 23:03:40". Nobody reading when a photo was taken needs the second
 * it was taken on — it is noise in the one line that answers "when was this?" —
 * and the exact stored value is still shown, in the technical details.
 *
 * Invalid inputs render as the original string (or empty for non-strings), like
 * the rest of this module.
 */
export function formatDateTimeMinutes(value: string | number | Date, locale: string): string {
  const date = toDate(value)
  if (date === null) {
    return typeof value === 'string' ? value : ''
  }
  return new Intl.DateTimeFormat(locale, CAPTURE_FORMAT).format(date)
}

/** {@link formatDateTimeMinutes}, cut at the seam between the day and the clock. */
export interface CaptureParts {
  /**
   * The calendar day, e.g. "7. 9. 2019" or "9/7/2019". The part a reader needs —
   * losing the year off the end of it is the failure this split exists to prevent.
   */
  date: string
  /** Whatever the locale writes between the two, e.g. `" "` or `", "`. */
  separator: string
  /** The clock to the minute, e.g. "17:17" or "5:17 PM"; empty when the locale writes none. */
  time: string
}

/**
 * Splits {@link formatDateTimeMinutes} into the calendar day and the clock, so a
 * caller with too little room can drop the time and keep the date rather than let
 * the whole label truncate — which, since the year sits at the end in both of the
 * app's locales, is exactly what an ellipsis eats first.
 *
 * The seam is found through `formatToParts` rather than by splitting on a space:
 * the order and the separator are the locale's business ("7. 9. 2019 17:17" but
 * "9/7/2019, 5:17 PM"), and only the formatter knows which run of characters is
 * the clock. `date + separator + time` is always exactly what
 * {@link formatDateTimeMinutes} returns for the same input.
 *
 * Invalid inputs render as the original string (or empty for non-strings) in
 * `date`, with no time — like the rest of this module.
 */
export function formatCaptureParts(value: string | number | Date, locale: string): CaptureParts {
  const date = toDate(value)
  if (date === null) {
    return { date: typeof value === 'string' ? value : '', separator: '', time: '' }
  }
  const parts = new Intl.DateTimeFormat(locale, CAPTURE_FORMAT).formatToParts(date)
  const join = (from: number, to: number): string =>
    parts
      .slice(from, to)
      .map((part) => part.value)
      .join('')
      .replace(NARROW_NO_BREAK_SPACE, ' ')
  const clock = parts.findIndex((part) => TIME_PARTS.has(part.type))
  // No clock, or a locale that opens with it: there is no seam to cut, so the
  // whole label stays on the side that is never dropped.
  if (clock <= 0) {
    return { date: join(0, parts.length), separator: '', time: '' }
  }
  // The literal in front of the clock is the separator, and it goes with the time:
  // hiding "17:17" must take its leading space with it, or the date keeps a
  // trailing one.
  const seam = parts[clock - 1].type === 'literal' ? clock - 1 : clock
  return { date: join(0, seam), separator: join(seam, clock), time: join(clock, parts.length) }
}

/**
 * Formats a 1-based calendar month (`year`, `month` in 1–12) as a locale-aware
 * short month name plus the year, e.g. `2026, 1, 'en'` → `"Jan 2026"` and
 * `'cs'` → `"led 2026"`. Used by the timeline scrubber to label its month
 * ticks. An out-of-range month (outside 1–12) renders as an empty string so a
 * bad bucket never surfaces a wrong label.
 */
export function formatMonth(year: number, month: number, locale: string): string {
  const name = formatMonthName(year, month, locale)
  return name === '' ? '' : `${name} ${year}`
}

/**
 * Formats a 1-based calendar month (`month` in 1–12) as the locale's short month
 * name alone, e.g. `2026, 1, 'en'` → `"Jan"` and `'cs'` → `"led"`. It is the
 * name half of {@link formatMonth}, exported on its own for a chart axis where
 * the year is already said in the label above it and repeating it on every tick
 * would not fit. An out-of-range month renders as an empty string.
 */
export function formatMonthName(year: number, month: number, locale: string): string {
  if (!Number.isInteger(month) || month < 1 || month > 12) {
    return ''
  }
  // Build the date from parts (day 1, local midnight) so the short month name is
  // stable regardless of the host timezone.
  const date = new Date(year, month - 1, 1)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleDateString(locale, { month: 'short' })
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
