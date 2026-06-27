import { ApiError } from './auth'

/**
 * Bulk-metadata client for `POST /api/v1/photos/bulk` (`internal/bulkapi`):
 * applies one operation set to many photos in a single transaction. The UI uses
 * it to add a multi-photo grid selection to albums or labels in one call; the
 * full operation set is intentionally not modelled here — only the fields the
 * selection affordances need are declared.
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

/**
 * The subset of bulk operations the grid-selection affordances use: adding the
 * selected photos to albums and/or attaching labels. Every field is optional;
 * omitted operations are not applied.
 */
export interface BulkOperations {
  add_to_albums?: string[]
  remove_from_albums?: string[]
  add_labels?: string[]
  remove_labels?: string[]
}

/** Per-photo outcome of a bulk apply (`bulk.PhotoResult`). */
export interface BulkPhotoResult {
  photo_uid: string
  status: string
  error?: string
}

/** Aggregate counts of a bulk apply (`bulk.Counts`). */
export interface BulkCounts {
  total: number
  updated: number
  skipped: number
  errored: number
}

/** Response body of `POST /api/v1/photos/bulk` (`bulk.Result`). */
export interface BulkResult {
  results: BulkPhotoResult[]
  counts: BulkCounts
}

/**
 * Applies `operations` to `photoUids` via `POST /photos/bulk`. Per-photo errors
 * are reported in the result body with a 200; only a validation or server error
 * throws {@link ApiError} (400 for a bad operation/conflict, 413 for too large a
 * batch, 5xx otherwise).
 */
export async function bulkUpdatePhotos(
  photoUids: string[],
  operations: BulkOperations,
  signal?: AbortSignal,
): Promise<BulkResult> {
  const res = await fetch(`${API_BASE}/photos/bulk`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ photo_uids: photoUids, operations }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as BulkResult
}
