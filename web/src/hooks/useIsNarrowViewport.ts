import { useEffect, useState } from 'react'

/**
 * The width at or below which the app treats the viewport as phone-width. It is
 * Bootstrap's `md` breakpoint boundary (`768px`), so a single source of truth
 * drives every "narrow viewport" decision — the filter bar's offcanvas drawer
 * and the grid's one-photo-per-row default alike.
 */
export const NARROW_VIEWPORT_QUERY = '(max-width: 767.98px)'

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/**
 * Resolves the narrow-viewport media query, or `null` when `matchMedia` is
 * unavailable — jsdom, for instance, exposes the function but returns
 * `undefined`, so route through `unknown` + a guard rather than crashing on
 * `.matches`.
 */
function narrowQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  const result: unknown = window.matchMedia(NARROW_VIEWPORT_QUERY)
  return isMediaQueryList(result) ? result : null
}

/** Reads the current narrow-viewport state, treating a missing `matchMedia` as "wide". */
function matchesNarrow(): boolean {
  return narrowQuery()?.matches ?? false
}

/**
 * Reports whether the viewport is phone-width, keeping up with changes made
 * while the app is open (rotation, window resize). Environments without
 * `matchMedia` report "wide", so components fall back to their desktop layout.
 */
export function useIsNarrowViewport(): boolean {
  const [narrow, setNarrow] = useState(matchesNarrow)
  useEffect(() => {
    const mq = narrowQuery()
    if (!mq || typeof mq.addEventListener !== 'function') return
    const handler = (e: MediaQueryListEvent) => {
      setNarrow(e.matches)
    }
    mq.addEventListener('change', handler)
    // Re-read on subscribe: the viewport may have changed between the initial
    // render and this effect.
    setNarrow(mq.matches)
    return () => {
      mq.removeEventListener('change', handler)
    }
  }, [])
  return narrow
}
