import { ApiError } from './auth'
import { type Photo } from './photos'

/**
 * Collection-expansion client for the /expand page, mirroring the backend JSON
 * shapes from `internal/expandapi` / `internal/expand`. `GET
 * /albums/{uid}/similar` and `GET /labels/{uid}/similar` answer "which photos
 * look like the ones already in this collection, but are not in it yet?". Both
 * are read-only — adding the found photos goes through the existing `POST
 * /photos/bulk`, and rejecting one for a label through the feedback client. The
 * session cookie is sent automatically (same-origin); every call throws
 * {@link ApiError} on a non-OK response.
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

/** Issues a GET and parses the JSON reply, throwing on non-OK. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** Which kind of collection is being expanded. */
export type ExpandKind = 'album' | 'label'

/**
 * Why a search returned no candidates for a structural (non-error) cause
 * (`expand.Reason`): the collection has no photos at all, or it has photos but
 * none of the sampled ones carry an embedding yet.
 */
export type ExpandReason = 'empty_collection' | 'no_source_embeddings'

/**
 * One expansion result (`expand.Candidate`): the photo in the shape the grid
 * consumes (with `thumb_url`/`download_url` stamped), its distance to the
 * nearest agreeing source photo, the similarity the UI shows as a percentage,
 * and how many source photos voted for it.
 */
export interface ExpandCandidate {
  photo: Photo
  /** Minimum cosine distance to any voting source photo. */
  distance: number
  /** 1 - distance, the value shown as the tile's match percentage. */
  similarity: number
  /** How many source photos returned this candidate in their kNN. */
  match_count: number
}

/**
 * The full search response (`expand.Result`): the ranked candidates plus the
 * summary numbers the page uses to explain thin or empty results (a
 * half-embedded collection, an applied source cap, the vote floor).
 */
export interface ExpandResult {
  kind: ExpandKind
  collection_uid: string
  /** Total number of members in the collection. */
  source_photo_count: number
  /** How many members were used as query vectors after the cap. */
  source_photos_sampled: number
  /** How many of the sampled members actually carry an embedding. */
  source_photos_with_embedding: number
  /** Whether the source set was sampled down to the cap. */
  source_capped: boolean
  /** The source cap that was in force. */
  source_cap: number
  /** The vote floor a candidate had to clear. */
  min_match_count: number
  /** The maximum cosine distance that was applied. */
  threshold: number
  /** The effective result cap after defaulting and clamping. */
  limit: number
  /** len(candidates) as the backend counted it. */
  result_count: number
  /** Names why the result is empty, when it is. */
  reason?: ExpandReason
  candidates: ExpandCandidate[]
}

/** Query parameters of an expansion search. */
export interface ExpandSearchRequest {
  /** Maximum cosine distance a candidate may have (smaller = closer). */
  threshold: number
  /** Result cap. */
  limit: number
}

/**
 * Runs the collection-expansion search via `GET /{albums,labels}/{uid}/similar`.
 * The search is expensive (a kNN per source photo), so callers trigger it
 * explicitly rather than on every slider drag.
 */
export async function searchSimilar(
  kind: ExpandKind,
  uid: string,
  req: ExpandSearchRequest,
  signal?: AbortSignal,
): Promise<ExpandResult> {
  const params = new URLSearchParams({
    threshold: String(req.threshold),
    limit: String(req.limit),
  })
  const base = kind === 'album' ? 'albums' : 'labels'
  return getJSON<ExpandResult>(
    `/${base}/${encodeURIComponent(uid)}/similar?${params.toString()}`,
    signal,
  )
}
