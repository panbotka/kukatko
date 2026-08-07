import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  GRID_COLUMNS_MAX,
  GRID_COLUMNS_MIN,
  initialColumnsForWidth,
  LIBRARY_GRID_SCOPE,
  OUTLIER_GRID_SCOPE,
} from '../lib/gridDensity'

import { useGridDensity } from './useGridDensity'

const STORAGE_KEY = 'kukatko.grid.density'

/** Pins the jsdom viewport width so the width-derived seed is deterministic. */
function setViewportWidth(px: number): void {
  Object.defineProperty(window, 'innerWidth', { value: px, writable: true, configurable: true })
}

/** Resizes the jsdom viewport the way a browser would: width + a `resize` event. */
function resizeTo(px: number): void {
  act(() => {
    setViewportWidth(px)
    window.dispatchEvent(new Event('resize'))
  })
}

beforeEach(() => {
  window.localStorage.clear()
  setViewportWidth(1024)
})

afterEach(() => {
  window.localStorage.clear()
  setViewportWidth(1024)
})

describe('useGridDensity', () => {
  it('seeds a concrete count from the viewport width and persists it on first use', () => {
    setViewportWidth(1024)
    const { result } = renderHook(() => useGridDensity())

    // A wide viewport fits several tiles; the effective density is that concrete
    // number, not an "auto" mode.
    expect(result.current.density).toBe(initialColumnsForWidth(1024))
    expect(result.current.density).toBe(7)
    // And it really went to localStorage, so it stays put across a later resize.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('7')
  })

  it('seeds fewer columns on a narrow (phone-width) viewport', () => {
    setViewportWidth(375)
    const { result } = renderHook(() => useGridDensity())

    expect(result.current.density).toBe(2)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('2')
  })

  it('uses a stored number verbatim without re-seeding from width', () => {
    setViewportWidth(1024)
    window.localStorage.setItem(STORAGE_KEY, '4')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(4)
    // The stored choice is untouched — width never overrides it.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('4')
  })

  it('resolves a legacy "auto" to a concrete number and migrates storage', () => {
    setViewportWidth(1024)
    window.localStorage.setItem(STORAGE_KEY, '"auto"')
    const { result } = renderHook(() => useGridDensity())

    expect(result.current.density).toBe(7)
    // The legacy string is replaced by the concrete count, so it never recomputes.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('7')
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
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('6')
  })

  it('clamps an out-of-range value into 1..10 on the way in', () => {
    window.localStorage.setItem(STORAGE_KEY, '4')
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
    window.localStorage.setItem(STORAGE_KEY, '4')
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

  it('seeds a number instead of throwing on corrupt stored JSON', () => {
    window.localStorage.setItem(STORAGE_KEY, 'not-json-at-all')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(initialColumnsForWidth(window.innerWidth))
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('7')
  })

  it('re-renders every subscriber, so all grids on the page agree', () => {
    const a = renderHook(() => useGridDensity())
    const b = renderHook(() => useGridDensity())

    act(() => {
      a.result.current.setDensity(9)
    })

    expect(b.result.current.density).toBe(9)
  })

  it('keeps a second scope on its own number', () => {
    const library = renderHook(() => useGridDensity(LIBRARY_GRID_SCOPE))
    const review = renderHook(() => useGridDensity(OUTLIER_GRID_SCOPE))

    act(() => {
      library.result.current.setDensity(9)
    })

    expect(library.result.current.density).toBe(9)
    // Woken by the same listener set, but reading its own key: unmoved.
    expect(review.result.current.density).toBe(initialColumnsForWidth(1024, OUTLIER_GRID_SCOPE))
    expect(window.localStorage.getItem(OUTLIER_GRID_SCOPE.storageKey)).not.toBe('9')
  })

  it('clamps a desktop density down on a phone without touching the stored value', () => {
    setViewportWidth(393)
    window.localStorage.setItem(STORAGE_KEY, '8')
    const { result } = renderHook(() => useGridDensity())

    // Eight columns across 393px is a mesh of hearts over invisible photos.
    expect(result.current.density).toBe(3)
    expect(result.current.maxColumns).toBe(3)
    // The preference itself survives — the clamp is display-only.
    expect(result.current.storedDensity).toBe(8)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('8')
  })

  it('caps a small tablet one step higher than a phone', () => {
    setViewportWidth(700)
    window.localStorage.setItem(STORAGE_KEY, '8')
    const { result } = renderHook(() => useGridDensity())

    expect(result.current.density).toBe(4)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('8')
  })

  it('restores the chosen density when the window goes wide again', () => {
    setViewportWidth(393)
    window.localStorage.setItem(STORAGE_KEY, '8')
    const { result } = renderHook(() => useGridDensity())
    expect(result.current.density).toBe(3)

    resizeTo(1440)
    expect(result.current.density).toBe(8)
    expect(result.current.maxColumns).toBe(GRID_COLUMNS_MAX)

    // …and narrowing it clamps again, in both directions, for as long as the
    // stored 8 stays stored.
    resizeTo(393)
    expect(result.current.density).toBe(3)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('8')
  })

  it('leaves a density the viewport can carry alone', () => {
    setViewportWidth(393)
    window.localStorage.setItem(STORAGE_KEY, '2')
    const { result } = renderHook(() => useGridDensity())

    expect(result.current.density).toBe(2)
    expect(result.current.storedDensity).toBe(2)
  })

  it('clamps the review grid by the same viewport rules', () => {
    setViewportWidth(393)
    window.localStorage.setItem(OUTLIER_GRID_SCOPE.storageKey, '8')
    const { result } = renderHook(() => useGridDensity(OUTLIER_GRID_SCOPE))

    expect(result.current.density).toBe(3)
    expect(window.localStorage.getItem(OUTLIER_GRID_SCOPE.storageKey)).toBe('8')
  })

  it('does not thrash when the scope is passed as a fresh object each render', () => {
    // A call site that inlines `{…}` would otherwise re-run the seeding effect on
    // every render — the memo keys on the fields, not the object's identity.
    const { result, rerender } = renderHook(() => useGridDensity({ ...OUTLIER_GRID_SCOPE }))
    const seeded = initialColumnsForWidth(1024, OUTLIER_GRID_SCOPE)
    expect(result.current.density).toBe(seeded)

    act(() => {
      result.current.setDensity(2)
    })
    rerender()
    rerender()

    expect(result.current.density).toBe(2)
  })
})
