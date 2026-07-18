import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  defaultDensityForViewport,
  GRID_COLUMN_CHOICES,
  GRID_COLUMNS_MAX,
  GRID_COLUMNS_MIN,
  GRID_DENSITY_DEFAULT,
  gridTemplateColumns,
  readStoredDensity,
  sanitizeDensity,
  stepDensity,
  writeDensity,
} from './gridDensity'

const STORAGE_KEY = 'kukatko.grid.density'

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('GRID_COLUMN_CHOICES', () => {
  it('offers every column count from one-per-row to the maximum inclusive', () => {
    expect(GRID_COLUMN_CHOICES).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
    expect(GRID_COLUMN_CHOICES.at(0)).toBe(GRID_COLUMNS_MIN)
    expect(GRID_COLUMNS_MIN).toBe(1)
    expect(GRID_COLUMN_CHOICES.at(-1)).toBe(GRID_COLUMNS_MAX)
  })
})

describe('stepDensity', () => {
  it('enters the pinned range from auto at the smallest multi-column count', () => {
    // Never one-per-row: "more tiles per row" from the responsive default must
    // add columns, not collapse to a single one.
    expect(stepDensity('auto', 1)).toBe(2)
  })

  it('steps up one column at a time and clamps at the maximum', () => {
    expect(stepDensity(4, 1)).toBe(5)
    expect(stepDensity(GRID_COLUMNS_MAX, 1)).toBe(GRID_COLUMNS_MAX)
  })

  it('steps down one column at a time', () => {
    expect(stepDensity(5, -1)).toBe(4)
  })

  it('steps down from two columns to one photo per row', () => {
    expect(stepDensity(2, -1)).toBe(GRID_COLUMNS_MIN)
  })

  it('clamps at one photo per row, the floor', () => {
    expect(stepDensity(GRID_COLUMNS_MIN, -1)).toBe(GRID_COLUMNS_MIN)
  })

  it('clamps at auto when stepping below it', () => {
    expect(stepDensity('auto', -1)).toBe('auto')
  })

  it('leaves the density untouched for a zero step', () => {
    expect(stepDensity(3, 0)).toBe(3)
    expect(stepDensity('auto', 0)).toBe('auto')
  })

  it('snaps a tampered value onto the ladder before stepping', () => {
    // 42 sanitizes to the max, so stepping up clamps and stepping down is 7.
    expect(stepDensity(42, 1)).toBe(GRID_COLUMNS_MAX)
    expect(stepDensity(42, -1)).toBe(GRID_COLUMNS_MAX - 1)
  })
})

describe('sanitizeDensity', () => {
  it('keeps every offered column count untouched', () => {
    for (const n of GRID_COLUMN_CHOICES) {
      expect(sanitizeDensity(n)).toBe(n)
    }
  })

  it('clamps out-of-range counts into 1..8', () => {
    expect(sanitizeDensity(0)).toBe(1)
    expect(sanitizeDensity(-4)).toBe(1)
    expect(sanitizeDensity(9)).toBe(8)
    expect(sanitizeDensity(1000)).toBe(8)
  })

  it('rounds a fractional count to the nearest column', () => {
    expect(sanitizeDensity(0.6)).toBe(1)
    expect(sanitizeDensity(1.4)).toBe(1)
    expect(sanitizeDensity(3.4)).toBe(3)
    expect(sanitizeDensity(3.6)).toBe(4)
  })

  it('falls back to the responsive default for anything that is not a finite number', () => {
    expect(sanitizeDensity('auto')).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity('4')).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity(undefined)).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity(null)).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity(Number.NaN)).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity(Number.POSITIVE_INFINITY)).toBe(GRID_DENSITY_DEFAULT)
    expect(sanitizeDensity({ columns: 4 })).toBe(GRID_DENSITY_DEFAULT)
  })
})

describe('readStoredDensity', () => {
  it('returns null when nothing is persisted, so a viewport default can apply', () => {
    expect(readStoredDensity()).toBeNull()
  })

  it('round-trips a valid value', () => {
    writeDensity(5)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('5')
    expect(readStoredDensity()).toBe(5)
  })

  it('round-trips one photo per row', () => {
    writeDensity(GRID_COLUMNS_MIN)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('1')
    expect(readStoredDensity()).toBe(GRID_COLUMNS_MIN)
  })

  it('round-trips the responsive default as an explicit stored choice', () => {
    writeDensity('auto')
    expect(readStoredDensity()).toBe('auto')
  })

  it('clamps an out-of-range persisted value instead of honouring it', () => {
    window.localStorage.setItem(STORAGE_KEY, '99')
    expect(readStoredDensity()).toBe(GRID_COLUMNS_MAX)
  })

  it('treats corrupt stored JSON as no preference', () => {
    window.localStorage.setItem(STORAGE_KEY, '{not json')
    expect(readStoredDensity()).toBeNull()
  })

  it('treats a well-formed but nonsensical stored value as the responsive default', () => {
    window.localStorage.setItem(STORAGE_KEY, '{"columns":4}')
    expect(readStoredDensity()).toBe(GRID_DENSITY_DEFAULT)
  })
})

describe('defaultDensityForViewport', () => {
  it('starts at one photo per row on a narrow (phone-width) viewport', () => {
    expect(defaultDensityForViewport(true)).toBe(GRID_COLUMNS_MIN)
    expect(defaultDensityForViewport(true)).toBe(1)
  })

  it('keeps the responsive multi-column default on a wide viewport', () => {
    expect(defaultDensityForViewport(false)).toBe(GRID_DENSITY_DEFAULT)
  })
})

describe('gridTemplateColumns', () => {
  it('leaves the template width-driven for the responsive default', () => {
    expect(gridTemplateColumns('auto')).toBe('repeat(auto-fill, minmax(140px, 1fr))')
  })

  it('collapses to a single full-width column at one photo per row', () => {
    expect(gridTemplateColumns(GRID_COLUMNS_MIN)).toBe('minmax(0, 1fr)')
    expect(gridTemplateColumns(1)).toBe('minmax(0, 1fr)')
  })

  it('sizes the tracks so exactly the chosen number of columns fits', () => {
    // 4 columns, 3 gaps of 3px, plus 1px of sub-pixel slack => 10px.
    expect(gridTemplateColumns(4)).toBe(
      'repeat(auto-fill, minmax(max(140px, calc((100% - 10px) / 4)), 1fr))',
    )
  })

  it('accounts for a wider gap', () => {
    // 3 columns, 2 gaps of 8px, plus 1px of slack => 17px.
    expect(gridTemplateColumns(3, 8)).toBe(
      'repeat(auto-fill, minmax(max(140px, calc((100% - 17px) / 3)), 1fr))',
    )
  })

  it('keeps the tile floor so a narrow viewport falls back to fewer columns', () => {
    // The `max()` floor is what stops eight columns from becoming eight stamps on
    // a phone: below the floor the tracks stop shrinking and auto-fill fits fewer.
    // One-per-row is exempt — a single full-width column has nothing to fall back to.
    for (const n of GRID_COLUMN_CHOICES.filter((c) => c > 1)) {
      expect(gridTemplateColumns(n)).toContain('max(140px,')
    }
  })
})
