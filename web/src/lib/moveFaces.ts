import { type AssignRequest, type FaceView } from '../services/people'

/**
 * Where a move sends the faces: an existing person by uid, or a new one by name
 * (the backend finds-or-creates it by slug, exactly as naming a face does).
 * Exactly one of the two is set.
 */
export interface MoveTarget {
  subjectUid?: string
  subjectName?: string
}

/**
 * What one "move these photos to another person" run did. It counts photos, not
 * requests, everywhere but `moved`, because that is the unit the user picked.
 */
export interface MoveSummary {
  /** How many face markers were reassigned. */
  moved: number
  /** On how many of the picked photos at least one marker moved. */
  photos: number
  /** Photos where this person had no reassignable face (nothing was sent). */
  skipped: number
  /** Photos whose move failed; their assignment is unchanged. */
  failed: number
}

/** An empty summary, the starting point of a run. */
export const EMPTY_MOVE_SUMMARY: MoveSummary = { moved: 0, photos: 0, skipped: 0, failed: 0 }

/**
 * The assignment requests that move one photo's faces from `sourceUid` to the
 * target — one per marker the person holds on that photo, so a photo where they
 * were marked twice moves both.
 *
 * Only a face that actually carries a marker can move: the marker is what the
 * assignment repoints, and a bare detection has nothing to reassign. Faces the
 * backend synthesised for markers no detection claimed come back with a negative
 * `face_index` (see `facematch.appendUnmatchedMarkers`); those markers move, but
 * the index is left off the request — it names no face row, and sending it would
 * ask the backend to cache the link onto a slot that does not exist.
 *
 * The requests deliberately reuse `POST /photos/{uid}/faces/assign`, the same
 * write path the photo detail page and the review game use, so a move goes
 * through the assignment state machine and lands in the audit trail as an
 * ordinary `face.assign` — one entry per face, naming both the marker and the
 * person it now belongs to.
 */
export function moveRequests(
  faces: FaceView[],
  sourceUid: string,
  target: MoveTarget,
): AssignRequest[] {
  return faces
    .filter((face) => face.subject_uid === sourceUid && (face.marker_uid ?? '') !== '')
    .map((face) => {
      const req: AssignRequest = {
        action: 'assign_person',
        marker_uid: face.marker_uid,
        subject_uid: target.subjectUid,
        subject_name: target.subjectName,
      }
      if (face.face_index >= 0) {
        req.face_index = face.face_index
      }
      return req
    })
}

/** True when a run changed nothing at all — every photo was skipped or failed. */
export function movedNothing(summary: MoveSummary): boolean {
  return summary.moved === 0
}
