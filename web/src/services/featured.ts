import { ApiError } from './auth'
import { type Album } from './organize'

/**
 * Client for the homepage's featured albums ("Doporučujeme"), mirroring
 * `internal/organizeapi/featured.go`. It is a module of its own rather than
 * more of `services/organize` so the homepage strip — and a later kind of
 * featured entity, such as labels — depends on nothing but this surface.
 * Every call throws {@link ApiError} on a non-OK response.
 */

/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * One album on the homepage strip (`organize.FeaturedAlbum` + `thumb_url`).
 * `featured_until` is always set here — only running features are listed.
 * `cover_uids` are the album index's own cover candidates for the album (see
 * `AlbumSummary`), so the strip draws the same collage as the album's tile;
 * they, `thumb_url` and `cover_uid` are absent for an album with nothing to
 * show. `thumb_url` and `cover_blurhash` belong to `cover_uid`, the single
 * cover; `cover_blurhash` is absent for a photo catalogued before placeholders
 * existed.
 */
export interface FeaturedAlbum extends Album {
  featured_until: string
  featured_at: string
  photo_count: number
  cover_uid?: string
  cover_uids?: string[]
  cover_blurhash?: string
  thumb_url?: string
}

/**
 * Body of `GET /albums/featured`: the strip plus the bounds the feature dialog
 * pre-fills and caps its date from (`albums.featured.default_days`, the server's
 * one-year cap).
 */
export interface FeaturedAlbums {
  albums: FeaturedAlbum[]
  default_days: number
  max_days: number
}

/** Extracts the backend error message from a non-OK response, if present. */
async function errorOf(res: Response): Promise<ApiError> {
  let message = res.statusText || `request failed: ${res.status}`
  try {
    const body = (await res.json()) as { error?: string }
    if (typeof body.error === 'string' && body.error !== '') {
      message = body.error
    }
  } catch {
    // Body was empty or not JSON; keep the status text.
  }
  return new ApiError(res.status, message)
}

/** Issues a request and parses its JSON body, throwing ApiError on a non-OK status. */
async function request<T>(
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
    throw await errorOf(res)
  }
  return (await res.json()) as T
}

/**
 * Lists the albums featured on the homepage right now, newest feature first.
 * The server leaves private albums out for a caller who does not curate.
 */
export async function fetchFeaturedAlbums(signal?: AbortSignal): Promise<FeaturedAlbums> {
  return request<FeaturedAlbums>('GET', '/albums/featured', undefined, signal)
}

/**
 * Features an album on the homepage until `until` (an RFC 3339 instant), or
 * for the server's default length when omitted. Featuring an already featured
 * album re-dates it — that is how a feature is extended. Returns the album.
 */
export async function featureAlbum(
  uid: string,
  until?: string,
  signal?: AbortSignal,
): Promise<Album> {
  return request<Album>(
    'PUT',
    `/albums/${encodeURIComponent(uid)}/featured`,
    until === undefined ? {} : { until },
    signal,
  )
}

/** Takes an album off the homepage and returns the refreshed album. */
export async function unfeatureAlbum(uid: string, signal?: AbortSignal): Promise<Album> {
  return request<Album>('DELETE', `/albums/${encodeURIComponent(uid)}/featured`, undefined, signal)
}
