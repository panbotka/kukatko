import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  NARROW_VIEWPORT_QUERY,
  NAV_DRAWER_QUERY,
  useIsNarrowViewport,
  useIsNavDrawerViewport,
} from './useIsNarrowViewport'

/**
 * Two breakpoints, deliberately one step apart: the page's idea of "phone-width"
 * (`md`) and the navigation's own (`lg`). They were the same query until a
 * portrait tablet proved the inline navbar cannot fit between them — see
 * `NAV_DRAWER_QUERY`. What matters is that the two hooks stay *separately*
 * answerable, so widening the navigation's does not quietly turn a tablet's photo
 * grid into a phone's.
 */

/** Answers each media query against a fixed viewport width. */
function mockWidth(width: number): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => {
    const min = /min-width:\s*([\d.]+)px/.exec(query)
    const max = /max-width:\s*([\d.]+)px/.exec(query)
    return {
      matches:
        (min === null || width >= Number(min[1])) && (max === null || width <= Number(max[1])),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }
  })
}

/** The width each query stops matching above. */
function boundary(query: string): number {
  return Number(/max-width:\s*([\d.]+)px/.exec(query)?.[1])
}

describe('viewport breakpoints', () => {
  it('folds the navigation one step earlier than the page turns phone-shaped', () => {
    expect(boundary(NAV_DRAWER_QUERY)).toBeGreaterThan(boundary(NARROW_VIEWPORT_QUERY))
    expect(boundary(NARROW_VIEWPORT_QUERY)).toBeCloseTo(767.98)
    expect(boundary(NAV_DRAWER_QUERY)).toBeCloseTo(991.98)
  })

  it.each([390, 767])('reports both narrow on a phone (%ipx)', (width) => {
    mockWidth(width)
    expect(renderHook(() => useIsNarrowViewport()).result.current).toBe(true)
    expect(renderHook(() => useIsNavDrawerViewport()).result.current).toBe(true)
  })

  it.each([768, 834, 900, 991])('folds only the navigation on a tablet (%ipx)', (width) => {
    mockWidth(width)
    // The band the navbar used to overflow in: the bar owes the reader its
    // drawer, while the page below it is still a wide layout.
    expect(renderHook(() => useIsNarrowViewport()).result.current).toBe(false)
    expect(renderHook(() => useIsNavDrawerViewport()).result.current).toBe(true)
  })

  it.each([992, 1440])('reports neither on a desktop (%ipx)', (width) => {
    mockWidth(width)
    expect(renderHook(() => useIsNarrowViewport()).result.current).toBe(false)
    expect(renderHook(() => useIsNavDrawerViewport()).result.current).toBe(false)
  })

  it('reports wide where matchMedia is unavailable', () => {
    // jsdom exposes the function but answers `undefined`; a component must fall
    // back to its desktop layout rather than crash on `.matches`.
    window.matchMedia = vi.fn().mockImplementation(() => undefined)
    expect(renderHook(() => useIsNarrowViewport()).result.current).toBe(false)
    expect(renderHook(() => useIsNavDrawerViewport()).result.current).toBe(false)
  })
})
