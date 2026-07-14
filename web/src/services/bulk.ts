import { ApiError } from './auth'

/**
 * Bulk-metadata client for `POST /api/v1/photos/bulk` (`internal/bulkapi`):
 * applies one operation set to many photos in a single transaction. The UI uses
 * it from the grid-selection bulk-edit toolbar to add/remove albums and labels,
 * set or clear the description and location, change the archive state and toggle
 * the per-user favorite — all in one call — and to render the per-photo
 * result summary the endpoint returns.
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

/** A coordinate pair for a `set_location` bulk operation. */
export interface BulkLocation {
  lat: number
  lng: number
}

/**
 * The bulk operations the grid-selection toolbar can apply to many photos at
 * once, mirroring `internal/bulkapi` (`operationsInput`). Every field is
 * optional; omitted operations are left unchanged. Set/clear pairs are distinct
 * keys (matching the wire format): `set_*` carries a value, `clear_*` is a flag,
 * and supplying both of a pair — or both `archive` and `unarchive` — is rejected
 * by the backend with a 400. `set_favorite` is per-user (the acting user).
 */
export interface BulkOperations {
  add_to_albums?: string[]
  remove_from_albums?: string[]
  add_labels?: string[]
  remove_labels?: string[]
  /** Set the title/caption to this value. */
  set_caption?: string
  /** Clear the title/caption. */
  clear_caption?: boolean
  /** Set the description to this value. */
  set_description?: string
  /** Clear the description. */
  clear_description?: boolean
  /** Set the GPS location. */
  set_location?: BulkLocation
  /** Clear the GPS location. */
  clear_location?: boolean
  /** Archive (soft-delete) the photos. */
  archive?: boolean
  /** Unarchive the photos. */
  unarchive?: boolean
  /** Favorite (true) or unfavorite (false) for the acting user. */
  set_favorite?: boolean
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
