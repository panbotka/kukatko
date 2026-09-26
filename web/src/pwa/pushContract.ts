/**
 * The contract between the server, the service worker and the app for one Web
 * Push notification.
 *
 * The flow, end to end:
 *
 *  1. The browser subscribes (`push.ts`) with the instance's VAPID public key and
 *     hands the subscription to `POST /api/v1/push/subscriptions`.
 *  2. The server encrypts a small JSON object — `push.Notification` in
 *     `internal/push` — to that subscription: a title, a body, a deeplink path,
 *     a kind and a collapse tag, under the field names below.
 *  3. `build/service-worker.js` receives it in its `push` handler, turns it into
 *     what {@link parsePushPayload} describes and shows it. A payload that is
 *     empty or unreadable still shows a generic „Kukátko" notification: a push
 *     that shows nothing is a silently swallowed event, and Chrome penalises a
 *     `userVisibleOnly` subscription whose pushes show nothing.
 *  4. A click on it lands in the worker's `notificationclick` handler, which
 *     focuses an open Kukátko window and navigates it to the deeplink, or opens
 *     one when none is open.
 *
 * This module holds what the halves have to agree on. It is deliberately free of
 * DOM types, exactly like `shareContract.ts`: the worker is plain, un-bundled
 * JavaScript and cannot import it, so `build/pwa.test.ts` — typechecked without
 * the DOM lib — imports it and drives the *real* worker against it. That test is
 * what keeps the worker's copy of these rules from drifting.
 */

/** The payload's headline (`push.Notification.Title`); required by the server. */
export const PUSH_TITLE_FIELD = 'title'

/** The text under the headline (`push.Notification.Body`); may be empty. */
export const PUSH_BODY_FIELD = 'body'

/** The deeplink path opened on a click (`push.Notification.URL`). */
export const PUSH_URL_FIELD = 'url'

/** What the notification is about (`push.Notification.Kind`), e.g. `tagged`. */
export const PUSH_KIND_FIELD = 'kind'

/** The collapse key (`push.Notification.Tag`); empty never collapses. */
export const PUSH_TAG_FIELD = 'tag'

/**
 * The large icon of every notification: the app icon, as the manifest already
 * lists it. Served from `web/public`, so it is fetched on demand, not precached.
 */
export const PUSH_ICON = '/icons/kukatko-192.png'

/**
 * The small status-bar glyph Android draws from the notification's `badge`: a
 * 96×96 white-on-transparent silhouette of the peephole, rendered from
 * `icons/kukatko-badge.svg` by `scripts/icons.sh`. Android uses only its alpha
 * channel; without one Chrome shows a generic dot.
 */
export const PUSH_BADGE = '/icons/kukatko-badge-96.png'

/** The title shown when a payload carries none (or cannot be read at all). */
export const PUSH_FALLBACK_TITLE = 'Kukátko'

/**
 * Every tag the worker sets starts with this, so a Kukátko notification can
 * never collide with (and silently replace) one some other code on the origin
 * shows under a bare tag of its own.
 */
export const PUSH_TAG_PREFIX = 'kukatko-'

/**
 * The tag of the generic notification an unreadable payload turns into. A run
 * of them collapses into one entry instead of stacking identical „Kukátko"
 * lines in the shade.
 */
export const PUSH_FALLBACK_TAG = `${PUSH_TAG_PREFIX}fallback`

/** Where a notification without a usable deeplink leads: the home page. */
export const PUSH_HOME_PATH = '/'

/** What the worker shows for one push, and what it keeps in `notification.data`. */
export interface PushDisplay {
  /** The notification's title. Never empty. */
  title: string
  /** The notification's body; empty when the payload had none. */
  body: string
  /** The large icon, always {@link PUSH_ICON}. */
  icon: string
  /** The status-bar glyph, always {@link PUSH_BADGE}. */
  badge: string
  /**
   * The notification tag ({@link notificationTag}), or null for a payload that
   * asked not to collapse. A tagged notification replaces an earlier one with
   * the same tag — and re-alerts (`renotify`), so the replacement is not silent.
   */
  tag: string | null
  /** The deeplink, always a same-origin path ({@link deeplinkPath}). */
  url: string
  /** The payload's kind; empty when it had none. */
  kind: string
}

/**
 * The notification tag for a payload's collapse key: the key under
 * {@link PUSH_TAG_PREFIX}, or null when there is no key (or it is not a
 * string) — the server's "empty never collapses".
 */
export function notificationTag(tag: unknown): string | null {
  if (typeof tag !== 'string' || tag === '') {
    return null
  }
  return `${PUSH_TAG_PREFIX}${tag}`
}

/**
 * The deeplink a click opens, as a path on this instance. Anything a browser
 * would resolve to another origin — an absolute URL, a scheme-relative `//host`
 * or its backslash variant `/\host` — or anything that is not a non-empty
 * string becomes {@link PUSH_HOME_PATH}. The server refuses such a URL already
 * (`push.Notification.Encode`); this is the worker not trusting a payload to
 * send it off the instance.
 */
export function deeplinkPath(url: unknown): string {
  if (typeof url !== 'string' || !url.startsWith('/')) {
    return PUSH_HOME_PATH
  }
  if (url.startsWith('//') || url.startsWith('/\\')) {
    return PUSH_HOME_PATH
  }
  return url
}

/** A field of the payload if it is a string, otherwise the empty string. */
function stringField(payload: Record<string, unknown>, field: string): string {
  const value = payload[field]
  return typeof value === 'string' ? value : ''
}

/** The generic notification an unreadable payload turns into. */
function fallbackDisplay(): PushDisplay {
  return {
    title: PUSH_FALLBACK_TITLE,
    body: '',
    icon: PUSH_ICON,
    badge: PUSH_BADGE,
    tag: PUSH_FALLBACK_TAG,
    url: PUSH_HOME_PATH,
    kind: '',
  }
}

/**
 * What the worker shows for a push whose payload text is `text` (null for a
 * push without a payload).
 *
 * A payload that is missing, not JSON or not a JSON object becomes the generic
 * notification (fallback title, home page, {@link PUSH_FALLBACK_TAG}). An object
 * is read field by field, each falling back on its own: a missing or blank title
 * becomes {@link PUSH_FALLBACK_TITLE} and the body, deeplink and tag it did
 * carry are still used. Never throws.
 */
export function parsePushPayload(text: string | null): PushDisplay {
  if (text === null || text === '') {
    return fallbackDisplay()
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return fallbackDisplay()
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return fallbackDisplay()
  }
  const payload = parsed as Record<string, unknown>
  const title = stringField(payload, PUSH_TITLE_FIELD).trim()
  return {
    title: title === '' ? PUSH_FALLBACK_TITLE : title,
    body: stringField(payload, PUSH_BODY_FIELD),
    icon: PUSH_ICON,
    badge: PUSH_BADGE,
    tag: notificationTag(payload[PUSH_TAG_FIELD]),
    url: deeplinkPath(payload[PUSH_URL_FIELD]),
    kind: stringField(payload, PUSH_KIND_FIELD),
  }
}
