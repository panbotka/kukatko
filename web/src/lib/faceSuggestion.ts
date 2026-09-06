/**
 * Which identity suggestions a face panel may offer as a one-click chip.
 *
 * The backend deliberately hands out weak suggestions: `facematch.suggestForFace`
 * runs its primary search at `faces.suggestion_max_distance` and then, for an
 * *unnamed* face only, widens the search to no cutoff at all so a face always has
 * some candidates to look at (`fillSuggestions`). Those extra ones are nearest
 * neighbours, not matches — a 10 % suggestion is the closest of a bad lot, and
 * offering it as a button beside a genuine one invites a wrong click on a photo
 * of a crowd.
 *
 * So the display floor here is not a new threshold: it restores the backend's own
 * cutoff at the point where a suggestion becomes a *recommendation*. Anything the
 * primary pass would have returned is offered; anything only the widening produced
 * is shown once, muted and labelled uncertain, so the information survives without
 * looking like advice. See `docs/THRESHOLDS.md`.
 */

import { type Suggestion } from '../services/people'

/**
 * The confidence a suggestion must reach to be offered as a clickable chip.
 *
 * It is the complement of `faces.suggestion_max_distance` (0.5, see
 * `facematch.DefaultSuggestionMaxDistance`): confidence is `1 - distance`, so a
 * confidence of 0.5 is exactly the distance the backend's primary suggestion
 * search stops at.
 */
export const SUGGESTION_DISPLAY_FLOOR = 0.5

/** What a face panel should render for one face's ranked suggestions. */
export interface RankedSuggestions {
  /** Suggestions strong enough to be offered as one-click chips, strongest first. */
  offered: Suggestion[]
  /**
   * The strongest suggestion when *none* cleared the floor — shown muted and
   * labelled as uncertain. Null whenever something is offered, or when the face
   * has no suggestions at all.
   */
  uncertain: Suggestion | null
}

/**
 * rankSuggestions splits a face's ranked suggestions into the ones worth offering
 * and the single weak one worth mentioning.
 *
 * The backend already sorts by descending confidence, but the strongest candidate
 * is picked by comparison rather than by taking the first element, so a caller
 * that reorders (or a future endpoint that does not sort) cannot label the wrong
 * person "the closest we found".
 *
 * @param suggestions the face's suggestions, in any order
 * @param max how many chips the panel has room for
 */
export function rankSuggestions(suggestions: Suggestion[], max: number): RankedSuggestions {
  const offered = suggestions
    .filter((suggestion) => suggestion.confidence >= SUGGESTION_DISPLAY_FLOOR)
    .slice(0, max)
  if (offered.length > 0) {
    return { offered, uncertain: null }
  }
  const uncertain = suggestions.reduce<Suggestion | null>(
    (best, suggestion) =>
      best === null || suggestion.confidence > best.confidence ? suggestion : best,
    null,
  )
  return { offered: [], uncertain }
}
