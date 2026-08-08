import type { TFunction } from 'i18next'

import { type YearBucket } from '../services/photos'

import { formatDate } from './format'

/**
 * The library's capture-time period — the one filter on the time axis.
 *
 * It is a pair of **inclusive** calendar-date bounds, either of which may be
 * open (`''`). One representation covers everything readers ask of this axis: a
 * decade (`1960-01-01`…`1969-12-31`), a single year, "before 1950"
 * (`''`…`1949-12-31`) and "summer 2019" (`2019-06-01`…`2019-08-31`). That is why
 * the library has one period control rather than a year picker that cannot
 * express a decade plus a hidden date range that can.
 *
 * The bounds are read as **UTC** calendar days, which is what makes a picked
 * decade return exactly the photos the year facet counted: capture times are
 * stored as the EXIF wall clock read in UTC, and the database session is pinned
 * to UTC too, so `date_part('year', taken_at)` and these bounds agree by
 * construction.
 */
export interface Period {
  /** First day kept, `YYYY-MM-DD`, or `''` for an open start. */
  from: string
  /** Last day kept, `YYYY-MM-DD`, or `''` for an open end. */
  to: string
}

/** The resting period: both ends open, so nothing is filtered out. */
export const ANY_PERIOD: Period = { from: '', to: '' }

/** A calendar day, the only shape a period bound may take. */
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/

/** A four-digit calendar year, the only shape a year value may take. */
const YEAR_PATTERN = /^\d{4}$/

/** Years in a calendar decade — the grain the period picker offers first. */
export const DECADE_YEARS = 10

/**
 * Narrows a raw string to a `YYYY-MM-DD` bound, dropping anything else (a
 * hand-typed or stale URL) to "open" rather than letting the backend answer 400
 * and the grid render an error.
 */
export function toDateBound(raw: string): string {
  return DATE_PATTERN.test(raw) ? raw : ''
}

/** Reports whether a period filters nothing, both of its ends being open. */
export function isAnyPeriod(period: Period): boolean {
  return period.from === '' && period.to === ''
}

/** The first year of the calendar decade a year belongs to (1963 → 1960). */
export function decadeOf(year: number): number {
  return Math.floor(year / DECADE_YEARS) * DECADE_YEARS
}

/** A year as its four-digit text, zero-padded so it sorts and compares as text. */
function yearText(year: number): string {
  return String(year).padStart(4, '0')
}

/**
 * The period covering whole calendar years `from`…`to` inclusive, either end
 * `null` for an open one. `periodForYears(1960, 1969)` is the sixties;
 * `periodForYears(null, 1949)` is "before 1950".
 */
export function periodForYears(from: number | null, to: number | null): Period {
  return {
    from: from === null ? '' : `${yearText(from)}-01-01`,
    to: to === null ? '' : `${yearText(to)}-12-31`,
  }
}

/**
 * The whole-year span a period covers, or `null` when its bounds do not sit on
 * calendar-year boundaries (so "summer 2019" is not mistaken for "2019", which
 * would misreport what the grid shows). An open end stays `null` in the span.
 */
export function yearSpanOf(period: Period): { from: number | null; to: number | null } | null {
  if (isAnyPeriod(period)) {
    return null
  }
  if (period.from !== '' && !period.from.endsWith('-01-01')) {
    return null
  }
  if (period.to !== '' && !period.to.endsWith('-12-31')) {
    return null
  }
  return {
    from: period.from === '' ? null : Number(period.from.slice(0, 4)),
    to: period.to === '' ? null : Number(period.to.slice(0, 4)),
  }
}

/**
 * The upper bound as the list API wants it: the **end** of the chosen day rather
 * than its midnight. The backend compares `taken_at <= taken_before`, so sending
 * the bare date would silently drop everything shot on that day after 00:00:00 —
 * a decade would lose its last New Year's Eve. Microsecond precision is the most
 * a Postgres `timestamptz` keeps, so this is the exact inclusive end of the day.
 *
 * A value that is not a calendar day passes through untouched; the backend
 * rejects it and the caller has already sanitised it via {@link toDateBound}.
 */
export function takenBeforeParam(bound: string): string {
  return DATE_PATTERN.test(bound) ? `${bound}T23:59:59.999999Z` : bound
}

/**
 * Parses a `year:` filter **value** the way the backend's number range does:
 * `1965` (one year), `1960-1969` (a span), `1960-` (from) and `-1949` (until).
 * Anything richer — alternatives (`1960|1970`), a negation, a quoted value — is
 * `null`: it still filters server-side, but it cannot be shown as one period, and
 * a control must not claim a narrower state than the query really sets.
 */
export function parseYearRange(value: string): { from: number | null; to: number | null } | null {
  if (YEAR_PATTERN.test(value)) {
    const year = Number(value)
    return { from: year, to: year }
  }
  if (!/^(\d{4})?-(\d{4})?$/.test(value)) {
    return null
  }
  // The pattern guarantees exactly one dash, so the split is the two ends.
  const [start, end] = value.split('-')
  const from = start === '' ? null : Number(start)
  const to = end === '' ? null : Number(end)
  if (from === null && to === null) {
    return null
  }
  if (from !== null && to !== null && from > to) {
    return null
  }
  return { from, to }
}

/** The filter keys of the query language that scope the capture-time period. */
export const PERIOD_QUERY_KEYS = ['year', 'taken', 'before', 'after'] as const

/**
 * The period a search query itself sets, or `null` when it sets none it can be
 * shown as. This is what lets the period control **derive its displayed state
 * from the query** instead of resting on "any period" over a grid the query has
 * already narrowed to the sixties.
 *
 * Only a lone, plainly-shaped `year:` token qualifies. A second period token —
 * another `year:`, or a `taken:`/`before:`/`after:` — narrows further, and
 * rendering just one of them as *the* period would be a new contradiction rather
 * than a fix; those cases fall back to quoting the tokens verbatim.
 *
 * @param tokens the query's recognised filter tokens, from `queryFilterTokens`.
 */
export function periodFromQuery(tokens: ReadonlyMap<string, string[]>): Period | null {
  const years = tokens.get('year')
  if (years?.length !== 1) {
    return null
  }
  if (tokens.has('taken') || tokens.has('before') || tokens.has('after')) {
    return null
  }
  const raw = years[0]
  const span = parseYearRange(raw.slice(raw.indexOf(':') + 1))
  if (span === null) {
    return null
  }
  return periodForYears(span.from, span.to)
}

/**
 * One calendar decade offered by the period picker, with the years inside it
 * that actually hold photos. The decade is always the full calendar one
 * (`1960`–`1969`), so what it selects never depends on which of its years the
 * library happens to hold today.
 */
export interface DecadeGroup {
  /** First year of the calendar decade, e.g. `1960`. */
  decade: number
  /** The decade's years that hold photos, newest first, each with its count. */
  years: YearBucket[]
  /** Photos held by the whole decade — the sum of its years' counts. */
  count: number
}

/**
 * Groups the years that hold photos into calendar decades, newest decade first
 * and newest year first inside each. Only decades with photos are returned, so
 * the picker offers exactly what the library can answer — the same promise the
 * 109-entry year dropdown made, one grain coarser.
 *
 * The input is sorted defensively: the years endpoint returns them newest first,
 * but the grouping must not depend on that.
 *
 * This is deliberately *not* built on `components/library/timelineRail`'s
 * `buildRail`, the app's other producer of labelled time ranges. That one
 * collapses month buckets by **measured pixel density** — how many ticks fit in
 * the rail — so its ranges shift with the viewport and never align to calendar
 * boundaries. A period offered for picking has to be the opposite: the same
 * thirteen decades whatever the window size, so a shared link means the same
 * thing on a phone.
 */
export function groupYearsIntoDecades(years: YearBucket[]): DecadeGroup[] {
  const groups = new Map<number, DecadeGroup>()
  for (const bucket of [...years].sort((a, b) => b.year - a.year)) {
    const decade = decadeOf(bucket.year)
    const group = groups.get(decade)
    if (group === undefined) {
      groups.set(decade, { decade, years: [bucket], count: bucket.count })
      continue
    }
    group.years.push(bucket)
    group.count += bucket.count
  }
  return [...groups.values()].sort((a, b) => b.decade - a.decade)
}

/**
 * A calendar day as a locale-aware date. The bound is turned into a *local*
 * midnight (`T00:00:00`, no zone) before formatting: parsed as a bare date it
 * would be UTC midnight and would render as the previous day west of Greenwich.
 */
function formatDay(bound: string, locale: string): string {
  return formatDate(`${bound}T00:00:00`, locale)
}

/**
 * The period as the one line the control's trigger and its chip both show:
 *
 * - a whole decade or span:  `"1960–1969"` (en dash, as `formatCaptureRange`
 *   writes an album's span)
 * - one whole year:          `"1965"`
 * - an open end:             `"od 1960"` / `"do 1949"`
 * - anything finer:          `"1. 6. 2019 – 31. 8. 2019"`, in the reader's locale
 *
 * An empty period renders as the resting "any period" wording, so a caller never
 * has to special-case it.
 */
export function formatPeriod(period: Period, t: TFunction, locale: string): string {
  if (isAnyPeriod(period)) {
    return t('library.filters.anyPeriod')
  }
  const span = yearSpanOf(period)
  if (span !== null) {
    const { from, to } = span
    if (from !== null && to !== null) {
      return from === to ? yearText(from) : `${yearText(from)}–${yearText(to)}`
    }
    if (from !== null) {
      return t('library.filters.periodFromYear', { year: yearText(from) })
    }
    if (to !== null) {
      return t('library.filters.periodToYear', { year: yearText(to) })
    }
  }
  if (period.from !== '' && period.to !== '') {
    return `${formatDay(period.from, locale)} – ${formatDay(period.to, locale)}`
  }
  return period.from !== ''
    ? t('library.filters.periodFromDate', { date: formatDay(period.from, locale) })
    : t('library.filters.periodToDate', { date: formatDay(period.to, locale) })
}
