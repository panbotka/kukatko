import { type FaceView } from '../services/people'

/**
 * How far a detected face has got through naming — and that is the whole
 * question, so there are only two answers:
 *
 * - `named` — it names a subject (green).
 * - `unnamed` — nobody is named on it yet (yellow).
 *
 * An earlier version split `unnamed` in two by whether a marker already covered
 * the face. That is bookkeeping inherited from PhotoPrism (which has boxes)
 * meeting Kukátko's detector (which has vectors): naming either one is the same
 * single click, only the verb the backend picks differs (`create_marker` vs
 * `assign_person`, see `internal/facematch`). The split cost the reader a third
 * colour for a fact nobody acts on — and the marker-less half is ~82 % of the
 * library, reported in the colour that means "something is wrong".
 *
 * The distinction that does matter is {@link hasEmbedding}, and it is not a
 * colour: it decides whether automation can ever reach the face.
 */
export type FaceState = 'named' | 'unnamed'

/** True when the face names a subject. */
export function isNamed(face: FaceView): boolean {
  return face.subject_name !== undefined && face.subject_name !== ''
}

/**
 * True when the row is backed by a stored face vector — i.e. the recogniser has
 * an embedding for it.
 *
 * The faces payload carries this without a dedicated field: `facematch` lists the
 * photo's stored faces under their own per-photo slot and then appends the markers
 * that matched none of them under *descending negative* indexes, so a negative
 * `face_index` is exactly "a marker with no face row behind it" (144 of them in
 * production). Such a face is nameable by hand, but no similarity search will ever
 * surface it, the sweep cannot suggest it, and naming it teaches the recogniser
 * nothing.
 */
export function hasEmbedding(face: FaceView): boolean {
  return face.face_index >= 0
}

/**
 * Classifies a face for display. It reads the assignment fields rather than
 * `face.action` because the optimistic update in `useFaces` patches the name
 * before the server re-states the action — deriving the state from the name keeps
 * the box and the panel row in step with the click that just happened.
 */
export function faceState(face: FaceView): FaceState {
  return isNamed(face) ? 'named' : 'unnamed'
}
