import { type ReactNode, useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { useLocation } from 'react-router-dom'

import { usePrefersReducedMotion } from '../../hooks/usePrefersReducedMotion'
import {
  MORPH_SETTLE_TIMEOUT_MS,
  morphStarter,
  type ViewTransitionHost,
} from '../../lib/viewTransition'

import { MorphContext, type MorphState } from './MorphContext'

/** Props for {@link MorphProvider}. */
export interface MorphProviderProps {
  children: ReactNode
  /**
   * The document the transitions run against. Defaults to the real one; tests
   * pass a stand-in carrying a fake `startViewTransition`, since jsdom
   * implements none.
   */
  document?: ViewTransitionHost
}

/**
 * The app's single view-transition runner: it owns which element is marked as
 * the morphing one, and it is the only place that wraps a router navigation in
 * `document.startViewTransition`.
 *
 * Keeping it in one place is the point of the whole design. A future route joins
 * the morph by navigating through {@link MorphState.morph} (or by rendering a
 * `MorphLink`) and marking its own element with `useMorphMark` — no second
 * integration with the router, and no second place to get the capture ordering
 * wrong.
 *
 * That ordering is the subtle part, and it is not the textbook one. The browser
 * captures the **old** state at the first rendering opportunity after
 * `startViewTransition` is called, so the element being left must already carry
 * the name by then: the mark is painted with `flushSync` *before* the transition
 * is asked for, not inside it. The **new** state is captured once the update
 * callback settles — and the usual `flushSync(() => navigate(…))` inside that
 * callback does **not** work here, because react-router v7's `BrowserRouter`
 * applies its own location update inside `React.startTransition`, which
 * `flushSync` cannot force through. The DOM would still be the old page when the
 * new state was captured, and the pair would morph the leaving element into
 * itself. So every morph is deferred: the callback triggers the navigation and
 * then waits for a `useLayoutEffect` on `location.key` to report that the new
 * route has rendered, or for {@link MORPH_SETTLE_TIMEOUT_MS} to run out. That
 * covers a history pop for free, which is asynchronous in its own right.
 */
export function MorphProvider({ children, document: doc }: MorphProviderProps) {
  const reducedMotion = usePrefersReducedMotion()
  const [morphId, setMorphId] = useState<string | null>(null)
  const location = useLocation()
  // Resolves the morph waiting on the navigation it triggered, if one is in flight.
  const settle = useRef<(() => void) | null>(null)

  // A layout effect, not a passive one: it runs synchronously after the commit
  // that rendered the new route, so the transition's new state is captured
  // against a DOM that already holds it rather than the page being left.
  useLayoutEffect(() => {
    const resolve = settle.current
    settle.current = null
    resolve?.()
  }, [location.key])

  // Re-read per morph rather than once: `document` is stable, but the reduced
  // motion preference can flip while the app is open, and a user who turns it on
  // must stop getting morphs immediately.
  const starter = useMemo(
    () =>
      morphStarter(doc ?? (typeof window === 'undefined' ? null : window.document), reducedMotion),
    [doc, reducedMotion],
  )

  const morph = useCallback(
    (id: string, navigate: () => void) => {
      if (starter === null) {
        navigate()
        return
      }
      flushSync(() => {
        setMorphId(id)
      })
      starter(async () => {
        const landed = new Promise<void>((resolve) => {
          const finish = (): void => {
            window.clearTimeout(timer)
            // Only ever retract its own registration: a later morph may already
            // have replaced it, and clearing that one would leave the newer
            // transition waiting on a resolver nobody holds.
            if (settle.current === finish) {
              settle.current = null
            }
            resolve()
          }
          // The escape hatch: a navigation that never lands — a pop the browser
          // refuses, a route that keeps the old page on screen while it loads —
          // must not leave the page frozen behind a transition that can no
          // longer finish. Giving up costs the morph, never the navigation.
          const timer = window.setTimeout(finish, MORPH_SETTLE_TIMEOUT_MS)
          settle.current = finish
        })
        navigate()
        await landed
      })
    },
    [starter],
  )

  const value = useMemo<MorphState>(
    () => ({ enabled: starter !== null, morphId, morph }),
    [starter, morphId, morph],
  )

  return <MorphContext.Provider value={value}>{children}</MorphContext.Provider>
}
