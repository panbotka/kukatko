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
  /** Capture notes (private annotations); present on detail responses. */
  notes?: string
  /** EXIF capture settings, present when the original carried them. */
  iso?: number
  aperture?: number
  exposure?: string
  focal_length?: number
  /** GPS altitude in metres, when geotagged with elevation. */
  altitude?: number
  /** Media kind: `image`, `video` or `live`. Absent is treated as `image`. */
  media_type?: string
  /** Clip length in milliseconds for videos/live photos; absent for images. */
  duration_ms?: number
  /** Primary video codec (e.g. `h264`, `hevc`); empty/absent for images. */
  video_codec?: string
  /** Primary audio codec (e.g. `aac`); empty/absent when there is no audio. */
  audio_codec?: string
  /** Whether the video carries an audio stream. */
  has_audio?: boolean
  /** Average frame rate of the video; absent for images. */
  fps?: number
  private: boolean
  archived_at?: string
  created_at: string
  updated_at: string
  /**
   * Whether the current user has favorited this photo. Present on list, search
   * and detail responses (`internal/photoapi` annotates each photo for the
   * acting user); absent (treated as false) when the favorites backend is
   * unwired.
   */
  is_favorite?: boolean
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
  /** Effective search mode, only present on `GET /search` responses. */
  mode?: SearchMode
  /**
   * True when a semantic or hybrid search fell back to full-text because the
   * embeddings sidecar was unavailable, so the UI can tell the user semantic
   * ranking was skipped. Absent (treated as false) on list responses.
   */
  degraded?: boolean
}

/**
 * Search ranking mode for `GET /search` (`internal/photoapi`): `fulltext` ranks
 * by Czech-aware full-text relevance, `semantic` by CLIP vector similarity to the
 * embedded query, and `hybrid` (the default) fuses the two with Reciprocal Rank
 * Fusion.
 */
export type SearchMode = 'fulltext' | 'semantic' | 'hybrid'

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
  /** Scope the listing to photos in this album (`album` query param). */
  album?: string
  /** Scope the listing to photos carrying this label (`label` query param). */
  label?: string
  /**
   * Scope the listing to the current user's favorites when set to `'true'`
   * (`favorite` query param). Any other value / undefined means no scope.
   */
  favorite?: string
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
  set('album', params.album)
  set('label', params.label)
  set('favorite', params.favorite)
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
 * Searches the catalogue via `GET /api/v1/search`. `params.q` is the search text
 * (required by the backend; an empty value yields a 400). `mode` selects the
 * ranking strategy (default `hybrid`); list filters and pagination in `params`
 * apply in every mode. The response mirrors {@link fetchPhotos} and additionally
 * carries the effective `mode` and a `degraded` flag set when a semantic/hybrid
 * search fell back to full-text because the sidecar was offline.
 *
 * @throws ApiError with `status` 400 (missing query / invalid filter) or 5xx so
 *   the caller can render the matching message.
 */
export async function searchPhotos(
  params: PhotoListParams,
  mode?: SearchMode,
  signal?: AbortSignal,
): Promise<PhotoListResponse> {
  const query = buildPhotoQuery(params)
  if (mode !== undefined) {
    query.set('mode', mode)
  }
  const res = await fetch(`${API_BASE}/search?${query.toString()}`, {
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
 * A primary or sidecar file backing a photo (`photos.PhotoFile`). Only the
 * fields the detail view needs are declared.
 */
export interface PhotoFile {
  id: number
  role: string
  is_primary: boolean
  file_mime: string
  file_size: number
}

/** A compact album reference on a photo detail response (an inline chip). */
export interface PhotoAlbumRef {
  uid: string
  title: string
}

/** A compact label reference on a photo detail response (an inline chip). */
export interface PhotoLabelRef {
  uid: string
  name: string
}

/**
 * Full photo detail (`internal/photoapi` detail handler): a photo plus its
 * files and its album/label memberships (empty arrays when none).
 */
export interface PhotoDetail extends Photo {
  files: PhotoFile[]
  albums: PhotoAlbumRef[]
  labels: PhotoLabelRef[]
}

/**
 * Partial metadata update for `PATCH /api/v1/photos/{uid}`. An omitted key leaves
 * the field unchanged; `null` clears a nullable field (`taken_at`, `lat`, `lng`).
 * Mirrors the backend `updateBody`.
 */
export interface PhotoMetadataUpdate {
  title?: string
  description?: string
  notes?: string
  taken_at?: string | null
  lat?: number | null
  lng?: number | null
  private?: boolean
}

/**
 * Applies a partial metadata update to a photo via `PATCH /api/v1/photos/{uid}`
 * and returns the refreshed detail. Editor/admin only.
 *
 * @throws ApiError with `status` 400 (invalid field/coordinate), 403 (viewer),
 *   404 (no such photo) or 5xx.
 */
export async function updatePhoto(
  uid: string,
  patch: PhotoMetadataUpdate,
  signal?: AbortSignal,
): Promise<PhotoDetail> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}`, {
    method: 'PATCH',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * The non-destructive edit stored for a photo (`photo_edits`): an optional
 * normalised crop rectangle (all four set together or all absent), a rotation of
 * 0/90/180/270 degrees, and brightness/contrast each neutral at 0 and meaningful
 * in [-1, 1]. Mirrors the backend `photos.Edit`.
 */
export interface PhotoEdit {
  photo_uid?: string
  crop_x?: number
  crop_y?: number
  crop_w?: number
  crop_h?: number
  rotation: number
  brightness: number
  contrast: number
  updated_at?: string
}

/**
 * Fetches the stored non-destructive edit for a photo via
 * `GET /api/v1/photos/{uid}/edit`. An unedited photo returns a neutral edit
 * (rotation 0, brightness/contrast 0, no crop), so the caller always has a value.
 *
 * @throws ApiError with `status` 404 (no such photo) or 5xx.
 */
export async function fetchEdit(uid: string, signal?: AbortSignal): Promise<PhotoEdit> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/edit`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoEdit
}

/**
 * Saves the non-destructive edit for a photo via `PUT /api/v1/photos/{uid}/edit`
 * and returns the persisted edit. The original file is never modified; downloads
 * honour the edit. Editor/admin only.
 *
 * @throws ApiError with `status` 400 (invalid edit), 403 (viewer), 404 (no such
 *   photo) or 5xx.
 */
export async function saveEdit(
  uid: string,
  edit: PhotoEdit,
  signal?: AbortSignal,
): Promise<PhotoEdit> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/edit`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(edit),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoEdit
}

/**
 * Builds the URL of a photo's download (`GET /api/v1/photos/{uid}/download`). By
 * default the download honours any saved edit; pass `original: true` for the
 * untouched original bytes. A download token can be appended for cookie-less
 * contexts.
 */
export function downloadUrl(
  uid: string,
  options: { original?: boolean; token?: string | null } = {},
): string {
  const query = new URLSearchParams()
  if (options.original === true) {
    query.set('original', 'true')
  }
  if (options.token !== undefined && options.token !== null && options.token !== '') {
    query.set('t', options.token)
  }
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  return `${API_BASE}/photos/${encodeURIComponent(uid)}/download${suffix}`
}

/**
 * Fetches one photo's full detail via `GET /api/v1/photos/{uid}`.
 *
 * @throws ApiError with `status` 404 (no such photo) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchPhoto(uid: string, signal?: AbortSignal): Promise<PhotoDetail> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PhotoDetail
}

/**
 * One entry in the similar-photos response (`internal/photoapi/similar.go`): a
 * full photo record plus its cosine `distance` to the source photo (smaller is
 * closer / more similar).
 */
export interface SimilarPhoto extends Photo {
  distance: number
}

/** Response body of `GET /api/v1/photos/{uid}/similar`. */
export interface SimilarResponse {
  similar: SimilarPhoto[]
}

/**
 * Fetches the photos most visually similar to `uid` via
 * `GET /api/v1/photos/{uid}/similar`, ordered nearest-first and excluding the
 * source photo. The endpoint is empty-friendly: a photo that has not been
 * embedded yet (or a server with no search backend) yields an empty list with
 * 200, so an empty array is a normal result, not an error.
 *
 * @param limit optional cap on the number of neighbours (backend default 24,
 *   max 100); omitted values use the backend default.
 * @throws ApiError with `status` 404 (no such photo) or 5xx so the caller can
 *   render the matching message.
 */
export async function fetchSimilar(
  uid: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<SimilarPhoto[]> {
  const query = new URLSearchParams()
  if (limit !== undefined) {
    query.set('limit', String(limit))
  }
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/similar${suffix}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as SimilarResponse
  return body.similar
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

/**
 * Builds the URL of a photo's inline video stream
 * (`GET /api/v1/photos/{uid}/video`). The endpoint supports HTTP range requests
 * (seeking) and serves a live photo's motion clip. The browser sends the session
 * cookie for same-origin `<video>` requests; an optional download token is
 * appended for cookie-less contexts.
 */
export function videoUrl(uid: string, downloadToken?: string | null): string {
  const url = `${API_BASE}/photos/${encodeURIComponent(uid)}/video`
  if (downloadToken !== undefined && downloadToken !== null && downloadToken !== '') {
    return `${url}?t=${encodeURIComponent(downloadToken)}`
  }
  return url
}

/**
 * Toggles whether the current user has favorited a photo via
 * `PUT /api/v1/photos/{uid}/favorite` (favorite) or `DELETE …` (unfavorite).
 * Both are idempotent and resolve with no body (204). Favoriting is a personal
 * action available to every signed-in user, including viewers.
 *
 * @throws ApiError with `status` 404 (no such photo), 503 (favorites backend
 *   unwired) or 5xx, so the caller can roll back an optimistic update.
 */
export async function favoritePhoto(
  uid: string,
  favorite: boolean,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/favorite`, {
    method: favorite ? 'PUT' : 'DELETE',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Restores an archived photo via `POST /api/v1/photos/{uid}/unarchive`, clearing
 * its `archived_at` so it leaves the trash and reappears in the library. Editor/
 * admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 403 (not an editor) or 5xx.
 */
export async function unarchivePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/unarchive`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Permanently deletes a single archived photo via
 * `POST /api/v1/photos/{uid}/purge?confirm=true`. This is irreversible: the row,
 * its originals and thumbnails are removed. The explicit `confirm=true` guard
 * mirrors the server's requirement. Editor/admin only.
 *
 * @throws ApiError with `status` 404 (no such photo), 409 (not archived), 503
 *   (purge backend unwired) or 5xx.
 */
export async function purgePhoto(uid: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/photos/${encodeURIComponent(uid)}/purge?confirm=true`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Counts returned by the empty-trash action. */
export interface PurgeResult {
  purged: number
  failed: number
}

/**
 * Permanently deletes every archived photo via
 * `POST /api/v1/trash/empty?confirm=true` and returns how many were purged and
 * failed. Irreversible; editor/admin only.
 *
 * @throws ApiError with `status` 503 (purge backend unwired) or 5xx.
 */
export async function emptyTrash(signal?: AbortSignal): Promise<PurgeResult> {
  const res = await fetch(`${API_BASE}/trash/empty?confirm=true`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PurgeResult
}

/** The trash retention window, used to show each item's auto-purge countdown. */
export interface TrashInfo {
  retention_days: number
}

/**
 * Fetches the trash retention window via `GET /api/v1/trash/info` so the trash
 * UI can compute how long each archived photo has before it is auto-purged.
 *
 * @throws ApiError on a non-2xx response.
 */
export async function fetchTrashInfo(signal?: AbortSignal): Promise<TrashInfo> {
  const res = await fetch(`${API_BASE}/trash/info`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as TrashInfo
}

/** Thumbnail size used for library grid tiles — a square crop, high enough DPI. */
export const GRID_THUMB_SIZE = 'tile_500'
