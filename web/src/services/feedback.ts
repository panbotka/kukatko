import { ApiError } from './auth'

/**
 * Persisted-feedback client, mirroring `internal/feedbackapi`. A rejection is an
 * opinion recorded durably ("this face is not this person") — it never mutates the
 * assignment, but it keeps the rejected face out of future candidate searches, so
 * the same wrong guess does not come back on the next run. This is the difference
 * from photo-sorter, where a rejection evaporated. The session cookie is sent
 * automatically (same-origin); every call throws {@link ApiError} on a non-OK
 * response.
 */

const API_BASE = '/api/v1'

/** Standard backend error envelope shared by every API group. */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/** Sends a body-carrying request that expects no content, throwing on non-OK. */
async function send(
  method: string,
  path: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * A "not this person" face rejection (`feedbackapi.faceRejectionInput`): the face
 * (photo UID + index) and the subject it is rejected for.
 */
export interface FaceRejection {
  photo_uid: string
  face_index: number
  subject_uid: string
}

/**
 * Records a face rejection via `POST /feedback/face-rejections`. It is idempotent —
 * rejecting the same pair twice is a no-op — so the caller can fire it optimistically.
 */
export async function rejectFace(req: FaceRejection, signal?: AbortSignal): Promise<void> {
  await send('POST', '/feedback/face-rejections', req, signal)
}

/**
 * Withdraws a face rejection via `DELETE /feedback/face-rejections`, the inverse of
 * {@link rejectFace}. Also idempotent.
 */
export async function unrejectFace(req: FaceRejection, signal?: AbortSignal): Promise<void> {
  await send('DELETE', '/feedback/face-rejections', req, signal)
}

/**
 * A "really this person" face confirmation (`feedbackapi.faceFeedbackInput`):
 * the face (photo UID + index) and the subject the assignment is confirmed for.
 * Recorded from the outlier review's ✗ ("no, this really is them") so a face a
 * user vouched for is not offered as an outlier again. Note the polarity: this
 * is the OPPOSITE of {@link rejectFace}, which records "this face is NOT this
 * person".
 */
export interface FaceConfirmation {
  photo_uid: string
  face_index: number
  subject_uid: string
}

/**
 * Records a face confirmation via `POST /feedback/face-confirmations`. It is
 * idempotent — confirming the same pair twice is a no-op — so the caller can
 * fire it optimistically.
 */
export async function confirmFace(req: FaceConfirmation, signal?: AbortSignal): Promise<void> {
  await send('POST', '/feedback/face-confirmations', req, signal)
}

/**
 * Withdraws a face confirmation via `DELETE /feedback/face-confirmations`, the
 * inverse of {@link confirmFace}. Also idempotent.
 */
export async function unconfirmFace(req: FaceConfirmation, signal?: AbortSignal): Promise<void> {
  await send('DELETE', '/feedback/face-confirmations', req, signal)
}

/**
 * A "not this label" photo rejection (`feedbackapi.labelRejectionInput`): the
 * photo and the label it is rejected for. Recorded from the /expand page's
 * per-tile ✗ so the photo is never offered for that collection again.
 */
export interface LabelRejection {
  photo_uid: string
  label_uid: string
}

/**
 * Records a label rejection via `POST /feedback/label-rejections`. It is
 * idempotent — rejecting the same pair twice is a no-op — so the caller can fire
 * it optimistically.
 */
export async function rejectLabel(req: LabelRejection, signal?: AbortSignal): Promise<void> {
  await send('POST', '/feedback/label-rejections', req, signal)
}

/**
 * Withdraws a label rejection via `DELETE /feedback/label-rejections`, the
 * inverse of {@link rejectLabel}. Also idempotent.
 */
export async function unrejectLabel(req: LabelRejection, signal?: AbortSignal): Promise<void> {
  await send('DELETE', '/feedback/label-rejections', req, signal)
}

/**
 * A "these two are genuinely different" duplicate dismissal
 * (`feedbackapi.duplicateDismissalInput`): the two photos of the pair. Recorded
 * from the compare view's "keep both" so the detector stops linking the pair.
 *
 * The pair is unordered — the backend normalises it — so which uid goes in which
 * field does not matter, and dismissing (A,B) then (B,A) stays one decision.
 */
export interface DuplicateDismissal {
  photo_uid: string
  other_uid: string
}

/**
 * Records a duplicate dismissal via `POST /feedback/duplicate-dismissals`, so the
 * pair is dropped from every later `GET /duplicates` scan rather than being offered
 * forever. Nothing is archived or merged: it records only the opinion. It is
 * idempotent, so the caller can fire it optimistically.
 */
export async function dismissDuplicate(
  req: DuplicateDismissal,
  signal?: AbortSignal,
): Promise<void> {
  await send('POST', '/feedback/duplicate-dismissals', req, signal)
}

/**
 * Withdraws a duplicate dismissal via `DELETE /feedback/duplicate-dismissals`, the
 * inverse of {@link dismissDuplicate}, putting the pair back in the review queue.
 * Also idempotent.
 */
export async function undismissDuplicate(
  req: DuplicateDismissal,
  signal?: AbortSignal,
): Promise<void> {
  await send('DELETE', '/feedback/duplicate-dismissals', req, signal)
}
