/**
 * "This page is where the visit started" — for a page that forwards somewhere
 * else by *replacing* its own history entry.
 *
 * The photo viewer closes by stepping back through history, so the browser
 * restores the grid it was opened from, and it only reconstructs the list URL
 * when it was loaded directly — a deep link, a refresh — which it recognises by
 * the router's `default` location key. A page that forwards with `replace` hands
 * the viewer a fresh key, so a viewer reached through it believes there is a
 * grid behind it. When the forwarding page was itself the first entry (a push
 * tapped on a phone opens a new window on `/n/{uid}`), there is nothing behind,
 * `navigate(-1)` does nothing, and the close button is dead.
 *
 * The forwarding page therefore says so in the navigation state, and the viewer
 * treats that state exactly like the `default` key. The address stays the plain
 * `/photos/{uid}`; a viewer reached any other way carries no such state.
 */

/** The navigation state a forwarding page attaches when it was the first entry. */
export interface DirectEntry {
  directEntry: true
}

/** The state to navigate with, so the destination knows nothing is behind it. */
export function directEntryState(): DirectEntry {
  return { directEntry: true }
}

/**
 * Whether the navigation state says the page was reached from a first entry.
 * History state is opaque and outlives the navigation that set it, so it is
 * checked rather than trusted: anything but the exact marker reads as "no".
 */
export function isDirectEntry(state: unknown): boolean {
  if (typeof state !== 'object' || state === null) {
    return false
  }
  return (state as Partial<Record<keyof DirectEntry, unknown>>).directEntry === true
}

/**
 * Whether the current page is the first in-app entry of this history: nothing
 * of the app's own lies behind it for a `navigate(-1)` to return to.
 *
 * The router's `default` key catches a plain load. It misses the page reached
 * after the sign-in round trip — the guard and the login page both navigate with
 * `replace`, so the entry has a real key yet still nothing behind it — which the
 * index the browser router keeps in `history.state` (`idx`, 0 for the entry it
 * started on) does catch. A memory router keeps no such index, so there only the
 * key decides.
 */
export function isFirstEntry(locationKey: string): boolean {
  if (locationKey === 'default') {
    return true
  }
  const state: unknown = window.history.state
  if (typeof state !== 'object' || state === null) {
    return false
  }
  return (state as { idx?: unknown }).idx === 0
}
