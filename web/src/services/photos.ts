import { ApiError } from './auth'

/**
 * A photo in the catalogue, mirroring the backend `photos.Photo` JSON shape
 * (`internal/photos/models.go`). Only the fields the library grid needs are
 * declared explicitly; the rest are intentionally omitted to keep the type
 * focused — extend it as later views require more metadata.
 */
export interface Photo {
  uid: string
  file_hash: string
  file_name: string
  file_size: number
  file_mime: string
  file_width: number
  file_height: number
  taken_at?: string
  taken_at_source: string
  title: string
  description: string
  lat?: number
  lng?: number
  camera_make: string
  camera_model: string
  lens_model: string
  private: boolean
  archived_at?: string
  created_at: string
  updated_at: string
}

/**
 * Response body of `GET /api/v1/photos`. `next_offset` is the offset to request
 * for the following page, or `null` when the current page is the last one —
 * letting an infinite-scroll client page until it is absent.
 */
export interface PhotoListResponse {
  photos: Photo[]
  total: number
  limit: number
  offset: number
  next_offset: number | null
}

/** Public sort aliases accepted by the list endpoint (`internal/photoapi`). */
export type PhotoSort = 'newest' | 'oldest' | 'added' | 'title' | 'size'

/** Archive-state selector: hide archived (default), include them, or only them. */
export type ArchivedFilter = 'false' | 'true' | 'only'

/**
 * Filters, sort and pagination for a photo list request. Empty strings and
 * `undefined` values are treated as "no filter" and omitted from the query, so
 * the same shape works for both the URL-encoded view state and direct calls.
 */
export interface PhotoListParams {
  limit?: number
  offset?: number
  sort?: PhotoSort
  archived?: ArchivedFilter
  /** Tri-state filter: 'true', 'false', or '' / undefined for no filter. */
  has_gps?: string
  /** Tri-state filter: 'true', 'false', or '' / undefined for no filter. */
  private?: string
  camera?: string
  q?: string
  /** RFC3339 timestamp or YYYY-MM-DD date. */
  taken_after?: string
  /** RFC3339 timestamp or YYYY-MM-DD date. */
  taken_before?: string
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/photoapi/http.go`). */
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
 * Serialises list params into a query string, omitting empty / undefined values
 * so absent filters never reach the backend (which would otherwise treat an
 * empty value as no filter anyway, but a minimal query keeps requests tidy and
 * cache-friendly).
 */
export function buildPhotoQuery(params: PhotoListParams): URLSearchParams {
  const query = new URLSearchParams()
  const set = (key: string, value: string | number | undefined): void => {
    if (value === undefined) {
      return
    }
    const str = String(value)
    if (str !== '') {
      query.set(key, str)
    }
  }
  set('limit', params.limit)
  set('offset', params.offset)
  set('sort', params.sort)
  set('archived', params.archived)
  set('has_gps', params.has_gps)
  set('private', params.private)
  set('camera', params.camera)
  set('q', params.q)
  set('taken_after', params.taken_after)
  set('taken_before', params.taken_before)
  return query
}

/**
 * Fetches a page of photos from `GET /api/v1/photos`. The session cookie is sent
 * automatically (same-origin); the backend filters, sorts and paginates.
 *
 * @throws ApiError with `status` 400 (invalid filter/sort/page) or 5xx so the
 *   caller can render the matching message.
 */
export async function fetchPhotos(
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<PhotoListResponse> {
  const query = buildPhotoQuery(params)
  const res = await fetch(`${API_BASE}/photos?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoListResponse
}

/**
 * Builds the URL of a photo's cached thumbnail at the given size (for example
 * `tile_500`). The media endpoint accepts the session cookie sent by the browser
 * for same-origin `<img>` requests; an optional download token is appended for
 * cookie-less contexts.
 */
export function thumbUrl(uid: string, size: string, downloadToken?: string | null): string {
  const url = `${API_BASE}/photos/${encodeURIComponent(uid)}/thumb/${encodeURIComponent(size)}`
  if (downloadToken !== undefined && downloadToken !== null && downloadToken !== '') {
    return `${url}?t=${encodeURIComponent(downloadToken)}`
  }
  return url
}

/** Thumbnail size used for library grid tiles — a square crop, high enough DPI. */
export const GRID_THUMB_SIZE = 'tile_500'
