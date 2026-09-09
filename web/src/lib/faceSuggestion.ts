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
 * So the display floor here is a line drawn across that widened tail: strong
 * enough that a suggestion is worth a button, low enough that the faces of people
 * the library already knows are not withheld. Everything under it is shown once,
 * muted and labelled uncertain, so the information survives without looking like
 * advice. Where the line sits, and what was measured to put it there, is in
 * `docs/THRESHOLDS.md`.
 */

import { type FaceView, type Suggestion } from '../services/people'
import { hasEmbedding, isNamed } from './faceState'

/**
 * The confidence a suggestion must reach to be offered as a clickable chip.
 *
 * It sat at 0.5 — the complement of `faces.suggestion_max_distance`
 * (`facematch.DefaultSuggestionMaxDistance`), i.e. exactly where the backend's
 * primary suggestion search stops — and is now **0.4**, one band into the widened
 * tail. Measured against the names a human already gave, a top suggestion in the
 * 0.40–0.50 band is the right person 88 % of the time, and admitting it grows the
 * share of unnamed faces carrying a one-tap button by about half. See
 * `docs/THRESHOLDS.md` for the measurement and for why the line stops here.
 */
export const SUGGESTION_DISPLAY_FLOOR = 0.4

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

/**
 * topSuggestion returns the identity a one-tap confirmation would give `face`, or
 * null when the face may not be confirmed at all.
 *
 * A face qualifies only when it is still unnamed, has an embedding (a marker with
 * no face row behind it never carries a suggestion anyway, see
 * {@link hasEmbedding}) and its strongest suggestion clears the display floor —
 * the tier {@link rankSuggestions} already offers as a button. The strongest one
 * is picked by comparison rather than by taking the first element, so a caller
 * that reorders cannot confirm the wrong person.
 *
 * It is the single answer to "what does this face's confirm button do", shared by
 * the photo detail's bulk action ({@link bulkConfirmations}) and by the album
 * face-tagging run, which shows the same identity as one row's offer.
 */
export function topSuggestion(face: FaceView): Suggestion | null {
  if (isNamed(face) || !hasEmbedding(face)) {
    return null
  }
  // The whole offered tier, then its maximum by comparison — the backend sorts
  // by descending confidence, but nothing here depends on it having done so.
  const { offered } = rankSuggestions(face.suggestions, face.suggestions.length)
  return offered.reduce<Suggestion | null>(
    (best, suggestion) =>
      best === null || suggestion.confidence > best.confidence ? suggestion : best,
    null,
  )
}

/** One unnamed face and the identity a bulk confirmation would give it. */
export interface FaceConfirmation {
  /** The face to be named. */
  face: FaceView
  /** Its strongest offered suggestion — the identity the confirmation applies. */
  subject: Suggestion
}

/**
 * bulkConfirmations picks the faces a single "confirm all" may name, and the
 * identity each of them would get.
 *
 * A face qualifies exactly when {@link topSuggestion} names somebody for it.
 * There is deliberately no second, stricter threshold: the bulk action confirms
 * exactly what the panel offers one by one, and follows the floor if it ever
 * moves (see `docs/THRESHOLDS.md`).
 *
 * **One person is named once per photo.** When two qualifying faces top-suggest
 * the same subject, only the more confident one is confirmed; the other is left
 * untouched for a human to decide, because a photo carrying the same person on
 * two markers is exactly what `internal/dupmarkers` exists to clean up.
 *
 * The result keeps the panel's reading order, so the run walks the photo the way
 * the rows are listed.
 *
 * @param faces the photo's faces, in the panel's order
 */
export function bulkConfirmations(faces: FaceView[]): FaceConfirmation[] {
  const position = new Map(faces.map((face, index) => [face.face_index, index]))
  // Strongest confirmation per subject: the second face suggesting the same
  // person loses rather than adding a duplicate marker.
  const bySubject = new Map<string, FaceConfirmation>()
  for (const face of faces) {
    const top = topSuggestion(face)
    if (top === null) {
      continue
    }
    const held = bySubject.get(top.subject_uid)
    if (held === undefined || top.confidence > held.subject.confidence) {
      bySubject.set(top.subject_uid, { face, subject: top })
    }
  }
  return [...bySubject.values()].sort(
    (a, b) => (position.get(a.face.face_index) ?? 0) - (position.get(b.face.face_index) ?? 0),
  )
}
