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
