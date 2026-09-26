import { ApiError } from './auth'
import { type Photo } from './photos'

/**
 * Notifications client, mirroring `internal/notification` and
 * `internal/notificationapi`.
 *
 * A notification is the record behind one push: what it said, when it was sent,
 * and — for the kinds that are about photographs — a **frozen, ordered set** of
 * them. The set is what makes "you were tagged in 12 photos" land on exactly
 * those twelve rather than on a live search a thirteenth tag would change.
 *
 * Every call is scoped to the caller: an unknown or somebody else's uid is a
 * `404`, never a `403`, so both throw {@link ApiError} with that status and a
 * page tells "gone" apart from "could not be loaded" through `isNotFound`.
 */

const API_BASE = '/api/v1'

/**
 * The in-app route of one notification's own page — the deeplink its push
 * carries. Kept to two characters because it travels inside a push payload with
 * a hard size limit; it must match `notification.PathPrefix` on the server,
 * which is what writes the link.
 */
export const NOTIFICATION_PATH_PREFIX = '/n/'

/** The deeplink of one notification's page. */
export function notificationPath(uid: string): string {
  return `${NOTIFICATION_PATH_PREFIX}${encodeURIComponent(uid)}`
}

/**
 * What a notification is about (`notification.Kind`): `tagged`,
 * `registration_pending`, … Kept an open string — the server adds kinds without
 * a client release, and the page never branches on it.
 */
export type NotificationKind = string

/** One notification as its owner reads it (`notificationView`). */
export interface NotificationRecord {
  uid: string
  kind: NotificationKind
  /** The headline the push was shown with. */
  title: string
  /** The line under it; may be empty. */
  body: string
  /** The deeplink the push opened — this page's own path for a photo set. */
  link: string
  /** When it was made (RFC 3339). */
  created_at: string
  /** When it was first read (RFC 3339), or null while unread. */
  read_at: string | null
}

/**
 * One notification with the part of its frozen photo set the caller may see
 * **now**, as returned by `GET /notifications/{uid}` (`detailView`).
 */
export interface NotificationDetail extends NotificationRecord {
  /** The visible photographs, in the frozen order, shaped like every photo payload. */
  photos: Photo[]
  /** How many photographs of the set still exist, visible or not. */
  total_count: number
  /**
   * How many of those the caller may no longer see — archived, hidden or made
   * private since the notification was sent. What lets a page say "10 of 12"
   * honestly instead of silently showing fewer than the push promised.
   */
  dropped_count: number
}

/** Builds the endpoint of one notification, escaping the uid. */
function endpoint(uid: string): string {
  return `${API_BASE}/notifications/${encodeURIComponent(uid)}`
}

/** Reads the JSON body of a response, or throws {@link ApiError} with its status. */
async function readJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = res.statusText || `request failed: ${res.status}`
    try {
      const body = (await res.json()) as { error?: unknown }
      if (typeof body.error === 'string' && body.error !== '') {
        message = body.error
      }
    } catch {
      // Empty or non-JSON body: keep the status text.
    }
    throw new ApiError(res.status, message)
  }
  return (await res.json()) as T
}

/**
 * Reads one of the caller's notifications with its visible photographs. Reading
 * does **not** mark it read — see {@link markNotificationRead}.
 *
 * @throws ApiError 404 when the uid is unknown, somebody else's, or purged by retention.
 */
export async function fetchNotification(
  uid: string,
  signal?: AbortSignal,
): Promise<NotificationDetail> {
  const res = await fetch(endpoint(uid), { credentials: 'same-origin', signal })
  return readJson<NotificationDetail>(res)
}

/**
 * Marks one of the caller's notifications read. Idempotent server-side: marking
 * again keeps the moment it was first read.
 *
 * @throws ApiError 404 when the uid is unknown or somebody else's.
 */
export async function markNotificationRead(uid: string): Promise<NotificationRecord> {
  const res = await fetch(`${endpoint(uid)}/read`, {
    method: 'POST',
    credentials: 'same-origin',
  })
  return readJson<NotificationRecord>(res)
}
