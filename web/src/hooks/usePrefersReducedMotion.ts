import { useEffect, useState } from 'react'

/** The media query that reports the OS-level "reduce motion" accessibility setting. */
const QUERY = '(prefers-reduced-motion: reduce)'

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/**
 * Resolves the reduced-motion media query, or `null` where `matchMedia` is
 * unavailable — jsdom, for instance, may expose the function but return
 * `undefined`, so route through `unknown` + a guard rather than crashing on
 * `.matches`.
 */
function reducedMotionQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return null
  }
  const result: unknown = window.matchMedia(QUERY)
  return isMediaQueryList(result) ? result : null
}

/** Reads the current preference, treating a missing `matchMedia` as "no preference". */
function matchesReducedMotion(): boolean {
  return reducedMotionQuery()?.matches ?? false
}

/**
 * Reports whether the user asked their OS to reduce motion, keeping up with
 * changes made while the app is open. Callers use it to skip decorative
 * animation (the slideshow's Ken Burns pan, for instance) rather than merely
 * shortening it — the preference means "no motion", not "less motion".
 *
 * Environments without `matchMedia` report no preference.
 */
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(matchesReducedMotion)

  useEffect(() => {
    const query = reducedMotionQuery()
    if (query === null) {
      return
    }
    // Re-read on subscribe: the preference may have flipped between the initial
    // render and this effect.
    setReduced(query.matches)

    const onChange = (event: MediaQueryListEvent): void => {
      setReduced(event.matches)
    }
    query.addEventListener('change', onChange)
    return () => {
      query.removeEventListener('change', onChange)
    }
  }, [])

  return reduced
}
