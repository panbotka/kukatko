import { type FaceView } from '../services/people'

/**
 * How far a detected face has got through naming:
 *
 * - `assigned` — it names a subject (green).
 * - `unassigned` — a marker covers it, but nobody is named on that marker (yellow).
 * - `unmatched` — a raw detection with no marker at all (red).
 *
 * The order mirrors the work left to do, which is why the overlay and the faces
 * panel colour it the same way: the redder the box, the less anyone knows about it.
 */
export type FaceState = 'assigned' | 'unassigned' | 'unmatched'

/** True when the face names a subject. */
export function isNamed(face: FaceView): boolean {
  return face.subject_name !== undefined && face.subject_name !== ''
}

/**
 * Classifies a face for display. It reads the assignment fields rather than
 * `face.action` because the optimistic update in `useFaces` patches the name
 * before the server re-states the action — deriving the state from the name keeps
 * the box and the panel row in step with the click that just happened.
 */
export function faceState(face: FaceView): FaceState {
  if (isNamed(face)) {
    return 'assigned'
  }
  if (face.marker_uid !== undefined && face.marker_uid !== '') {
    return 'unassigned'
  }
  return 'unmatched'
}
