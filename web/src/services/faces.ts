import { ApiError } from './auth'
import { type Bbox } from './people'
import { type Photo } from './photos'

/**
 * Candidate-search client for the "find a person among untagged photos" page,
 * mirroring the backend JSON shapes from `internal/candidatesapi` /
 * `internal/candidates`. `POST /subjects/{uid}/candidates` answers "where else does
 * this person appear, unnamed?". It is read-only: confirming a candidate goes
 * through {@link assignFace} (`internal/people` service), rejecting one through the
 * feedback client. The session cookie is sent automatically (same-origin); every
 * call throws {@link ApiError} on a non-OK response so callers can branch on
 * `status`.
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

/** Issues a POST with a JSON body and parses the JSON reply, throwing on non-OK. */
async function postJSON<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/**
 * What confirming a candidate would do (`candidates.Action`), a subset of the
 * on-photo {@link import('./people').FaceAction}: draw a new marker, assign the
 * person to an existing marker, or nothing (already this subject).
 */
export type CandidateAction = 'create_marker' | 'assign_person' | 'already_done'

/**
 * Why a search returned no candidates for a structural (non-error) cause
 * (`candidates.Reason`): the subject has no tagged faces at all, or its faces carry
 * no embedding yet (the sidecar was offline). Empty on a normal result.
 */
export type CandidateReason = 'no_faces' | 'no_embeddings' | ''

/** A candidate face's box in both spaces the UI needs (`candidates.FaceBox`). */
export interface FaceBox {
  /** Display-relative `[x, y, w, h]` in 0..1, already EXIF-oriented. */
  relative: Bbox
  /** The same box in display pixels. */
  pixel: [number, number, number, number]
}

/** One untagged face that resembles the subject (`candidates.Candidate`). */
export interface Candidate {
  /** The owning photo, with media URLs stamped. */
  photo: Photo
  /** Identifies the face within its photo. */
  face_index: number
  /** The face box in relative and pixel space. */
  bbox: FaceBox
  /** Minimum cosine distance to any voting exemplar (nearest wins). */
  distance: number
  /** How many distinct source photos voted for this face. */
  match_count: number
  /** What confirming this candidate would do. */
  action: CandidateAction
  /**
   * The existing marker overlapping this face, when there is one (assign_person /
   * already_done). Absent for an unmarked face (create_marker). The confirm call is
   * routed with it.
   */
  marker_uid?: string
}

/** Candidate tallies per action, for a summary the UI shows without walking the list. */
export interface CandidateCounts {
  create_marker: number
  assign_person: number
  already_done: number
}

/** The per-call search parameters (`candidates.Request`). */
export interface CandidateSearchRequest {
  /** Maximum cosine distance a candidate may sit from an exemplar (0 = default). */
  threshold: number
  /** Caps how many candidates come back; 0 means all. */
  limit: number
}

/** The search outcome for one subject (`candidates.Result`). */
export interface CandidateResult {
  subject_uid: string
  /** Distinct photos that contributed an exemplar (one per photo). */
  source_photo_count: number
  /** How many embedded faces the subject has. */
  source_face_count: number
  /** Marked photos with no embedded face to search from (sidecar was offline). */
  faces_without_embedding: number
  /** The computed vote threshold that was applied. */
  min_match_count: number
  /** The maximum cosine distance actually used (after defaulting). */
  threshold: number
  /** Set when the result is empty for a structural cause; empty otherwise. */
  reason?: CandidateReason
  counts: CandidateCounts
  /** The surviving untagged faces, nearest first. */
  candidates: Candidate[]
}

/**
 * Runs the untagged-face candidate search for a subject via
 * `POST /subjects/{uid}/candidates`. The search is expensive (per-exemplar kNN over
 * every unassigned face), so callers trigger it explicitly rather than on every
 * slider drag.
 */
export async function searchCandidates(
  subjectUid: string,
  req: CandidateSearchRequest,
  signal?: AbortSignal,
): Promise<CandidateResult> {
  return postJSON<CandidateResult>(
    `/subjects/${encodeURIComponent(subjectUid)}/candidates`,
    req,
    signal,
  )
}
