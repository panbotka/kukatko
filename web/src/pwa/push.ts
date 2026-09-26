/**
 * Browser half of Web Push: whether this browser can receive a push at all,
 * the notification permission, and the subscription this browser holds with
 * its push service and with Kukátko's server.
 *
 * The worker half — what a push shows and where a click leads — lives in
 * `build/service-worker.js`, held to `pushContract.ts`. The server half is
 * `internal/notificationapi`: `GET /push/config` (is push on, and the VAPID
 * public key), `POST /push/subscriptions` (register this browser) and
 * `DELETE /push/subscriptions?endpoint=` (forget it).
 *
 * # Never throws
 *
 * Every export resolves to a {@link PushState} (or a permission) instead of
 * rejecting: an unsupported browser, a denied permission, a missing worker
 * registration and a failing request are all ordinary states a settings page
 * explains, not errors it has to catch. The one {@link PushState} that means
 * "something went wrong" is `failed`, and it is still a value.
 *
 * # The permission prompt belongs to a click
 *
 * Browsers grant `Notification.requestPermission()` only from a user gesture,
 * and a prompt nobody asked for is the fastest way to a permanent "block". So
 * this module never asks on its own: {@link requestPushPermission} is exported
 * for a click handler, and {@link subscribeToPush} refuses to run (reporting
 * `prompt`) until the permission is already granted — `pushManager.subscribe`
 * would otherwise raise the prompt itself.
 * Firefox (72+, Android 79+) additionally accepts `pushManager.subscribe` only
 * inside a user gesture, so call {@link subscribeToPush} from that same click
 * too, never from an effect.
 *
 * # Platforms
 *
 * Android needs nothing beyond VAPID: Chrome (50+) and Firefox (48+) receive
 * pushes in a plain browser tab. iOS delivers pushes only to a PWA added to the
 * home screen (Safari 16.4+); mobile Safari without the install exposes no
 * `PushManager`, and a subscribe it refuses with `NotSupportedError` is
 * reported as `unsupported` too — so the UI can say "install the app first"
 * instead of showing a broken button.
 */

const API_BASE = '/api/v1'

/** The notification permission, or `unsupported` where the browser cannot push. */
export type PushPermission = 'default' | 'granted' | 'denied' | 'unsupported'

/**
 * Where this browser stands with push:
 *
 *  - `unsupported` — no Push API here (an old browser, an insecure origin, iOS
 *    outside an installed PWA);
 *  - `no-worker` — the API exists but no service worker is registered (the dev
 *    server, or a registration that failed), and a push needs one to land in;
 *  - `disabled` — the instance has push switched off (or has no VAPID key);
 *  - `denied` — the person blocked notifications for this site;
 *  - `prompt` — nobody has been asked yet; a click must call
 *    {@link requestPushPermission} first;
 *  - `unsubscribed` — permission granted, no subscription held;
 *  - `subscribed` — this browser holds a subscription (`endpoint` names it);
 *  - `failed` — a request or the push service failed; nothing was left
 *    half-done (see {@link subscribeToPush}).
 */
export type PushStatus =
  | 'unsupported'
  | 'no-worker'
  | 'disabled'
  | 'denied'
  | 'prompt'
  | 'unsubscribed'
  | 'subscribed'
  | 'failed'

/** A reported push state. */
export interface PushState {
  /** Where this browser stands; see {@link PushStatus}. */
  status: PushStatus
  /** The subscription's push-service endpoint when `subscribed`, otherwise null. */
  endpoint: string | null
}

/** Response body of `GET /api/v1/push/config`. */
interface PushConfig {
  enabled: boolean
  public_key: string
}

/** A state that carries no subscription. */
function state(status: Exclude<PushStatus, 'subscribed'>): PushState {
  return { status, endpoint: null }
}

/** The state of a browser holding `subscription`. */
function subscribed(subscription: PushSubscription): PushState {
  return { status: 'subscribed', endpoint: subscription.endpoint }
}

/**
 * Reports whether this browser can receive a push at all: a service worker
 * container, the Push API and the Notification API must all be present.
 */
export function isPushSupported(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    'serviceWorker' in navigator &&
    typeof window !== 'undefined' &&
    'PushManager' in window &&
    'Notification' in window
  )
}

/** The current notification permission, without asking for it. */
export function getPushPermission(): PushPermission {
  if (!isPushSupported()) {
    return 'unsupported'
  }
  return Notification.permission
}

/**
 * Asks the person for the notification permission and resolves with the
 * answer. **Call it only from a click handler**: outside a user gesture a
 * browser refuses (or quietly auto-denies) the prompt. A browser that cannot
 * push resolves `unsupported`, and a prompt that fails reports the permission as
 * it stands.
 */
export async function requestPushPermission(): Promise<PushPermission> {
  if (!isPushSupported()) {
    return 'unsupported'
  }
  try {
    return await Notification.requestPermission()
  } catch {
    return getPushPermission()
  }
}

/** The state implied by the permission alone, for a browser without a subscription. */
function unsubscribedState(): PushState {
  switch (getPushPermission()) {
    case 'unsupported':
      return state('unsupported')
    case 'denied':
      return state('denied')
    case 'default':
      return state('prompt')
    case 'granted':
      return state('unsubscribed')
  }
}

/**
 * The service worker registration a push would land in, or null when there is
 * none. `getRegistration` rather than `ready`: `ready` never settles on a page
 * no worker controls (the dev server), and this module must not hang.
 */
async function workerRegistration(): Promise<ServiceWorkerRegistration | null> {
  try {
    const registration = await navigator.serviceWorker.getRegistration('/')
    return registration ?? null
  } catch {
    return null
  }
}

/**
 * Where this browser stands with push right now: support, permission and the
 * subscription its worker holds. Asks nothing and changes nothing; the server
 * is not consulted (whether it still has the subscription is its own business).
 */
export async function getPushState(): Promise<PushState> {
  if (!isPushSupported()) {
    return state('unsupported')
  }
  if (getPushPermission() === 'denied') {
    return state('denied')
  }
  const registration = await workerRegistration()
  if (!registration) {
    return state('no-worker')
  }
  try {
    const subscription = await registration.pushManager.getSubscription()
    return subscription ? subscribed(subscription) : unsubscribedState()
  } catch {
    return state('failed')
  }
}

/** Reads the instance's push configuration, or null when the request fails. */
async function fetchPushConfig(): Promise<PushConfig | null> {
  try {
    const res = await fetch(`${API_BASE}/push/config`, { credentials: 'same-origin' })
    if (!res.ok) {
      return null
    }
    return (await res.json()) as PushConfig
  } catch {
    return null
  }
}

/**
 * Reports whether the instance has push switched on and a VAPID key to push
 * with. A configuration that cannot be read (a 5xx, the network) reads as
 * off: nobody should be offered something the server may not deliver.
 */
export async function isPushEnabled(): Promise<boolean> {
  const config = await fetchPushConfig()
  return config !== null && config.enabled && config.public_key !== ''
}

/**
 * Reports whether this is iOS (or iPadOS) outside an installed home-screen
 * app — the one place where a push can never arrive however the permission is
 * answered, because Safari delivers pushes only to a PWA added to the home
 * screen (16.4+). Recent Safari hides `PushManager` in a tab, so
 * {@link isPushSupported} usually says so already; this is the explicit check
 * for the UI that has to be honest about it rather than rely on an omission.
 * iPadOS reports itself as a Mac, so a "Mac" with a touch screen counts as one.
 */
export function isIosOutsideInstalledApp(): boolean {
  if (typeof navigator === 'undefined' || typeof window === 'undefined') {
    return false
  }
  const ios =
    /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.userAgent.includes('Macintosh') && navigator.maxTouchPoints > 1)
  if (!ios) {
    return false
  }
  const installed =
    (navigator as Navigator & { standalone?: boolean }).standalone === true ||
    (typeof window.matchMedia === 'function' &&
      window.matchMedia('(display-mode: standalone)').matches)
  return !installed
}

/**
 * Decodes a base64url string (the VAPID public key's encoding, padding
 * optional) into the bytes `applicationServerKey` expects.
 */
export function decodeBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes
}

/** Reports whether a subscription was made with exactly `key` as its server key. */
function madeWithKey(subscription: PushSubscription, key: Uint8Array): boolean {
  const own = subscription.options.applicationServerKey
  if (!own) {
    return false
  }
  const bytes = new Uint8Array(own)
  return bytes.length === key.length && bytes.every((byte, i) => byte === key[i])
}

/** Maps a rejected `pushManager.subscribe` to a state. */
function subscribeFailure(error: unknown): PushState {
  // iOS outside an installed PWA (and any browser without a push service)
  // refuses here; that is a platform limit, not a failure.
  if (error instanceof DOMException && error.name === 'NotSupportedError') {
    return state('unsupported')
  }
  if (getPushPermission() === 'denied') {
    return state('denied')
  }
  return state('failed')
}

/** Drops a browser subscription, swallowing a failure (it is best effort). */
async function dropBrowserSubscription(subscription: PushSubscription): Promise<void> {
  try {
    await subscription.unsubscribe()
  } catch {
    // Left for the push service to expire; the server prunes it once gone.
  }
}

/**
 * The browser's subscription for `key`: the one already held when it was made
 * with the same key, otherwise a fresh one. A subscription made with another
 * key — the instance rotated its VAPID pair — is dropped first, since a browser
 * refuses to subscribe again under a different key while it holds one.
 */
async function subscriptionFor(
  registration: ServiceWorkerRegistration,
  key: Uint8Array<ArrayBuffer>,
): Promise<PushSubscription> {
  const existing = await registration.pushManager.getSubscription()
  if (existing && madeWithKey(existing, key)) {
    return existing
  }
  if (existing) {
    await existing.unsubscribe()
  }
  return registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
}

/** Registers `subscription` with the server; resolves with the response status (0 on a network error). */
async function registerWithServer(subscription: PushSubscription): Promise<number> {
  try {
    const res = await fetch(`${API_BASE}/push/subscriptions`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...subscription.toJSON(), user_agent: navigator.userAgent }),
    })
    return res.status
  } catch {
    return 0
  }
}

/**
 * Subscribes this browser to push and registers the subscription with the
 * server. **Does not ask for the permission**: with none granted yet it
 * resolves `prompt` (or `denied`) and does nothing — call
 * {@link requestPushPermission} from the click first.
 *
 * Resolves `subscribed` only when both halves hold the subscription. If the
 * server refuses it — `503` means push is off instance-wide (`disabled`),
 * anything else is `failed` — the browser subscription is dropped again, so the
 * browser never believes in a subscription nothing will deliver to.
 */
export async function subscribeToPush(): Promise<PushState> {
  if (!isPushSupported()) {
    return state('unsupported')
  }
  if (getPushPermission() !== 'granted') {
    return unsubscribedState()
  }
  const registration = await workerRegistration()
  if (!registration) {
    return state('no-worker')
  }
  const config = await fetchPushConfig()
  if (!config) {
    return state('failed')
  }
  if (!config.enabled || config.public_key === '') {
    return state('disabled')
  }

  let subscription: PushSubscription
  try {
    subscription = await subscriptionFor(registration, decodeBase64Url(config.public_key))
  } catch (error) {
    return subscribeFailure(error)
  }

  const status = await registerWithServer(subscription)
  if (status >= 200 && status < 300) {
    return subscribed(subscription)
  }
  await dropBrowserSubscription(subscription)
  return state(status === 503 ? 'disabled' : 'failed')
}

/**
 * Unsubscribes this browser in both places: the server forgets the endpoint
 * (a `404` — already forgotten — is fine), then the browser drops the
 * subscription. A server that cannot be reached does not keep the browser
 * subscribed: once the browser has dropped it the push service answers the next
 * delivery with "gone", and the server prunes the row then. Resolves with the
 * state afterwards: still `subscribed` when the browser declined to drop the
 * subscription, `failed` when dropping it threw.
 */
export async function unsubscribeFromPush(): Promise<PushState> {
  if (!isPushSupported()) {
    return state('unsupported')
  }
  const registration = await workerRegistration()
  if (!registration) {
    return state('no-worker')
  }
  let subscription: PushSubscription | null
  try {
    subscription = await registration.pushManager.getSubscription()
  } catch {
    return state('failed')
  }
  if (!subscription) {
    return unsubscribedState()
  }

  try {
    await fetch(
      `${API_BASE}/push/subscriptions?endpoint=${encodeURIComponent(subscription.endpoint)}`,
      { method: 'DELETE', credentials: 'same-origin' },
    )
  } catch {
    // Pruned server-side once the push service reports the endpoint gone.
  }

  try {
    if (!(await subscription.unsubscribe())) {
      return subscribed(subscription)
    }
  } catch {
    return state('failed')
  }
  return unsubscribedState()
}
