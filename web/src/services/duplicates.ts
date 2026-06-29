import { ApiError } from './auth'

const API_BASE = '/api/v1'

/** Why a group's members were detected as duplicates of each other. */
export type DuplicateReason = 'phash' | 'embedding' | 'both'

/** One photo within a duplicate group, with the fields needed to compare it. */
export interface DuplicateMember {
  uid: string
  title: string
  file_name: string
  file_width: number
  file_height: number
  file_size: number
  media_type: string
  taken_at?: string
  is_keeper: boolean
  /** pHash Hamming distance to the suggested keeper (absent on the keeper). */
  phash_distance?: number
  /** Embedding cosine distance to the suggested keeper, when linked that way. */
  embedding_distance?: number
}

/** A set of photos detected as likely duplicates, with a suggested keeper. */
export interface DuplicateGroup {
  id: string
  reason: DuplicateReason
  keeper_uid: string
  members: DuplicateMember[]
}

/** One page of duplicate groups plus the pagination cursor. */
export interface DuplicatesResponse {
  groups: DuplicateGroup[]
  total: number
  limit: number
  offset: number
  next_offset: number | null
}

/** Query parameters for {@link fetchDuplicates}. */
export interface DuplicatesParams {
  limit?: number
  offset?: number
}

interface ErrorBody {
  error?: string
}

/** Extracts a server error message from a failed response, with a fallback. */
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

/**
 * Fetches one page of duplicate groups from `GET /api/v1/duplicates`. Throws an
 * {@link ApiError} carrying the HTTP status on failure (503 when detection is
 * disabled server-side).
 */
export async function fetchDuplicates(
  params: DuplicatesParams,
  signal?: AbortSignal,
): Promise<DuplicatesResponse> {
  const query = new URLSearchParams()
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    query.set('offset', String(params.offset))
  }
  const res = await fetch(`${API_BASE}/duplicates?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as DuplicatesResponse
}
