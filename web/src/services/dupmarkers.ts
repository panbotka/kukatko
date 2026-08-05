import { ApiError } from './auth'
import { type Bbox } from './people'

/**
 * Client for the repeated-marker review (`internal/dupmarkersapi`): the photos
 * where one and the same person carries more than one valid face marker.
 *
 * The listing is read-only; the two repairs it offers are the existing write
 * paths under another name — keeping one marker detaches the others through the
 * face-assignment state machine, and flagging one invalid flips the marker flag.
 * The third decision a curator can make ("leave it be") is persisted feedback and
 * lives in `services/feedback`. The session cookie is sent automatically
 * (same-origin); every call throws {@link ApiError} on a non-OK response.
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

/** One marker of a repeated-marker group. */
export interface DuplicateMarker {
  uid: string
  /** Normalised bounding box [x, y, w, h] in 0..1 display space. */
  bbox: Bbox
  /** Detector/matcher confidence as an integer percentage; 0 means unrecorded. */
  score: number
  /** Whether a user already confirmed this marker. */
  reviewed: boolean
}

/**
 * One finding: a person marked more than once on one photo. `width`/`height` are
 * the photo's stored (pre-rotation) pixel dimensions and `orientation` its raw
 * EXIF tag, which together give the frame the bboxes are measured against.
 * `markers` arrive ordered left to right, so their position in the array is the
 * number drawn over the preview.
 */
export interface DuplicateMarkerGroup {
  photo_uid: string
  photo_title: string
  taken_at?: string
  width: number
  height: number
  orientation: number
  subject_uid: string
  subject_name: string
  markers: DuplicateMarker[]
}

/** One page of findings plus the pagination cursor. */
export interface DuplicateMarkersResponse {
  groups: DuplicateMarkerGroup[]
  total: number
  limit: number
  offset: number
  next_offset: number | null
}

/** Query parameters for {@link fetchDuplicateMarkers}. */
export interface DuplicateMarkersParams {
  limit?: number
  offset?: number
}

/** What "keep this one" did: which marker survived and which lost their person. */
export interface KeepMarkerResult {
  photo_uid: string
  subject_uid: string
  keep_marker_uid: string
  detached: string[]
}

/**
 * Fetches one page of repeated-marker findings from
 * `GET /api/v1/duplicate-markers`, worst (most markers) first. Throws an
 * {@link ApiError} carrying the HTTP status on failure (503 when the review is
 * not wired server-side).
 */
export async function fetchDuplicateMarkers(
  params: DuplicateMarkersParams = {},
  signal?: AbortSignal,
): Promise<DuplicateMarkersResponse> {
  const query = new URLSearchParams()
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    query.set('offset', String(params.offset))
  }
  const res = await fetch(`${API_BASE}/duplicate-markers?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as DuplicateMarkersResponse
}

/**
 * Resolves one finding via `POST /api/v1/duplicate-markers/keep`: the named
 * marker stays with the person and every other valid face marker of theirs on
 * that photo loses its subject — but survives as a region, because on a group
 * shot it usually belongs to somebody else.
 *
 * The losing markers are not sent: the server resolves the group from (photo,
 * subject) itself, so a list that went stale cannot detach the wrong box.
 */
export async function keepMarker(
  input: { photo_uid: string; subject_uid: string; keep_marker_uid: string },
  signal?: AbortSignal,
): Promise<KeepMarkerResult> {
  const res = await fetch(`${API_BASE}/duplicate-markers/keep`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as KeepMarkerResult
}

/**
 * Flags one marker as holding no face at all via
 * `POST /api/v1/duplicate-markers/invalid`. The marker is not deleted — only the
 * flag changes, which is enough for every listing that means "a real face" to
 * skip it.
 */
export async function invalidateMarker(markerUid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/duplicate-markers/invalid`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ marker_uid: markerUid }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}
