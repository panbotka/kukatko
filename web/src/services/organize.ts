import { ApiError } from './auth'

/**
 * Organisation client for albums and labels, mirroring the backend JSON shapes
 * from `internal/organize` and `internal/organizeapi`. The session cookie is sent
 * automatically (same-origin); every call throws {@link ApiError} on a non-OK
 * response so callers can branch on `status`.
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

/** Issues a GET and parses the JSON body, throwing ApiError on a non-OK status. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/**
 * Issues a body-carrying request (POST/PATCH/DELETE) and parses the JSON body,
 * throwing ApiError on a non-OK status. A 204 (or otherwise empty) response
 * resolves to `undefined`, so callers expecting no content can ignore the result.
 */
async function sendJSON<T>(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  return (text === '' ? undefined : JSON.parse(text)) as T
}

/** Album classification, mirroring the backend `organize.AlbumType`. */
export type AlbumType = 'album' | 'folder' | 'moment' | 'state' | 'month'

/** A named, ordered grouping of photos (`organize.Album`). */
export interface Album {
  uid: string
  slug: string
  title: string
  description: string
  type: AlbumType
  cover_photo_uid?: string
  private: boolean
  order_by: string
  created_by?: string
  created_at: string
  updated_at: string
}

/** An album paired with its photo count (`organize.AlbumCount`). */
export interface AlbumCount extends Album {
  photo_count: number
}

/**
 * Editable album fields sent to create (`POST /albums`) and update
 * (`PATCH /albums/{uid}`). `type` is only honoured on create — the backend
 * preserves the structural type on update. A `null` cover clears it.
 */
export interface AlbumInput {
  title: string
  description: string
  type?: AlbumType
  cover_photo_uid?: string | null
  private: boolean
  order_by: string
}

/** Response body of `GET /api/v1/albums`. */
interface AlbumsResponse {
  albums: AlbumCount[]
}

/** Response body of the album membership endpoints: the photos in display order. */
interface PhotoUIDsResponse {
  photo_uids: string[]
}

/** Lists every album with its photo count and cover, ordered by title. */
export async function fetchAlbums(signal?: AbortSignal): Promise<AlbumCount[]> {
  const body = await getJSON<AlbumsResponse>('/albums', signal)
  return body.albums
}

/** Fetches one album by UID; throws ApiError 404 when missing. */
export async function fetchAlbum(uid: string, signal?: AbortSignal): Promise<Album> {
  return getJSON<Album>(`/albums/${encodeURIComponent(uid)}`, signal)
}

/** Creates an album from the editable fields and returns the stored record. */
export async function createAlbum(input: AlbumInput, signal?: AbortSignal): Promise<Album> {
  return sendJSON<Album>('POST', '/albums', input, signal)
}

/** Updates an album's editable fields and returns the refreshed record. */
export async function updateAlbum(
  uid: string,
  input: AlbumInput,
  signal?: AbortSignal,
): Promise<Album> {
  return sendJSON<Album>('PATCH', `/albums/${encodeURIComponent(uid)}`, input, signal)
}

/** Deletes an album; its membership rows are removed server-side. */
export async function deleteAlbum(uid: string, signal?: AbortSignal): Promise<void> {
  await sendJSON<undefined>('DELETE', `/albums/${encodeURIComponent(uid)}`, undefined, signal)
}

/** Appends photos to an album and returns the refreshed display order. */
export async function addAlbumPhotos(
  uid: string,
  photoUids: string[],
  signal?: AbortSignal,
): Promise<string[]> {
  const body = await sendJSON<PhotoUIDsResponse>(
    'POST',
    `/albums/${encodeURIComponent(uid)}/photos`,
    { photo_uids: photoUids },
    signal,
  )
  return body.photo_uids
}

/** Removes photos from an album and returns the refreshed display order. */
export async function removeAlbumPhotos(
  uid: string,
  photoUids: string[],
  signal?: AbortSignal,
): Promise<string[]> {
  const body = await sendJSON<PhotoUIDsResponse>(
    'DELETE',
    `/albums/${encodeURIComponent(uid)}/photos`,
    { photo_uids: photoUids },
    signal,
  )
  return body.photo_uids
}

/** Reorders an album's photos to match `photoUids` and returns the new order. */
export async function reorderAlbumPhotos(
  uid: string,
  photoUids: string[],
  signal?: AbortSignal,
): Promise<string[]> {
  const body = await sendJSON<PhotoUIDsResponse>(
    'PATCH',
    `/albums/${encodeURIComponent(uid)}/order`,
    { photo_uids: photoUids },
    signal,
  )
  return body.photo_uids
}

/** A tag attachable to photos (`organize.Label`). */
export interface Label {
  uid: string
  slug: string
  name: string
  priority: number
  created_at: string
  updated_at: string
}

/** A label paired with how many photos carry it (`organize.LabelCount`). */
export interface LabelCount extends Label {
  photo_count: number
}

/** Editable label fields sent to create (`POST /labels`) and update. */
export interface LabelInput {
  name: string
  priority: number
}

/** Response body of `GET /api/v1/labels`. */
interface LabelsResponse {
  labels: LabelCount[]
}

/** Lists every label with its photo count, ordered by priority (highest first). */
export async function fetchLabels(signal?: AbortSignal): Promise<LabelCount[]> {
  const body = await getJSON<LabelsResponse>('/labels', signal)
  return body.labels
}

/** Fetches one label by UID; throws ApiError 404 when missing. */
export async function fetchLabel(uid: string, signal?: AbortSignal): Promise<Label> {
  return getJSON<Label>(`/labels/${encodeURIComponent(uid)}`, signal)
}

/** Creates a label from the editable fields and returns the stored record. */
export async function createLabel(input: LabelInput, signal?: AbortSignal): Promise<Label> {
  return sendJSON<Label>('POST', '/labels', input, signal)
}

/** Updates a label's editable fields and returns the refreshed record. */
export async function updateLabel(
  uid: string,
  input: LabelInput,
  signal?: AbortSignal,
): Promise<Label> {
  return sendJSON<Label>('PATCH', `/labels/${encodeURIComponent(uid)}`, input, signal)
}

/** Deletes a label; its attachments are removed server-side. */
export async function deleteLabel(uid: string, signal?: AbortSignal): Promise<void> {
  await sendJSON<undefined>('DELETE', `/labels/${encodeURIComponent(uid)}`, undefined, signal)
}

/** Attaches a label to a single photo (idempotent). */
export async function attachLabel(
  uid: string,
  photoUid: string,
  signal?: AbortSignal,
): Promise<void> {
  await sendJSON<undefined>(
    'POST',
    `/labels/${encodeURIComponent(uid)}/photos`,
    { photo_uid: photoUid },
    signal,
  )
}

/** Detaches a label from a single photo (idempotent). */
export async function detachLabel(
  uid: string,
  photoUid: string,
  signal?: AbortSignal,
): Promise<void> {
  await sendJSON<undefined>(
    'DELETE',
    `/labels/${encodeURIComponent(uid)}/photos`,
    { photo_uid: photoUid },
    signal,
  )
}
