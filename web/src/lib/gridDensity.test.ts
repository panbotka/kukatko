import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  GRID_COLUMN_CHOICES,
  GRID_COLUMNS_MAX,
  GRID_COLUMNS_MIN,
  GRID_DENSITY_DEFAULT,
  gridTemplateColumns,
  initialColumns,
  initialColumnsForWidth,
  readStoredDensity,
  sanitizeDensity,
  stepDensity,
  writeDensity,
} from './gridDensity'

const STORAGE_KEY = 'kukatko.grid.density'

/** Pins the jsdom viewport width so width-derived counts are deterministic. */
function setViewportWidth(px: number): void {
  Object.defineProperty(window, 'innerWidth', { value: px, writable: true, configurable: true })
}

beforeEach(() => {
  window.localStorage.clear()
  setViewportWidth(1024)
})

afterEach(() => {
  window.localStorage.clear()
  setViewportWidth(1024)
})

describe('GRID_COLUMN_CHOICES', () => {
  it('offers every column count from one-per-row to the maximum inclusive', () => {
    expect(GRID_COLUMN_CHOICES).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
    expect(GRID_COLUMN_CHOICES.at(0)).toBe(GRID_COLUMNS_MIN)
    expect(GRID_COLUMNS_MIN).toBe(1)
    expect(GRID_COLUMNS_MAX).toBe(10)
    expect(GRID_COLUMN_CHOICES.at(-1)).toBe(GRID_COLUMNS_MAX)
  })
})

describe('stepDensity', () => {
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

  it('leaves the density untouched for a zero step', () => {
    expect(stepDensity(3, 0)).toBe(3)
  })

  it('snaps a tampered value onto the ladder before stepping', () => {
    // 42 sanitizes to the max, so stepping up clamps and stepping down is 9.
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

  it('clamps out-of-range counts into 1..10', () => {
    expect(sanitizeDensity(0)).toBe(1)
    expect(sanitizeDensity(-4)).toBe(1)
    expect(sanitizeDensity(11)).toBe(10)
    expect(sanitizeDensity(1000)).toBe(10)
  })

  it('rounds a fractional count to the nearest column', () => {
    expect(sanitizeDensity(0.6)).toBe(1)
    expect(sanitizeDensity(1.4)).toBe(1)
    expect(sanitizeDensity(3.4)).toBe(3)
    expect(sanitizeDensity(3.6)).toBe(4)
  })

  it('coerces a legacy "auto" to a concrete count seeded from the viewport width', () => {
    setViewportWidth(1024)
    // 1024px fits seven ~140px tiles, so legacy 'auto' resolves to that count.
    expect(sanitizeDensity('auto')).toBe(initialColumnsForWidth(1024))
    expect(sanitizeDensity('auto')).toBe(7)
  })

  it('coerces anything that is not a finite number to an in-range count', () => {
    for (const bad of [
      '4',
      undefined,
      null,
      Number.NaN,
      Number.POSITIVE_INFINITY,
      { columns: 4 },
    ]) {
      const n = sanitizeDensity(bad)
      expect(Number.isInteger(n)).toBe(true)
      expect(n).toBeGreaterThanOrEqual(GRID_COLUMNS_MIN)
      expect(n).toBeLessThanOrEqual(GRID_COLUMNS_MAX)
    }
  })
})

describe('readStoredDensity', () => {
  it('returns null when nothing is persisted, so a viewport seed can apply', () => {
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

  it('clamps an out-of-range persisted value instead of honouring it', () => {
    window.localStorage.setItem(STORAGE_KEY, '99')
    expect(readStoredDensity()).toBe(GRID_COLUMNS_MAX)
  })

  it('treats corrupt stored JSON as no preference', () => {
    window.localStorage.setItem(STORAGE_KEY, '{not json')
    expect(readStoredDensity()).toBeNull()
  })

  it('treats a legacy "auto" string as no numeric preference (so it is re-seeded)', () => {
    window.localStorage.setItem(STORAGE_KEY, '"auto"')
    expect(readStoredDensity()).toBeNull()
  })

  it('treats a well-formed but nonsensical stored value as no preference', () => {
    window.localStorage.setItem(STORAGE_KEY, '{"columns":4}')
    expect(readStoredDensity()).toBeNull()
  })
})

describe('initialColumnsForWidth', () => {
  it('starts at one photo per row on a very narrow viewport', () => {
    expect(initialColumnsForWidth(200)).toBe(1)
    expect(initialColumnsForWidth(120)).toBe(1)
  })

  it('gives a phone one or two columns', () => {
    expect(initialColumnsForWidth(375)).toBe(2)
  })

  it('fits more tiles as the viewport widens', () => {
    expect(initialColumnsForWidth(768)).toBe(5)
    expect(initialColumnsForWidth(1024)).toBe(7)
  })

  it('caps a very wide viewport at the maximum column count', () => {
    expect(initialColumnsForWidth(1440)).toBe(GRID_COLUMNS_MAX)
    expect(initialColumnsForWidth(4000)).toBe(GRID_COLUMNS_MAX)
  })

  it('falls back to the concrete default when the width is unusable', () => {
    expect(initialColumnsForWidth(0)).toBe(GRID_DENSITY_DEFAULT)
    expect(initialColumnsForWidth(-100)).toBe(GRID_DENSITY_DEFAULT)
    expect(initialColumnsForWidth(Number.NaN)).toBe(GRID_DENSITY_DEFAULT)
  })

  it('reads the current viewport width via initialColumns', () => {
    setViewportWidth(375)
    expect(initialColumns()).toBe(2)
    setViewportWidth(1440)
    expect(initialColumns()).toBe(GRID_COLUMNS_MAX)
  })
})

describe('gridTemplateColumns', () => {
  it('emits exactly the chosen number of equal tracks', () => {
    for (const n of GRID_COLUMN_CHOICES) {
      expect(gridTemplateColumns(n)).toBe(`repeat(${String(n)}, 1fr)`)
    }
  })

  it('collapses to a single full-width column at one photo per row', () => {
    expect(gridTemplateColumns(GRID_COLUMNS_MIN)).toBe('repeat(1, 1fr)')
    expect(gridTemplateColumns(1)).toBe('repeat(1, 1fr)')
  })

  it('clamps an out-of-range count before templating', () => {
    expect(gridTemplateColumns(99)).toBe('repeat(10, 1fr)')
    expect(gridTemplateColumns(0)).toBe('repeat(1, 1fr)')
  })
})
