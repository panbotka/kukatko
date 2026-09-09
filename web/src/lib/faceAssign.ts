import { type AssignRequest, type FaceView } from '../services/people'

/**
 * Who a face is being named as: an existing subject by uid, or a new one by name
 * (the backend finds-or-creates it by slug). Exactly one of the two is set — the
 * request shape carries both fields, and the backend reads whichever is present.
 */
export type AssignTarget = Pick<AssignRequest, 'subject_uid' | 'subject_name'>

/**
 * buildAssign builds the request that names `face` with the given identity.
 *
 * A face matched to an existing marker is assigned in place; one with no marker
 * creates a new marker from its bbox. That fork is the backend's own
 * (`internal/facematch`) and stays here rather than in a caller: the UI shows
 * neither of its two branches, because naming either face is the same one click
 * (see `lib/faceState`).
 *
 * It lives in `lib/` rather than inside a hook because two different naming
 * surfaces send it — the photo detail's faces panel (`useFaces`) and the album
 * face-tagging run (`useAlbumFaces`) — and a second, subtly different builder is
 * exactly how one of them would start creating markers the other assigns.
 */
export function buildAssign(face: FaceView, who: AssignTarget): AssignRequest {
  if (face.marker_uid !== undefined && face.marker_uid !== '') {
    return { action: 'assign_person', marker_uid: face.marker_uid, ...who }
  }
  return { action: 'create_marker', bbox: face.bbox, face_index: face.face_index, ...who }
}
