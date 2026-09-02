/**
 * The one place the app talks to the browser's View Transitions API.
 *
 * Opening a photograph should read as the tile *growing* into the viewer rather
 * than as a page swap, and the platform can do that for free: name the same
 * element on both sides of a DOM change and the browser interpolates between the
 * two snapshots. The whole trick is that the change has to happen *inside*
 * `document.startViewTransition`, so the old state is captured before it and the
 * new one after — which is why the app's router navigation has to be routed
 * through here instead of being called directly.
 *
 * This module is deliberately the pure half: it knows how to find the API and
 * how to decide whether a morph may run at all, and nothing about React or the
 * router. The React half — marking the element, holding a transition open until
 * an asynchronous history pop has landed — is `components/morph`.
 *
 * Progressive enhancement is the hard rule: in a browser without the API (every
 * Firefox and Safari before 18, and jsdom) `morphStarter` returns `null` and
 * every caller falls back to exactly the navigation it did before. Nothing here
 * polyfills anything.
 */

/**
 * The `view-transition-name` the morphing pair shares. One name, not one per
 * photo: only ever two elements carry it — the tile being left and the viewer
 * being entered — and they are never in the document at the same time, since a
 * route change replaces one with the other. The mark is applied through the
 * `[data-kk-morph]` attribute (see `styles/viewTransition.css`) rather than an
 * inline style, so it is observable from jsdom, where the property itself is not.
 */
export const MORPH_NAME = 'kk-morph'

/**
 * How long the app waits for a navigation to land before it lets the transition
 * finish anyway.
 *
 * A morph has to hold its transition open until the page being entered has
 * rendered, because that is when the browser captures the new state — see
 * `components/morph`. If the navigation never lands (a pop the browser refuses,
 * a route that keeps the old page on screen), the morph must not freeze the page
 * behind a transition that can no longer finish: it gives up after this long and
 * completes against whatever is on screen, which is a cross-fade.
 */
export const MORPH_SETTLE_TIMEOUT_MS = 400

/** The part of the browser's `ViewTransition` object this app actually uses. */
export interface ViewTransitionHandle {
  /** Resolves once the animation has finished (or been skipped). */
  finished: Promise<void>
}

/** `document.startViewTransition`, as this app calls it. */
export type StartViewTransition = (update: () => void | Promise<void>) => ViewTransitionHandle

/**
 * A document seen as "might implement the View Transitions API".
 *
 * Structural on purpose rather than `Document`: the property is absent from the
 * DOM typings this project builds against, and a test — jsdom implements none of
 * this — can hand the runner a two-property stand-in instead of forging a whole
 * document.
 */
export interface ViewTransitionHost {
  startViewTransition?: unknown
}

/**
 * The document's `startViewTransition` bound to it, or `null` where the browser
 * does not implement the API.
 *
 * The lookup goes through `unknown` on purpose: the property is absent from the
 * DOM typings this project builds against, and inventing a global declaration
 * for it would make the *type system* claim a capability the runtime may not
 * have — exactly the assumption this function exists to avoid making.
 */
export function viewTransitionStarter(
  doc: ViewTransitionHost | null | undefined,
): StartViewTransition | null {
  if (doc === null || doc === undefined) {
    return null
  }
  const candidate = doc.startViewTransition
  if (typeof candidate !== 'function') {
    return null
  }
  const start = candidate as StartViewTransition
  return (update) => start.call(doc, update)
}

/**
 * The starter to run a morph with, or `null` when the app must not morph at all
 * — either because the browser cannot, or because the user asked their OS to
 * reduce motion. The preference means "no motion", not "less motion", so it
 * turns the morph off rather than shortening it; the navigation itself is
 * untouched either way.
 */
export function morphStarter(
  doc: ViewTransitionHost | null | undefined,
  reducedMotion: boolean,
): StartViewTransition | null {
  return reducedMotion ? null : viewTransitionStarter(doc)
}
