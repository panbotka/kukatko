import { ApiError } from './auth'
import { type Photo } from './photos'
import { apiPath, instancePath } from './api'

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

/**
 * How a notification reaches the account (`notification.Channel`): `in_app` (the
 * inbox behind the bell), `push` to its devices or `email`. A string rather than a union because the server may grow
 * a channel this build does not know; the card simply ignores it.
 */
export type NotificationChannel = string

/**
 * The in-app channel: a notification listed in the inbox behind the navbar's
 * bell. It needs no device and no instance switch.
 */
export const IN_APP_CHANNEL: NotificationChannel = 'in_app'

/** The push channel: a notification delivered to the account's registered browsers. */
export const PUSH_CHANNEL: NotificationChannel = 'push'

/** The e-mail channel: a notification mailed to the account's address. */
export const EMAIL_CHANNEL: NotificationChannel = 'email'

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
  return apiPath(`/notifications/${encodeURIComponent(uid)}`)
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

/**
 * Where a notification opens: its own link — the notification page for a photo
 * set, the approval screen for a registration, … — or, for one that carries
 * none, its own page, which shows its text.
 */
export function notificationTarget(n: Pick<NotificationRecord, 'uid' | 'link'>): string {
  return n.link !== '' ? n.link : notificationPath(n.uid)
}

/** One page of the inbox, as returned by `GET /notifications` (`listEnvelope`). */
export interface NotificationPage {
  /** The page, newest first. */
  notifications: NotificationRecord[]
  /** How many notifications the selection holds across all pages. */
  total: number
  /** The page size the server actually used (clamped to its maximum). */
  limit: number
  /** How many notifications were skipped before this page. */
  offset: number
}

/** What one inbox read asks for. Every field is optional. */
export interface NotificationListQuery {
  /** Only notifications not read yet. */
  unread?: boolean
  /** Page size; the server defaults to 30 and caps it at 100. */
  limit?: number
  /** How many to skip. */
  offset?: number
}

/**
 * Reads one page of the caller's inbox — the notifications delivered in-app,
 * newest first. A record that only backs a push or an e-mail is never listed.
 */
export async function fetchNotifications(
  query: NotificationListQuery = {},
  signal?: AbortSignal,
): Promise<NotificationPage> {
  const params = new URLSearchParams()
  if (query.unread === true) {
    params.set('unread', 'true')
  }
  if (query.limit !== undefined) {
    params.set('limit', String(query.limit))
  }
  if (query.offset !== undefined && query.offset > 0) {
    params.set('offset', String(query.offset))
  }
  const search = params.toString()
  const res = await fetch(apiPath(`/notifications${search === '' ? '' : `?${search}`}`), {
    credentials: 'same-origin',
    signal,
  })
  return readJson<NotificationPage>(res)
}

/** Reads how many notifications in the caller's inbox are unread — the bell's number. */
export async function fetchUnreadNotificationCount(signal?: AbortSignal): Promise<number> {
  const res = await fetch(apiPath('/notifications/unread-count'), {
    credentials: 'same-origin',
    signal,
  })
  const body = await readJson<{ unread: number }>(res)
  return body.unread
}

/** Marks every unread notification in the caller's inbox read; resolves to how many it marked. */
export async function markAllNotificationsRead(): Promise<number> {
  const res = await fetch(apiPath('/notifications/read-all'), {
    method: 'POST',
    credentials: 'same-origin',
  })
  const body = await readJson<{ marked: number }>(res)
  return body.marked
}

/**
 * One browser registered to receive this account's pushes, as listed by
 * `GET /push/subscriptions`. The client keys are never returned — they are
 * encryption secrets, not something a settings page shows.
 */
export interface PushSubscriptionRecord {
  id: string
  /** The push-service endpoint; equal to the browser's own when it is this one. */
  endpoint: string
  /** The raw user-agent the browser registered with; summarise it before showing it. */
  user_agent: string
  /** When the browser first registered (RFC 3339). */
  created_at: string
  /** When a push was last delivered to it (RFC 3339), or null before the first. */
  last_used_at: string | null
}

/**
 * Lists the browsers registered for the caller's pushes, oldest first.
 *
 * @throws ApiError when the request fails.
 */
export async function fetchPushSubscriptions(
  signal?: AbortSignal,
): Promise<PushSubscriptionRecord[]> {
  const res = await fetch(instancePath('/push/subscriptions'), {
    credentials: 'same-origin',
    signal,
  })
  const body = await readJson<{ subscriptions: PushSubscriptionRecord[] | null }>(res)
  return body.subscriptions ?? []
}

/**
 * Forgets one of the caller's registered browsers by its endpoint. Only the
 * server's half: the browser itself keeps its subscription, and its pushes
 * simply stop arriving. A `404` — somebody (that device, another tab) already
 * removed it — counts as done.
 *
 * @throws ApiError on any other failure.
 */
export async function deletePushSubscription(endpoint: string): Promise<void> {
  const res = await fetch(
    instancePath(`/push/subscriptions?endpoint=${encodeURIComponent(endpoint)}`),
    { method: 'DELETE', credentials: 'same-origin' },
  )
  if (res.ok || res.status === 404) {
    return
  }
  await readJson<unknown>(res)
}

/**
 * One kind's effective setting on one channel (`notification.Preference`): the
 * stored choice, or the channel's default for the kind when the account never
 * chose (`is_default`).
 */
export interface NotificationPreference {
  kind: NotificationKind
  channel: NotificationChannel
  enabled: boolean
  is_default: boolean
}

/** A choice to store, as `PUT /notifications/preferences` takes it. */
export interface NotificationPreferenceInput {
  kind: NotificationKind
  channel: NotificationChannel
  enabled: boolean
}

/** The response body of both preference routes. */
interface PreferencesBody {
  preferences: NotificationPreference[] | null
}

/**
 * Reads the caller's effective notification preferences — every known kind and
 * channel it is delivered through, in display order, defaults merged in. They belong to the **account**: one set
 * for every device it is signed in on.
 *
 * @throws ApiError when the request fails.
 */
export async function fetchNotificationPreferences(
  signal?: AbortSignal,
): Promise<NotificationPreference[]> {
  const res = await fetch(apiPath('/notifications/preferences'), {
    credentials: 'same-origin',
    signal,
  })
  return (await readJson<PreferencesBody>(res)).preferences ?? []
}

/**
 * Replaces the caller's stored choices and resolves with the new effective set.
 * **A (kind, channel) left out returns to its default**, so a caller changing
 * one sends the other stored choices along — see {@link withPreference}.
 *
 * @throws ApiError when the server refuses the set or the request fails.
 */
export async function updateNotificationPreferences(
  preferences: NotificationPreferenceInput[],
): Promise<NotificationPreference[]> {
  const res = await fetch(apiPath('/notifications/preferences'), {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ preferences }),
  })
  return (await readJson<PreferencesBody>(res)).preferences ?? []
}

/**
 * The body that changes one kind on one channel and keeps everything else as it
 * is: every choice the account already stored — on any channel, including ones
 * this screen does not show — plus `kind` on `channel` set to `enabled`. Pairs
 * still on their default are left out on purpose — posting them would freeze
 * today's default into a stored choice the account never made.
 */
export function withPreference(
  current: NotificationPreference[],
  kind: NotificationKind,
  channel: NotificationChannel,
  enabled: boolean,
): NotificationPreferenceInput[] {
  const stored = current
    .filter((pref) => !pref.is_default && !(pref.kind === kind && pref.channel === channel))
    .map((pref) => ({ kind: pref.kind, channel: pref.channel, enabled: pref.enabled }))
  return [...stored, { kind, channel, enabled }]
}
