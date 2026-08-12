import type { TFunction } from 'i18next'

import { formatDate } from './format'
import { DECADE_YEARS, decadeOf } from './period'

/**
 * How fine a photo's capture date was actually stated, mirroring the backend's
 * `photos.TakenAtPrecision*` (migration 0055). A grain coarser than a day means
 * `taken_at` holds the **first instant of the stated period in UTC** — 1 January
 * for a year, the 1st for a month — so the photo goes on sorting and filtering
 * into that period like any other, while anything that *shows* the date must ask
 * here how much of it may be shown.
 *
 * Without this a shelf of scans dated "somewhere in 1974" would read back as
 * "1 January 1974" in every caption: a day nobody ever claimed, indistinguishable
 * from a real one.
 */
export const TAKEN_PRECISIONS = ['day', 'month', 'year', 'decade'] as const

/** One of {@link TAKEN_PRECISIONS}. */
export type TakenPrecision = (typeof TAKEN_PRECISIONS)[number]

/** The grain of an ordinary date, and what a photo with no stated grain means. */
export const DAY_PRECISION: TakenPrecision = 'day'

/**
 * Whether a stored precision is coarser than a day, i.e. whether the date must
 * be shown as a period rather than as the instant it is anchored at. An unknown
 * or absent value is not coarse: a row that says nothing is an ordinary date.
 */
export function isCoarsePrecision(precision: string | undefined): boolean {
  return precision === 'month' || precision === 'year' || precision === 'decade'
}

/**
 * The stated period of a coarse capture date — `"1974"`, `"červen 1974"`,
 * `"1970–1979"` — or `''` when the date is an ordinary one (or missing), leaving
 * the caller to render it however it renders dates. Callers therefore read as
 * "the period, else my usual date", which is exactly the rule.
 *
 * The instant is read in **UTC**, the zone it was anchored in. Read locally, a
 * photo dated "1974" would be 31 December 1973 for every reader west of
 * Greenwich, and the year facet it sits in — which the backend also derives in
 * UTC — would disagree with the year printed beside it.
 */
export function formatTakenPeriod(
  takenAt: string | undefined,
  precision: string | undefined,
  t: TFunction,
  locale: string,
): string {
  if (takenAt === undefined || takenAt === '' || !isCoarsePrecision(precision)) {
    return ''
  }
  const date = new Date(takenAt)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  if (precision === 'month') {
    return date.toLocaleDateString(locale, { year: 'numeric', month: 'long', timeZone: 'UTC' })
  }
  const year = date.getUTCFullYear()
  if (precision === 'year') {
    return String(year)
  }
  return formatDecade(decadeOf(year), t)
}

/**
 * A decade as the one label used everywhere it is offered or shown: the plain
 * span `"1970–1979"`, in both languages, which is exactly what
 * {@link ./period.formatPeriod} already writes for a decade picked in the period
 * control — so the filter and the caption cannot disagree about how a decade is
 * spelled. Czech has no phrase that works across every decade ("70. léta" is
 * idiomatic, "00. léta" is not), and the span is unambiguous in a caption where
 * the century is not otherwise stated.
 *
 * The years are interpolated as text so no locale's number formatting can turn
 * 1970 into "1 970".
 */
export function formatDecade(decade: number, t: TFunction): string {
  return t('takenDate.decade', {
    from: String(decade),
    to: String(decade + DECADE_YEARS - 1),
  })
}

/** The capture-time fields {@link formatTakenLabel} reads from a photo. */
export interface TakenSource {
  taken_at?: string
  taken_at_precision?: string
  taken_at_estimated?: boolean
}

/**
 * When a photo was taken, in the one form anything showing a date to a reader
 * wants: the stated period when the date is coarse ("1974", "1970–1979"),
 * otherwise the locale's date, and prefixed with the estimate marker ("cca") when
 * the date is a guess rather than a record. `''` when the photo has no capture
 * time at all, which callers render as nothing rather than as a placeholder.
 *
 * The three rules — period before day, estimate marked, absence honest — belong
 * together: a caption that applied two of them would quietly claim a precision or
 * a certainty the catalogue never had.
 */
export function formatTakenLabel(photo: TakenSource, t: TFunction, locale: string): string {
  const period = formatTakenPeriod(photo.taken_at, photo.taken_at_precision, t, locale)
  const date =
    period !== '' ? period : photo.taken_at !== undefined ? formatDate(photo.taken_at, locale) : ''
  if (date === '' || photo.taken_at_estimated !== true) {
    return date
  }
  return `${t('photo.metadata.estimatedMarker')} ${date}`
}
