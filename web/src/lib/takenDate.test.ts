import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { formatDecade, formatTakenLabel, formatTakenPeriod, isCoarsePrecision } from './takenDate'

/**
 * {@link formatTakenPeriod} through the app's own translations, so the
 * assertions are what a reader really sees.
 */
function say(takenAt: string | undefined, precision: string | undefined, locale = 'en'): string {
  return formatTakenPeriod(takenAt, precision, i18n.t, locale)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('isCoarsePrecision', () => {
  it('is true only for a grain coarser than a day', () => {
    expect(isCoarsePrecision('month')).toBe(true)
    expect(isCoarsePrecision('year')).toBe(true)
    expect(isCoarsePrecision('decade')).toBe(true)
    expect(isCoarsePrecision('day')).toBe(false)
  })

  it('treats an absent or unknown grain as an ordinary date', () => {
    // A row that says nothing about its grain is a plain date — the pre-0055
    // meaning, and the safe reading for anything the backend adds later.
    expect(isCoarsePrecision(undefined)).toBe(false)
    expect(isCoarsePrecision('')).toBe(false)
    expect(isCoarsePrecision('century')).toBe(false)
  })
})

describe('formatTakenPeriod', () => {
  it('shows a year-precision date as the bare year, not its 1 January', () => {
    expect(say('1974-01-01T00:00:00Z', 'year')).toBe('1974')
  })

  it('shows a month-precision date as the month and year', () => {
    expect(say('1974-06-01T00:00:00Z', 'month')).toBe('June 1974')
    expect(say('1974-06-01T00:00:00Z', 'month', 'cs')).toBe('červen 1974')
  })

  it('shows a decade-precision date as the whole decade', () => {
    expect(say('1970-01-01T00:00:00Z', 'decade')).toBe('1970–1979')
  })

  it('rounds a decade anchor that is not the decade’s first year', () => {
    // Nothing should write such an anchor, but a period is the honest reading of
    // one that exists — inventing 1974 back out of it would not be.
    expect(say('1974-01-01T00:00:00Z', 'decade')).toBe('1970–1979')
  })

  it('reads the anchor in UTC, so the period never slips a year', () => {
    // The anchor is stored at UTC midnight. Read locally west of Greenwich this
    // is 31 December 1973 — and the year facet, which the backend derives in
    // UTC, would then disagree with the year printed beside the photo.
    expect(say('1974-01-01T00:00:00Z', 'year')).toBe('1974')
    expect(say('1974-01-01T00:00:00Z', 'month')).toBe('January 1974')
  })

  it('says nothing for an ordinary date, leaving the caller to render it', () => {
    expect(say('1974-06-14T10:30:00Z', 'day')).toBe('')
    expect(say('1974-06-14T10:30:00Z', undefined)).toBe('')
  })

  it('says nothing for a missing or unparseable date', () => {
    expect(say(undefined, 'year')).toBe('')
    expect(say('', 'year')).toBe('')
    expect(say('not a date', 'year')).toBe('')
  })
})

describe('formatDecade', () => {
  it('names a decade as the span it covers, in either language', async () => {
    // The same shape `lib/period`'s formatPeriod already writes for a picked
    // decade, so the picker and the caption cannot disagree. Czech has no phrase
    // that works across every decade ("00. léta" is not one), and "1970s" has no
    // Czech equivalent, so the span is spelled out in both.
    expect(formatDecade(1970, i18n.t)).toBe('1970–1979')
    await i18n.changeLanguage('cs')
    expect(formatDecade(1970, i18n.t)).toBe('1970–1979')
  })

  it('does not let number formatting group the years', () => {
    expect(formatDecade(1970, i18n.t)).not.toContain(' ')
  })
})

describe('formatTakenLabel', () => {
  const label = (photo: Parameters<typeof formatTakenLabel>[0]): string =>
    formatTakenLabel(photo, i18n.t, 'en')

  it('shows an ordinary date as the locale writes it', () => {
    expect(label({ taken_at: '1974-06-14T10:30:00Z' })).toBe('6/14/1974')
  })

  it('shows a coarse date as the period it was stated as', () => {
    // Never "1 January 1974" — a day nobody ever claimed.
    expect(label({ taken_at: '1974-01-01T00:00:00Z', taken_at_precision: 'year' })).toBe('1974')
  })

  it('marks an estimate so it cannot be read as a record', () => {
    expect(label({ taken_at: '1950-06-14T00:00:00Z', taken_at_estimated: true })).toBe(
      `${i18n.t('photo.metadata.estimatedMarker')} 6/14/1950`,
    )
  })

  it('says nothing at all for a photo with no capture time', () => {
    expect(label({})).toBe('')
    // Not even the marker: there is nothing to qualify.
    expect(label({ taken_at_estimated: true })).toBe('')
  })
})
