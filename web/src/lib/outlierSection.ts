/**
 * The pure model behind the outlier section of a **person's page** — the small
 * "is this still the same person?" panel, not the `/outliers` workspace (that
 * one is `lib/outlierReview.ts`, and the two share the ranking's key and the
 * "can this face be detached at all" test).
 *
 * The panel asks a few questions at a time. The backend hands over the whole
 * ranking — hundreds of faces for a well-tagged person — and this module decides
 * which of them are on screen: a batch counter the reader grows explicitly, and
 * a set of faces that turned out to have no picture and are therefore never
 * asked about. Both live here, pure and unit-tested, so the component holds
 * nothing but the fetch and the markup.
 *
 * A rejected face is **not** dropped from the list: it stays where it is,
 * marked `removed`, which is what lets the reader take the answer back from the
 * same spot a second later.
 */

import { type OutlierFace } from '../services/people'

import { outlierKey } from './outlierReview'

/**
 * How many faces one batch shows. Small on purpose: the reader is answering a
 * question about each face, and thirty tiles at once is a wall to skim past
 * rather than a question to answer. Growing the batch is one click.
 */
export const OUTLIER_SECTION_BATCH = 8

/** Where a face in the section stands: unanswered, or detached and undoable. */
export type OutlierAnswer = 'pending' | 'removed'

/** One face the section asks about, with the answer given so far. */
export interface OutlierQuestion {
  face: OutlierFace
  answer: OutlierAnswer
}

/** Seeds the section's working list from a fresh ranking. */
export function toQuestions(faces: readonly OutlierFace[]): OutlierQuestion[] {
  return faces.map((face) => ({ face, answer: 'pending' }))
}

/** Returns the list with one face's answer replaced; others are untouched. */
export function answerQuestion(
  items: readonly OutlierQuestion[],
  key: string,
  answer: OutlierAnswer,
): OutlierQuestion[] {
  return items.map((item) => (outlierKey(item.face) === key ? { ...item, answer } : item))
}

/**
 * Drops the faces whose picture could not be produced. A question nobody can
 * see is not a question — the reader would be asked to judge a grey square —
 * so those faces leave the list entirely and the next ranked face takes the
 * place they held.
 */
export function askableQuestions(
  items: readonly OutlierQuestion[],
  unavailable: ReadonlySet<string>,
): OutlierQuestion[] {
  return items.filter((item) => !unavailable.has(outlierKey(item.face)))
}

/** The faces currently on screen: the first `revealed` askable ones. */
export function revealedQuestions(
  items: readonly OutlierQuestion[],
  revealed: number,
): OutlierQuestion[] {
  return items.slice(0, Math.max(0, revealed))
}

/** How many askable faces are still waiting behind the "show more" control. */
export function hiddenCount(items: readonly OutlierQuestion[], revealed: number): number {
  return Math.max(0, items.length - Math.max(0, revealed))
}
