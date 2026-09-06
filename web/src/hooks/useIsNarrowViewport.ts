import { useEffect, useState } from 'react'

/**
 * The width at or below which the app treats the viewport as phone-width. It is
 * Bootstrap's `md` breakpoint boundary (`768px`), so a single source of truth
 * drives every "narrow viewport" decision — the filter bar's offcanvas drawer
 * and the grid's one-photo-per-row default alike.
 */
export const NARROW_VIEWPORT_QUERY = '(max-width: 767.98px)'

/**
 * The width at or below which the **navigation** folds into the hamburger: one
 * step wider than {@link NARROW_VIEWPORT_QUERY}, at Bootstrap's `lg` boundary
 * (`992px`).
 *
 * The shell's two navigations do not share the phone breakpoint above because
 * they are not sized by the content of a page but by the length of one row: the
 * inline bar's items (ten of them for a maintainer) need roughly 960px, while a
 * `.container` between `md` and `lg` offers 696px. Between 768px and 991px the
 * row therefore ran past the viewport — the user menu, sign-out included, ended
 * up off-screen and the page scrolled sideways. A tablet gets the drawer and the
 * tab bar instead, exactly as a phone does; everything else that asks "is this
 * viewport narrow?" keeps the `md` answer.
 */
export const NAV_DRAWER_QUERY = '(max-width: 991.98px)'

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/**
 * Resolves `query`, or `null` when `matchMedia` is unavailable — jsdom, for
 * instance, exposes the function but returns `undefined`, so route through
 * `unknown` + a guard rather than crashing on `.matches`.
 */
function mediaQuery(query: string): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  const result: unknown = window.matchMedia(query)
  return isMediaQueryList(result) ? result : null
}

/**
 * Reports whether `query` currently matches, keeping up with changes made while
 * the app is open (rotation, window resize). Environments without `matchMedia`
 * report `false`, so components fall back to their desktop layout.
 */
function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => mediaQuery(query)?.matches ?? false)
  useEffect(() => {
    const mq = mediaQuery(query)
    if (!mq || typeof mq.addEventListener !== 'function') return
    const handler = (e: MediaQueryListEvent) => {
      setMatches(e.matches)
    }
    mq.addEventListener('change', handler)
    // Re-read on subscribe: the viewport may have changed between the initial
    // render and this effect.
    setMatches(mq.matches)
    return () => {
      mq.removeEventListener('change', handler)
    }
  }, [query])
  return matches
}

/**
 * Reports whether the viewport is phone-width, keeping up with changes made
 * while the app is open (rotation, window resize). Environments without
 * `matchMedia` report "wide", so components fall back to their desktop layout.
 */
export function useIsNarrowViewport(): boolean {
  return useMediaQuery(NARROW_VIEWPORT_QUERY)
}

/**
 * Reports whether the viewport is too narrow for the inline navigation bar, i.e.
 * whether the shell owes it the hamburger drawer and the bottom tab bar (see
 * {@link NAV_DRAWER_QUERY}). Only the navigation asks this; everything else asks
 * {@link useIsNarrowViewport}.
 */
export function useIsNavDrawerViewport(): boolean {
  return useMediaQuery(NAV_DRAWER_QUERY)
}
