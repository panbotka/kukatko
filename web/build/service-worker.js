/*
 * Kukátko's service worker — the source template.
 *
 * This file is NOT bundled by Vite. The `pwaPlugin` in build/pwa.ts reads it,
 * substitutes the two build-time placeholders below, and emits the result as
 * `/sw.js` at the root of the build output (root scope, unhashed name, served
 * with `Cache-Control: no-cache` by internal/web).
 *
 * # What it does, and just as importantly what it does not
 *
 * It precaches the built app shell — index.html plus Vite's content-hashed
 * assets — so a return visit paints from disk instead of the network, and a
 * navigation still resolves when the device is offline.
 *
 * Everything else is left strictly alone. The worker never touches:
 *   - anything under /api/ — the whole backend, including photo thumbnails,
 *     original downloads and video streaming, all of which live under
 *     /api/v1/photos/… (see internal/mediaurl). Caching those would serve stale
 *     or cross-user bytes, break Range requests mid-scrub, and hold responses
 *     that an auth cookie gated;
 *   - cross-origin requests — with object storage in front of the library, the
 *     media itself is fetched from signed R2 URLs on another origin;
 *   - any request carrying a Range header, or any method other than GET;
 *   - any path that is not in the precache manifest.
 *
 * The rule is a whitelist, not a blacklist: a request the worker does not
 * recognise falls through untouched (no respondWith call), so the browser does
 * exactly what it would do with no service worker installed at all.
 *
 * # The one exception: the share target
 *
 * A POST to /share-target is the phone's share sheet handing photos over (the
 * manifest's `share_target`). It is the only non-GET the worker answers, and it
 * is not cached: the body is consumed once, each file is stashed in a cache of
 * its own for the app to collect, and the browser is redirected to a GET the
 * page can render. The whole contract — paths, field and header names, the id
 * format — is documented in src/pwa/shareContract.ts, which build/pwa.test.ts
 * imports to hold this file to it.
 *
 * # Push notifications
 *
 * Two more events have nothing to do with fetching and leave the whitelist
 * above untouched. `push` reads the server's JSON payload (a title, a body, a
 * deeplink path, a kind and a collapse tag) and shows it with the app icon, the
 * monochrome Android badge and the tag, so a newer notification of the same
 * collapse key replaces the older one instead of stacking; an empty or
 * unreadable payload still shows a generic „Kukátko" notification rather than
 * throwing. `notificationclick` closes the notification and opens its
 * deeplink — in the Kukátko window that is already open when there is one
 * (focused and navigated: an installed app has no tabs, and a second window
 * leaves the person no way back), in a new window otherwise. The contract —
 * field names, icon paths, the tag shape, the deeplink rule — is documented in
 * src/pwa/pushContract.ts, which build/pwa.test.ts imports to hold this file
 * to it.
 *
 * # Updates
 *
 * Install does NOT call skipWaiting: a freshly deployed worker parks in
 * "waiting" so the running page keeps its matching shell and assets. The page
 * (src/pwa/register.ts) notices the waiting worker, offers a refresh, and posts
 * SKIP_WAITING when the reader accepts — the handler below then activates,
 * drops older caches and claims the open clients, and the page reloads onto the
 * new shell.
 */

// Both literals are replaced verbatim by build/pwa.ts. The placeholder values
// keep this file valid, formattable, lintable JavaScript on its own.
const CACHE_NAME = '__KUKATKO_CACHE_NAME__'
const PRECACHE = ['__KUKATKO_PRECACHE__']

/** Every cache this app has ever owned starts with this, so activate can prune. */
const CACHE_PREFIX = 'kukatko-shell-'

/** The precached document every in-app navigation resolves to. */
const SHELL_URL = '/index.html'

/**
 * Path prefixes the worker must never intercept. They are the server's own
 * routes: the API (media and downloads included) plus the health and metrics
 * endpoints. See the header comment for why caching them would be wrong.
 */
const BYPASS_PREFIXES = ['/api/', '/healthz', '/metrics']

/** Membership test for the precache manifest, built once per worker start. */
const PRECACHE_SET = new Set(PRECACHE)

/**
 * The share target's action — where the share sheet POSTs, and where the worker
 * redirects afterwards for the app to pick the files up. Mirrors
 * SHARE_TARGET_PATH in src/pwa/shareContract.ts.
 */
const SHARE_TARGET_PATH = '/share-target'

/** The manifest's file field; mirrors SHARE_FILES_FIELD. */
const SHARE_FILES_FIELD = 'files'

/** Query parameter naming the staged share; mirrors SHARE_PARAM. */
const SHARE_PARAM = 'share'

/**
 * Where staged files live. Deliberately *not* prefixed with CACHE_PREFIX: the
 * activate handler prunes shell caches, and a deployment landing between a share
 * and its collection must not delete the user's photos. Mirrors SHARE_CACHE.
 */
const SHARE_CACHE = 'kukatko-share'

/** Key prefix of one staged file: `<prefix><id>/<index>`. Mirrors SHARE_ENTRY_PREFIX. */
const SHARE_ENTRY_PREFIX = '/__kukatko-share__/'

/** Headers carrying what a Response cannot: the file's name and mtime. */
const SHARE_NAME_HEADER = 'x-kukatko-share-name'
const SHARE_MODIFIED_HEADER = 'x-kukatko-share-modified'

/** The push payload's field names; mirror the PUSH_*_FIELD constants in src/pwa/pushContract.ts. */
const PUSH_TITLE_FIELD = 'title'
const PUSH_BODY_FIELD = 'body'
const PUSH_URL_FIELD = 'url'
const PUSH_KIND_FIELD = 'kind'
const PUSH_TAG_FIELD = 'tag'

/** The notification's large icon and its Android status-bar glyph; mirror PUSH_ICON and PUSH_BADGE. */
const PUSH_ICON = '/icons/kukatko-192.png'
const PUSH_BADGE = '/icons/kukatko-badge-96.png'

/** Title of a notification whose payload carries none; mirrors PUSH_FALLBACK_TITLE. */
const PUSH_FALLBACK_TITLE = 'Kukátko'

/** Every tag this worker sets starts with this; mirrors PUSH_TAG_PREFIX. */
const PUSH_TAG_PREFIX = 'kukatko-'

/** Tag of the generic notification an unreadable payload becomes; mirrors PUSH_FALLBACK_TAG. */
const PUSH_FALLBACK_TAG = PUSH_TAG_PREFIX + 'fallback'

/** Where a notification without a usable deeplink leads; mirrors PUSH_HOME_PATH. */
const PUSH_HOME_PATH = '/'

/** Distinguishes shares minted in the same millisecond by one worker. */
let shareSequence = 0

/** Last-resort body when a navigation misses both the network and the cache. */
const OFFLINE_BODY = 'Kukátko je offline. / Kukátko is offline.'

/**
 * Reports whether pathname belongs to the server rather than to the app shell,
 * and must therefore reach the network untouched.
 */
function isBypassed(pathname) {
  return BYPASS_PREFIXES.some((prefix) => pathname.startsWith(prefix))
}

/**
 * Reports whether the worker is allowed to answer this request at all. A `false`
 * here means "behave as if no service worker existed": non-GET, cross-origin,
 * server-owned and ranged (video scrubbing) requests all land here.
 */
function isHandled(request, origin) {
  if (request.method !== 'GET') {
    return false
  }
  let url
  try {
    url = new URL(request.url)
  } catch {
    return false
  }
  if (url.origin !== origin) {
    return false
  }
  if (isBypassed(url.pathname)) {
    return false
  }
  // A ranged GET is a media scrub. Nothing ranged is ever part of the shell, so
  // this only ever fires as belt-and-braces against a future asset type.
  return !request.headers.has('range')
}

/**
 * Answers a precached asset from the cache, refetching and re-storing it if the
 * entry went missing (a cache the browser evicted under storage pressure).
 */
async function cacheFirst(request, pathname) {
  const cache = await caches.open(CACHE_NAME)
  const hit = await cache.match(pathname)
  if (hit) {
    return hit
  }
  const response = await fetch(request)
  if (response.ok) {
    await cache.put(pathname, response.clone())
  }
  return response
}

/**
 * Answers a navigation with the precached shell, which is what makes the app
 * open instantly and keeps it opening at all with no network. Falls back to the
 * network if the shell is somehow not cached, and to a plain offline notice if
 * that fails too — the in-app offline state (src/components/pwa) is what a
 * reader normally sees, and it needs the shell to render.
 */
async function shellResponse(request) {
  const cache = await caches.open(CACHE_NAME)
  const shell = await cache.match(SHELL_URL)
  if (shell) {
    return shell
  }
  try {
    return await fetch(request)
  } catch {
    return new Response(OFFLINE_BODY, {
      status: 503,
      headers: { 'Content-Type': 'text/plain; charset=utf-8' },
    })
  }
}

/**
 * Reports whether a request is the share sheet handing files over: a same-origin
 * POST to the manifest's share_target action.
 */
function isShareSubmission(request, origin) {
  if (request.method !== 'POST') {
    return false
  }
  let url
  try {
    url = new URL(request.url)
  } catch {
    return false
  }
  return url.origin === origin && url.pathname === SHARE_TARGET_PATH
}

/**
 * Mints an id for one share as `<epoch ms>-<sequence>`. The timestamp is not
 * decoration: the page reads it back out of the cache key to expire shares that
 * were never collected (see shareContract.ts).
 */
function nextShareId() {
  shareSequence += 1
  return Date.now() + '-' + shareSequence
}

/**
 * Writes every file of a share into the share cache under its own key and
 * returns the share's id. The file's name and modification time travel as
 * headers, since a Response body carries neither.
 *
 * Non-file parts (a shared title, text or URL) are ignored: Kukátko is being
 * asked to store photos, and there is nowhere to put a sentence.
 */
async function stageShare(request) {
  const form = await request.formData()
  const id = nextShareId()
  const cache = await caches.open(SHARE_CACHE)
  const parts = form.getAll(SHARE_FILES_FIELD)
  let index = 0
  for (const part of parts) {
    if (typeof part === 'string') {
      continue
    }
    const headers = {
      'Content-Type': part.type || 'application/octet-stream',
    }
    headers[SHARE_NAME_HEADER] = encodeURIComponent(part.name || 'shared-' + index)
    headers[SHARE_MODIFIED_HEADER] = String(part.lastModified || 0)
    await cache.put(SHARE_ENTRY_PREFIX + id + '/' + index, new Response(part, { headers }))
    index += 1
  }
  return id
}

/**
 * Answers the share POST. The response is always a 303 back to the share page,
 * so the browser ends up on a GET the app can render (a POST navigation would
 * re-submit on reload, and its body is single-use anyway).
 *
 * If staging fails — storage full, a payload the browser will not parse — the
 * redirect simply carries no share id, and the page then says the files did not
 * come through and offers the picker. Losing the photos silently, or answering
 * with an error document, would both be worse.
 */
async function receiveShare(request) {
  let target = SHARE_TARGET_PATH
  try {
    target =
      SHARE_TARGET_PATH + '?' + SHARE_PARAM + '=' + encodeURIComponent(await stageShare(request))
  } catch {
    // Fall through with the bare path.
  }
  return Response.redirect(new URL(target, self.location.origin).toString(), 303)
}

/**
 * The notification tag for a payload's collapse key, or null for none. Mirrors
 * notificationTag in src/pwa/pushContract.ts.
 */
function notificationTag(tag) {
  if (typeof tag !== 'string' || tag === '') {
    return null
  }
  return PUSH_TAG_PREFIX + tag
}

/**
 * The deeplink as a path on this instance; anything that would leave the origin
 * (or is not a path at all) becomes the home page. Mirrors deeplinkPath.
 */
function deeplinkPath(url) {
  if (typeof url !== 'string' || !url.startsWith('/')) {
    return PUSH_HOME_PATH
  }
  if (url.startsWith('//') || url.startsWith('/\\')) {
    return PUSH_HOME_PATH
  }
  return url
}

/** The generic notification an empty or unreadable payload turns into. */
function fallbackDisplay() {
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
 * Reads a push's payload into what to show. Mirrors parsePushPayload in
 * src/pwa/pushContract.ts field for field; never throws — a payload that is
 * missing, not JSON or not an object becomes the generic notification.
 */
function readPush(data) {
  let text = null
  try {
    text = data ? data.text() : null
  } catch {
    return fallbackDisplay()
  }
  if (text === null || text === '') {
    return fallbackDisplay()
  }
  let payload
  try {
    payload = JSON.parse(text)
  } catch {
    return fallbackDisplay()
  }
  if (typeof payload !== 'object' || payload === null || Array.isArray(payload)) {
    return fallbackDisplay()
  }
  const field = (name) => (typeof payload[name] === 'string' ? payload[name] : '')
  const title = field(PUSH_TITLE_FIELD).trim()
  return {
    title: title === '' ? PUSH_FALLBACK_TITLE : title,
    body: field(PUSH_BODY_FIELD),
    icon: PUSH_ICON,
    badge: PUSH_BADGE,
    tag: notificationTag(payload[PUSH_TAG_FIELD]),
    url: deeplinkPath(payload[PUSH_URL_FIELD]),
    kind: field(PUSH_KIND_FIELD),
  }
}

/**
 * Shows one notification. A tagged one replaces the earlier notification with
 * the same tag and re-alerts (`renotify`, which a browser refuses without a
 * tag, hence only then). Should the browser reject the options themselves, the
 * bare generic notification still goes up: a push that shows nothing is the one
 * outcome this handler must never have.
 */
function showPush(display) {
  const options = {
    body: display.body,
    icon: display.icon,
    badge: display.badge,
    data: { url: display.url, kind: display.kind },
  }
  if (display.tag !== null) {
    options.tag = display.tag
    options.renotify = true
  }
  return self.registration.showNotification(display.title, options).catch(() =>
    self.registration.showNotification(PUSH_FALLBACK_TITLE, {
      icon: PUSH_ICON,
      badge: PUSH_BADGE,
      data: { url: PUSH_HOME_PATH, kind: '' },
    }),
  )
}

/**
 * Opens a clicked notification's deeplink. An open Kukátko window is reused —
 * the focused one if any, else the first — by focusing it and navigating it
 * there: an installed PWA has no tabs, so a second window would leave the
 * person with no way back to the first. Only with no window open (or one that
 * refuses to navigate, i.e. a page this worker does not control) does it open a
 * new one.
 */
async function openDeeplink(path) {
  const target = new URL(deeplinkPath(path), self.location.origin).href
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const own = windows.filter((client) => {
    try {
      return new URL(client.url).origin === self.location.origin
    } catch {
      return false
    }
  })
  const client = own.find((candidate) => candidate.focused) || own[0]
  if (client) {
    try {
      const focused = await client.focus()
      await (focused || client).navigate(target)
      return
    } catch {
      // Uncontrolled or gone: fall through to a new window.
    }
  }
  await self.clients.openWindow(target)
}

self.addEventListener('install', (event) => {
  // No skipWaiting: see the header comment on the update flow.
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE)))
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key.startsWith(CACHE_PREFIX) && key !== CACHE_NAME)
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting()
  }
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  // The share sheet's POST: consumed here, never cached, answered with a
  // redirect to the page that picks the files up.
  if (isShareSubmission(request, self.location.origin)) {
    event.respondWith(receiveShare(request))
    return
  }
  if (!isHandled(request, self.location.origin)) {
    return
  }
  if (request.mode === 'navigate') {
    event.respondWith(shellResponse(request))
    return
  }
  const pathname = new URL(request.url).pathname
  if (PRECACHE_SET.has(pathname)) {
    event.respondWith(cacheFirst(request, pathname))
  }
})

self.addEventListener('push', (event) => {
  // Showing *something* is part of the userVisibleOnly promise the subscription
  // made, so the notification goes inside waitUntil, whatever the payload holds.
  event.waitUntil(showPush(readPush(event.data)))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const data = event.notification.data
  event.waitUntil(openDeeplink(data && data.url))
})
