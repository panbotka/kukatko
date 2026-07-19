import { ApiError } from './auth'

/**
 * Announcement client, mirroring the backend JSON shapes from
 * `internal/announcement` and `internal/announcementapi`. There is a single
 * instance-wide announcement: any signed-in user can read it, and a maintainer
 * publishes or clears it. The session cookie is sent automatically, so this
 * client never sends a user. Each call throws {@link ApiError} on a non-OK
 * response so callers can branch on `status`.
 */

const API_BASE = '/api/v1'

/** The banner variant an announcement is rendered with. */
export type AnnouncementLevel = 'info' | 'warning'

/**
 * The current announcement (`announcement.Announcement`). `message` is always
 * present — an empty string means nothing is published. `level` and `updated_at`
 * are present only when a message is set; `updated_at` keys the per-user dismissal.
 */
export interface Announcement {
  message: string
  level?: AnnouncementLevel
  author_uid?: string
  updated_at?: string
}

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
 * Issues a body-carrying request (PUT/DELETE) and parses the JSON body, throwing
 * ApiError on a non-OK status. A 204 (or otherwise empty) response resolves to
 * `undefined`, so callers expecting no content can ignore the result.
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

/** Reads the current announcement; an empty `message` means nothing is published. */
export async function fetchAnnouncement(signal?: AbortSignal): Promise<Announcement> {
  return getJSON<Announcement>('/announcement', signal)
}

/** Publishes (or replaces) the announcement; maintainer-only server-side. */
export async function setAnnouncement(
  message: string,
  level: AnnouncementLevel,
  signal?: AbortSignal,
): Promise<Announcement> {
  return sendJSON<Announcement>('PUT', '/announcement', { message, level }, signal)
}

/** Clears the announcement for all users; maintainer-only server-side. */
export async function clearAnnouncement(signal?: AbortSignal): Promise<void> {
  await sendJSON<undefined>('DELETE', '/announcement', undefined, signal)
}
