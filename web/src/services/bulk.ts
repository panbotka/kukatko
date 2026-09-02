import { type TakenPrecision } from '../lib/takenDate'

import { ApiError } from './auth'

/**
 * Bulk-metadata client for `POST /api/v1/photos/bulk` (`internal/bulkapi`):
 * applies one operation set to many photos in a single transaction. The UI uses
 * it from the grid-selection bulk-edit toolbar to add/remove albums and labels,
 * set or clear the description and location, change the archive state, hide the
 * photos from the library and toggle the per-user favorite — all in one call —
 * and to render the per-photo result summary the endpoint returns.
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
 * A coordinate pair for a `set_location` bulk operation, plus what to do about
 * the photos in the batch that already have one.
 *
 * `only_missing` is the choice a box of scans forces: most of them were never
 * near a GPS, a few came off a phone and know exactly where they were. Left out
 * (or false) the pin replaces every selected photo's location; true fills in the
 * empty ones and reports the rest as skipped, keeping coordinates that are
 * better evidence than a pin dropped from memory.
 */
export interface BulkLocation {
  lat: number
  lng: number
  only_missing?: boolean
}

/**
 * What `POST /photos/bulk/location-summary` answers about a selection: how many
 * of its photos exist and how many of those already carry coordinates. It is
 * what lets the set-location dialog say what an overwrite would cost before
 * anything is written — a repeated UID counts once and a photo that is gone
 * counts not at all, so the total is the batch the apply would really see.
 */
export interface BulkLocationSummary {
  total: number
  with_location: number
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
  /** Set the capture date of every photo at a stated grain. */
  set_taken_at?: BulkTakenAt
  /**
   * Declare the capture date unknown: the date goes, and the provenance records
   * that somebody said so rather than that nothing was ever known. The outgoing
   * date is not lost — the backend puts it away in `taken_at_before_unknown`, so
   * the photo's detail can show what was disowned and put it back. Supplying it
   * together with `set_taken_at` is a 400.
   */
  clear_taken_at?: boolean
  /** Set the GPS location. */
  set_location?: BulkLocation
  /** Clear the GPS location. */
  clear_location?: boolean
  /** Archive (soft-delete) the photos. */
  archive?: boolean
  /** Unarchive the photos. */
  unarchive?: boolean
  /**
   * Hide the photos from the library: out of the grid, the timeline, the map and
   * the default search, still visible in their albums and labels. Not archiving
   * — nothing is deleted. Supplying both `hide` and `unhide` is a 400.
   */
  hide?: boolean
  /** Bring the hidden photos back into the library. */
  unhide?: boolean
  /** Favorite (true) or unfavorite (false) for the acting user. */
  set_favorite?: boolean
}

/**
 * A capture date for a `set_taken_at` bulk operation. The value's shape follows
 * the precision, so the two can never disagree about how much of a date was
 * actually stated: `1974-06-14` for a day, `1974-06` for a month, `1974` for a
 * year and the decade's first year (`1970`) for a decade. There is no time of
 * day — the operation exists for scans, where the hour is never known.
 *
 * The backend stores the first instant of the stated period in UTC and records
 * the precision beside it, so the photos sort and filter into that period while
 * nothing shows a day nobody claimed. It is catalogue metadata only: the
 * original files and their EXIF are never touched.
 */
export interface BulkTakenAt {
  precision: TakenPrecision
  value: string
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
 * Reads how many of `photoUids` already have a location, via
 * `POST /photos/bulk/location-summary`. A POST for a read: the argument is the
 * whole selection, which belongs in a body rather than in a query string.
 * Throws {@link ApiError} on a rejected or failed request.
 */
export async function fetchBulkLocationSummary(
  photoUids: string[],
  signal?: AbortSignal,
): Promise<BulkLocationSummary> {
  const res = await fetch(`${API_BASE}/photos/bulk/location-summary`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ photo_uids: photoUids }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as BulkLocationSummary
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
