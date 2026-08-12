import { useEffect, useState } from 'react'

import { COARSE_POINTER_QUERY } from '../lib/mapGestures'

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/**
 * Resolves the coarse-pointer media query, or `null` where `matchMedia` is
 * unavailable — jsdom, for instance, may expose the function but return
 * `undefined`, so route through `unknown` + a guard rather than crashing on
 * `.matches`.
 */
function coarsePointerQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return null
  }
  const result: unknown = window.matchMedia(COARSE_POINTER_QUERY)
  return isMediaQueryList(result) ? result : null
}

/** Reads the current pointer kind, treating a missing `matchMedia` as "fine". */
function matchesCoarsePointer(): boolean {
  return coarsePointerQuery()?.matches ?? false
}

/**
 * Reports whether the device's primary pointer is a finger rather than a mouse,
 * keeping up with changes made while the app is open (a tablet gaining a
 * keyboard case, a phone paired with a mouse).
 *
 * It is the **layout-independent** half of the app's two responsive questions:
 * {@link useIsNarrowViewport} answers "how much room is there", this one answers
 * "how precisely can it be pointed at". A control whose *hit area* has to grow
 * asks this one — a tablet is wide and still touched, a 500 px desktop window is
 * narrow and still has a mouse — and gets the answer in JS when the decision is
 * not purely a matter of CSS. The query itself is the app's single spelling of
 * it, shared with the maps' two-finger gesture handling
 * (`lib/mapGestures.prefersTouchGestures`, the same decision without React).
 *
 * Environments without `matchMedia` report a fine pointer, so components keep
 * their compact desktop behaviour.
 */
export function useCoarsePointer(): boolean {
  const [coarse, setCoarse] = useState(matchesCoarsePointer)

  useEffect(() => {
    const query = coarsePointerQuery()
    if (query === null || typeof query.addEventListener !== 'function') {
      return
    }
    // Re-read on subscribe: the pointer may have changed between the initial
    // render and this effect.
    setCoarse(query.matches)

    const onChange = (event: MediaQueryListEvent): void => {
      setCoarse(event.matches)
    }
    query.addEventListener('change', onChange)
    return () => {
      query.removeEventListener('change', onChange)
    }
  }, [])

  return coarse
}
