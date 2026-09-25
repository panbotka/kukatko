/**
 * The way back from a photo's own page to the sorting game it was opened from.
 *
 * The game opens a photo in the same window — an installed PWA has no tabs, so a
 * new one would replace the game with no way home. The run itself survives the
 * trip (`lib/reviewSnapshot`); what the photo page needs is to *know* it was
 * reached from the game, so it can offer a labelled way back at the top, and
 * where exactly to go — the game's own URL, source toggle included.
 *
 * That knowledge rides in the router's navigation state rather than in the URL:
 * the photo's address stays the plain `/photos/{uid}` a player copies or shares,
 * and a photo reached any other way (a grid, a link, a new tab) carries no state
 * and so shows no way back to a game it never came from.
 */

/** The navigation state the game attaches to its way out to a photo. */
export interface ReviewReturn {
  /** The game's own path and query (`/review?source=people`) to return to. */
  reviewReturn: string
}

/** The game's route; a return path must be it, never an arbitrary address. */
const REVIEW_PATH = '/review'

/** Whether `path` is the game's own route, optionally with a query string. */
function isReviewPath(path: string): boolean {
  return path === REVIEW_PATH || path.startsWith(`${REVIEW_PATH}?`)
}

/**
 * The state for a trip from the game at `pathname` + `search` to a photo. The
 * location is taken as the router reports it, so the way back restores the very
 * source the player had chosen.
 */
export function reviewReturnState(pathname: string, search: string): ReviewReturn {
  return { reviewReturn: `${pathname}${search}` }
}

/**
 * The game's path to return to, or undefined when the photo page was not
 * reached from the game.
 *
 * History state is opaque and outlives the navigation that set it, so it is
 * validated rather than trusted: anything that is not the game's own route
 * reads as "not from the game" and the page offers no way back.
 */
export function reviewReturnPath(state: unknown): string | undefined {
  if (typeof state !== 'object' || state === null) {
    return undefined
  }
  const candidate = (state as Partial<Record<keyof ReviewReturn, unknown>>).reviewReturn
  if (typeof candidate !== 'string' || !isReviewPath(candidate)) {
    return undefined
  }
  return candidate
}
