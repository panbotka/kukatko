import { createContext, useContext } from 'react'

/** The attribute that marks the element taking part in the current morph. */
const MORPH_ATTRIBUTE = 'data-kk-morph'

/** What {@link useMorphMark} spreads onto an element: the mark, or nothing. */
export type MorphMark = Record<string, '' | undefined>

/** The view-transition plumbing, as the rest of the app sees it. */
export interface MorphState {
  /**
   * Whether a morph can run at all here: the browser implements the View
   * Transitions API and the user has not asked for reduced motion. Callers read
   * it to decide whether to take over a navigation — when it is false they must
   * leave the navigation exactly as it was, which is the fallback path.
   */
  enabled: boolean
  /** The id whose element currently carries the shared name, or null for none. */
  morphId: string | null
  /**
   * Runs `navigate` as a morph away from the element identified by `id`, and
   * into whichever element on the next page claims the same id.
   *
   * The navigation is **deferred**, both for a push and for a history pop: this
   * app's router applies its own state update inside `React.startTransition`, so
   * the DOM does not change while `navigate` runs — not even under `flushSync` —
   * and a pop is asynchronous anyway. The transition is therefore held open until
   * the new route has rendered (or until it gives up waiting), which is what
   * makes the browser snapshot the page being entered rather than the one being
   * left. When {@link MorphState.enabled} is false it simply runs `navigate`.
   */
  morph: (id: string, navigate: () => void) => void
}

/**
 * The inert default: nothing is marked and every navigation runs plainly.
 *
 * A non-throwing default is the point. The morph is decoration on top of a
 * working navigation, so a component rendered outside a `MorphProvider` — a
 * focused unit test, a page mounted straight into a `MemoryRouter` — must keep
 * navigating exactly as it always did rather than crash the tree.
 */
export const MORPH_DEFAULT: MorphState = {
  enabled: false,
  morphId: null,
  morph: (_id, navigate) => {
    navigate()
  },
}

/** Provides the app's single view-transition runner; see {@link MORPH_DEFAULT}. */
export const MorphContext = createContext<MorphState>(MORPH_DEFAULT)

/** The view-transition plumbing from the nearest provider, or the inert default. */
export function useMorph(): MorphState {
  return useContext(MorphContext)
}

/**
 * The attributes that make an element one half of the morphing pair while `id`
 * is the one being morphed, and nothing at all otherwise.
 *
 * Spread it onto the element that *is* the photograph on each side — the grid
 * tile's media box and the viewer's figure. Marking anything else (a wrapper
 * with padding, say) would morph that box instead and the photograph would
 * appear to jump inside it.
 */
export function useMorphMark(id: string): MorphMark {
  const { morphId } = useMorph()
  return morphId === id ? { [MORPH_ATTRIBUTE]: '' } : {}
}
