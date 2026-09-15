import { ApiError, NetworkError } from './auth'

const API_BASE = '/api/v1'

/**
 * Which source currently answers for an account's picture, as the server
 * resolved the chain: an uploaded picture, a photo of the library the user
 * pointed at, the face of the person the account is linked to, or nothing — in
 * which case the coloured initial is drawn.
 */
export type PictureOrigin = 'upload' | 'photo' | 'subject' | 'none'

/** What `GET /auth/picture` reports about the caller's own picture. */
export interface PictureState {
  origin: PictureOrigin
  /** The photo the picture is cut from, for `photo` and `subject`. */
  photo_uid?: string
  /**
   * The person the account is linked to, whenever it is linked — even while an
   * upload or a pick overrides that face, so the page can say what clearing the
   * picture falls back to.
   */
  subject_uid?: string
}

/** The largest upload the server accepts, in bytes; mirrors `userpic.MaxUploadBytes`. */
export const MAX_PICTURE_BYTES = 8 * 1024 * 1024

/** The image types the server decodes, for the file picker's `accept`. */
export const PICTURE_ACCEPT = 'image/jpeg,image/png,image/webp'

/**
 * The URL of one account's picture — a square JPEG, or a 404 when the account
 * has no picture from any source, which is the caller's cue to draw the initial.
 *
 * `version` is a cache-buster and nothing else: the response is cached for ten
 * minutes, so a picture the user has just changed would otherwise keep showing
 * the old one on the very page that changed it. Readers elsewhere want the cache
 * and pass nothing.
 */
export function userAvatarUrl(uid: string, version?: number): string {
  const url = `${API_BASE}/users/${encodeURIComponent(uid)}/avatar`
  return version === undefined ? url : `${url}?v=${String(version)}`
}

/** Reads which source currently answers for the signed-in account. */
export async function fetchMyPicture(signal?: AbortSignal): Promise<PictureState> {
  const res = await request('/auth/picture', { method: 'GET', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as PictureState
}

/**
 * Uploads a picture for the signed-in account. The server re-encodes it to a
 * small square JPEG and keeps only that, so the file itself never leaves the
 * request.
 *
 * The size is checked here as well as on the server — not as a security measure
 * (the server's bound is the real one) but so a user who picked a 30 MB photo is
 * told immediately instead of after uploading it.
 */
export async function uploadMyPicture(file: File, signal?: AbortSignal): Promise<void> {
  if (file.size > MAX_PICTURE_BYTES) {
    throw new ApiError(413, 'the picture is too large')
  }
  const form = new FormData()
  form.append('picture', file, file.name)
  const res = await request('/auth/picture', { method: 'PUT', body: form, signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Points the signed-in account's picture at a photo of the library. The server
 * refuses a photo flagged private or hidden with a 400 — a photo kept out of the
 * library must not appear on a profile, where every reader sees it.
 */
export async function pickMyPicture(photoUid: string, signal?: AbortSignal): Promise<void> {
  const res = await request('/auth/picture', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ photo_uid: photoUid }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Clears the signed-in account's stored picture. It does not necessarily restore
 * the coloured initial: a linked account falls back to that person's face, which
 * is the chain's default rather than a leftover.
 */
export async function clearMyPicture(signal?: AbortSignal): Promise<void> {
  const res = await request('/auth/picture', { method: 'DELETE', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Issues one request, turning a transport failure into a NetworkError. */
async function request(path: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(`${API_BASE}${path}`, { credentials: 'same-origin', ...init })
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new NetworkError(
      error instanceof Error ? error.message : 'the server could not be reached',
      { cause: error },
    )
  }
}

/** Reads the `error` field of a failed response, falling back to its status text. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    return body.error ?? res.statusText
  } catch {
    return res.statusText
  }
}
