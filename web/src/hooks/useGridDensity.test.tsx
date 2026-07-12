import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { GRID_COLUMNS_MAX, GRID_COLUMNS_MIN, GRID_DENSITY_DEFAULT } from '../lib/gridDensity'

import { useGridDensity } from './useGridDensity'

const STORAGE_KEY = 'kukatko.grid.density'

/**
 * Points `window.matchMedia` at a fixed narrow/wide answer so the viewport-aware
 * default is deterministic. The shared test setup stubs a non-matching (wide)
 * `matchMedia`; this overrides it for a single test.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
  // Restore the shared setup's wide default so tests never leak a narrow viewport.
  mockViewport(false)
})

describe('useGridDensity', () => {
  it('starts from the responsive default on a wide viewport when nothing is persisted', () => {
    mockViewport(false)
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(GRID_DENSITY_DEFAULT)
  })

  it('defaults to one photo per row on a narrow viewport when nothing is persisted', () => {
    mockViewport(true)
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(GRID_COLUMNS_MIN)
  })

  it('honours a stored preference over the narrow-viewport default', () => {
    mockViewport(true)
    window.localStorage.setItem(STORAGE_KEY, '4')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(4)
  })

  it('honours an explicit auto choice even on a narrow viewport', () => {
    mockViewport(true)
    window.localStorage.setItem(STORAGE_KEY, '"auto"')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe('auto')
  })

  it('round-trips a valid value and survives a remount', () => {
    const first = renderHook(() => useGridDensity())

    act(() => {
      first.result.current.setDensity(6)
    })
    expect(first.result.current.density).toBe(6)

    // A fresh hook (e.g. after a reload) reads the persisted preference back.
    const second = renderHook(() => useGridDensity())
    expect(second.result.current.density).toBe(6)

    // And it really went to localStorage.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('6')
  })

  it('clamps an out-of-range value into 1..8 on the way in', () => {
    const { result } = renderHook(() => useGridDensity())

    act(() => {
      result.current.setDensity(0)
    })
    expect(result.current.density).toBe(GRID_COLUMNS_MIN)

    act(() => {
      result.current.setDensity(42)
    })
    expect(result.current.density).toBe(GRID_COLUMNS_MAX)
  })

  it('pins one photo per row when asked', () => {
    const { result } = renderHook(() => useGridDensity())

    act(() => {
      result.current.setDensity(1)
    })
    expect(result.current.density).toBe(GRID_COLUMNS_MIN)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('1')
  })

  it('clamps an out-of-range value that was already persisted', () => {
    window.localStorage.setItem(STORAGE_KEY, '-3')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(GRID_COLUMNS_MIN)
  })

  it('ignores corrupt stored JSON instead of throwing', () => {
    window.localStorage.setItem(STORAGE_KEY, 'not-json-at-all')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(GRID_DENSITY_DEFAULT)
  })

  it('goes back to the responsive default when the user picks auto', () => {
    const { result } = renderHook(() => useGridDensity())

    act(() => {
      result.current.setDensity(3)
    })
    act(() => {
      result.current.setDensity('auto')
    })
    expect(result.current.density).toBe(GRID_DENSITY_DEFAULT)
  })

  it('re-renders every subscriber, so all grids on the page agree', () => {
    const a = renderHook(() => useGridDensity())
    const b = renderHook(() => useGridDensity())

    act(() => {
      a.result.current.setDensity(7)
    })

    expect(b.result.current.density).toBe(7)
  })
})
