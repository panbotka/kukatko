import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import {
  ANY_PERIOD,
  decadeOf,
  formatPeriod,
  groupYearsIntoDecades,
  isAnyPeriod,
  parseYearRange,
  periodForYears,
  periodFromQuery,
  takenBeforeParam,
  toDateBound,
  yearSpanOf,
} from './period'
import { queryFilterTokens } from './queryLanguage'

/**
 * {@link formatPeriod} through the app's own translations, so the assertions are
 * the sentences a reader really sees rather than a stand-in's.
 */
function say(from: string, to: string): string {
  return formatPeriod({ from, to }, i18n.t, 'en')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('period bounds', () => {
  it('accepts only a calendar day as a bound', () => {
    expect(toDateBound('1960-01-01')).toBe('1960-01-01')
    expect(toDateBound('1960-1-1')).toBe('')
    expect(toDateBound('yesterday')).toBe('')
    expect(toDateBound('')).toBe('')
  })

  it('builds whole calendar years, either end open', () => {
    expect(periodForYears(1960, 1969)).toEqual({ from: '1960-01-01', to: '1969-12-31' })
    expect(periodForYears(1965, 1965)).toEqual({ from: '1965-01-01', to: '1965-12-31' })
    expect(periodForYears(null, 1949)).toEqual({ from: '', to: '1949-12-31' })
    expect(periodForYears(1960, null)).toEqual({ from: '1960-01-01', to: '' })
  })

  it('knows when nothing is filtered', () => {
    expect(isAnyPeriod(ANY_PERIOD)).toBe(true)
    expect(isAnyPeriod({ from: '', to: '1949-12-31' })).toBe(false)
  })

  it('reads a whole-year span back, and refuses one that is not whole', () => {
    expect(yearSpanOf({ from: '1960-01-01', to: '1969-12-31' })).toEqual({ from: 1960, to: 1969 })
    expect(yearSpanOf({ from: '', to: '1949-12-31' })).toEqual({ from: null, to: 1949 })
    // "Summer 2019" is not "2019": rounding it up would misreport the grid.
    expect(yearSpanOf({ from: '2019-06-01', to: '2019-08-31' })).toBeNull()
    expect(yearSpanOf(ANY_PERIOD)).toBeNull()
  })

  it('stretches the upper bound to the end of its day', () => {
    // `taken_at <= taken_before`: the bare date would drop every photo shot that
    // day after midnight, so a decade would lose its last New Year's Eve.
    expect(takenBeforeParam('1969-12-31')).toBe('1969-12-31T23:59:59.999999Z')
    expect(takenBeforeParam('')).toBe('')
  })
})

describe('decades', () => {
  it('names the calendar decade a year belongs to', () => {
    expect(decadeOf(1963)).toBe(1960)
    expect(decadeOf(1960)).toBe(1960)
    expect(decadeOf(1969)).toBe(1960)
    expect(decadeOf(2026)).toBe(2020)
  })

  it('groups only the years the library holds, newest first, summing their counts', () => {
    const groups = groupYearsIntoDecades([
      { year: 2023, count: 12 },
      { year: 2021, count: 3 },
      { year: 1967, count: 5 },
      { year: 1963, count: 4 },
    ])

    expect(groups.map((g) => g.decade)).toEqual([2020, 1960])
    expect(groups[0].count).toBe(15)
    expect(groups[1].count).toBe(9)
    // A decade with no photos is never offered — 1970..2010 are simply absent.
    expect(groups).toHaveLength(2)
    expect(groups[1].years.map((y) => y.year)).toEqual([1967, 1963])
  })

  it('does not depend on the order it is handed the years', () => {
    const groups = groupYearsIntoDecades([
      { year: 1963, count: 4 },
      { year: 2023, count: 12 },
      { year: 1967, count: 5 },
    ])
    expect(groups.map((g) => g.decade)).toEqual([2020, 1960])
    expect(groups[1].years.map((y) => y.year)).toEqual([1967, 1963])
  })

  it('offers no decades for an empty library', () => {
    expect(groupYearsIntoDecades([])).toEqual([])
  })
})

describe('parseYearRange', () => {
  it('reads the shapes the backend number range accepts', () => {
    expect(parseYearRange('1965')).toEqual({ from: 1965, to: 1965 })
    expect(parseYearRange('1960-1969')).toEqual({ from: 1960, to: 1969 })
    expect(parseYearRange('1960-')).toEqual({ from: 1960, to: null })
    expect(parseYearRange('-1949')).toEqual({ from: null, to: 1949 })
  })

  it('refuses what it cannot show as one period', () => {
    for (const value of ['1960|1970', '!1965', '19', '', '-', '1969-1960', '1965 ']) {
      expect(parseYearRange(value)).toBeNull()
    }
  })
})

describe('periodFromQuery', () => {
  it('derives the period a lone year: token sets', () => {
    expect(periodFromQuery(queryFilterTokens('year:1960-1969'))).toEqual({
      from: '1960-01-01',
      to: '1969-12-31',
    })
    expect(periodFromQuery(queryFilterTokens('svatba year:1965'))).toEqual({
      from: '1965-01-01',
      to: '1965-12-31',
    })
  })

  it('derives none when the query leaves the time axis alone', () => {
    expect(periodFromQuery(queryFilterTokens('svatba person:Jarmila'))).toBeNull()
  })

  it('derives none when more than one token narrows the period', () => {
    // Two filters AND together; rendering either alone would overstate the grid.
    expect(periodFromQuery(queryFilterTokens('year:1960-1969 before:1965-01-01'))).toBeNull()
    expect(periodFromQuery(queryFilterTokens('year:1960 year:1969'))).toBeNull()
    expect(periodFromQuery(queryFilterTokens('year:1965 taken:1965-06'))).toBeNull()
  })

  it('derives none from a value it cannot show as one span', () => {
    expect(periodFromQuery(queryFilterTokens('year:1960|1970'))).toBeNull()
  })
})

describe('formatPeriod', () => {
  it('says a whole decade as its span, and a whole year as itself', () => {
    expect(say('1960-01-01', '1969-12-31')).toBe('1960–1969')
    expect(say('1965-01-01', '1965-12-31')).toBe('1965')
  })

  it('says an open end in words', () => {
    expect(say('1960-01-01', '')).toBe('from 1960')
    expect(say('', '1949-12-31')).toBe('until 1949')
  })

  it('says a finer period as dates in the reader s locale', () => {
    // Only the shape is pinned: the separators are the host's Intl data. The day
    // matters, though — a bound must never render as the day before, which is
    // what parsing it as UTC midnight would do west of Greenwich.
    expect(say('2019-06-01', '2019-08-31')).toMatch(/^6\D+1\D+2019 – 8\D+31\D+2019$/)
    expect(say('2019-06-01', '')).toMatch(/^from 6\D+1\D+2019$/)
    expect(say('', '2019-08-31')).toMatch(/^until 8\D+31\D+2019$/)
  })

  it('says the resting wording for an empty period', () => {
    expect(say('', '')).toBe('Any period')
  })
})
