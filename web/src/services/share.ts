import { SHARE_PREVIEW_SIZES, type ShareManifestFile } from '../lib/photoShare'

import { ApiError } from './auth'
import { downloadUrl, thumbUrl } from './photos'

/**
 * Client of everything sharing photos out of the library needs from the backend:
 * `POST /api/v1/photos/share-manifest` (what a selection is as files) and the
 * fetches that turn one of those entries into a `File` the phone's share sheet can
 * take.
 *
 * Both fetches ask the media routes to **stream** (`proxy=true`). The page has to
 * read these bytes, and on a deployment with originals in the object store the
 * plain routes answer with a redirect to another origin — which `fetch` follows and
 * then refuses to let the page read. That redirect is the most likely way this
 * feature would silently fail, so the streaming is explicit here rather than
 * assumed.
 */

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

/** Response body of `POST /api/v1/photos/share-manifest`. */
interface ShareManifestResponse {
  files: ShareManifestFile[]
}

/**
 * Asks what a selection looks like as files, in the order the UIDs were given
 * (`POST /api/v1/photos/share-manifest`).
 *
 * The page holds UIDs, not photos — a selection outlives the grid rows it was made
 * from — so this one request is what supplies the names, types and sizes the
 * batching needs. A UID the catalogue no longer knows is simply absent from the
 * answer.
 *
 * @throws ApiError on a non-OK response, notably 413 when the selection is over
 *   the per-share cap, so the caller can say so specifically.
 */
export async function fetchShareManifest(
  photoUids: string[],
  signal?: AbortSignal,
): Promise<ShareManifestFile[]> {
  const res = await fetch(`${API_BASE}/photos/share-manifest`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ photo_uids: photoUids }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as ShareManifestResponse
  return body.files
}

/**
 * The addresses to try for one manifest entry, in order. An original has exactly
 * one; a preview has one per cached size, largest first, because the biggest
 * preview is the one worth having in a phone library and a smaller one is still
 * far better than sharing nothing.
 */
function shareSourceUrls(file: ShareManifestFile): string[] {
  if (!file.preview) {
    return [downloadUrl(file.uid, { original: true, proxy: true })]
  }
  return SHARE_PREVIEW_SIZES.map((size) => thumbUrl(file.uid, size, null, { proxy: true }))
}

/**
 * Fetches one manifest entry's bytes and wraps them in a `File` named and typed as
 * the manifest says — which is what makes the photo arrive in Apple/Google Photos
 * under its own name instead of as an anonymous blob.
 *
 * A preview falls back down the size chain: a size the library never generated (or
 * cannot generate for this RAW) is a failed response, not a reason to give up on
 * the photo. Only when every address fails does the fetch fail, and then the error
 * names nothing but the status — the caller knows which photo it asked for.
 *
 * @throws ApiError when no address answered.
 */
export async function fetchShareFile(file: ShareManifestFile, signal?: AbortSignal): Promise<File> {
  let last: ApiError | undefined
  for (const url of shareSourceUrls(file)) {
    const res = await fetch(url, { credentials: 'same-origin', signal })
    if (!res.ok) {
      last = new ApiError(res.status, await readErrorMessage(res))
      continue
    }
    const blob = await res.blob()
    return new File([blob], file.name, { type: file.mime === '' ? blob.type : file.mime })
  }
  throw last ?? new ApiError(0, 'no source for this photo')
}
