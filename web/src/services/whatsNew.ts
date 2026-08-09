/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/** A newly created album the digest links to. */
export interface WhatsNewAlbum {
  uid: string
  title: string
}

/** A newly named person the digest links to. */
export interface WhatsNewPerson {
  uid: string
  name: string
}

/**
 * The digest of what happened in the library since the caller's previous visit,
 * as returned by `GET /api/v1/whats-new` (`whatsnew.Summary`).
 *
 * `has_news` is the only flag a client branches on: it is false both for a
 * first-ever visit — which has no reference point yet — and for a visit that
 * found nothing, and in both cases no panel is shown. Every other field is
 * absent or zero in that case.
 *
 * `albums` and `people` are capped server-side, while `album_count` and
 * `person_count` report the true totals, so the panel can honestly say "8 new
 * people" while linking only the first few.
 */
export interface WhatsNew {
  has_news: boolean
  /**
   * The reference point of this visit (RFC 3339). It stays constant for as long
   * as the visit lasts, which is what makes it a stable key for the dismissal:
   * dismissing the panel hides *this* digest, and the next visit brings a new
   * `since` and therefore a new panel.
   */
  since?: string
  photos?: number
  comments?: number
  albums?: WhatsNewAlbum[]
  album_count?: number
  people?: WhatsNewPerson[]
  person_count?: number
}

/**
 * Fetches the caller's digest from `GET /api/v1/whats-new`. The endpoint is
 * behind auth, so the session cookie is sent with the request.
 *
 * Note that the request is a GET that writes: reading the digest is what stamps
 * the reader's visit server-side, so it must be issued once per library-home
 * load and not polled.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws Error if the response status is not 2xx.
 */
export async function fetchWhatsNew(signal?: AbortSignal): Promise<WhatsNew> {
  const res = await fetch(`${API_BASE}/whats-new`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new Error(`whats-new request failed: ${res.status}`)
  }
  return (await res.json()) as WhatsNew
}
