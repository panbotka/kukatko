// Node's own File, not the ambient one: these tests run in the jsdom
// environment, whose Blob/File are not the ones `Response` (undici) knows how to
// read a body from — a jsdom File would be stringified to "[object File]".
import { File as NodeFile } from 'node:buffer'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'

import { describe, expect, it, vi } from 'vitest'

import {
  deeplinkPath,
  notificationTag,
  parsePushPayload,
  PUSH_BADGE,
  PUSH_BODY_FIELD,
  PUSH_FALLBACK_TAG,
  PUSH_FALLBACK_TITLE,
  PUSH_HOME_PATH,
  PUSH_ICON,
  PUSH_KIND_FIELD,
  PUSH_TAG_FIELD,
  PUSH_TITLE_FIELD,
  PUSH_URL_FIELD,
  type PushDisplay,
} from '../src/pwa/pushContract'
import {
  parseShareEntry,
  SHARE_CACHE,
  SHARE_FILES_FIELD,
  SHARE_MODIFIED_HEADER,
  SHARE_NAME_HEADER,
  SHARE_PARAM,
  SHARE_TARGET_PATH,
  shareIdStamp,
} from '../src/pwa/shareContract'

import {
  buildPrecacheManifest,
  cacheNameFor,
  renderServiceWorker,
  serviceWorkerSourcePath,
  SERVICE_WORKER_FILE,
} from './pwa'

/** The file names a real Vite build of this app emits, in bundle order. */
const BUNDLE_FILES = [
  'index.html',
  'assets/index-B1c2d3.js',
  'assets/index-A9z8y7.css',
  'assets/bootstrap-icons-C4d5e6.woff2',
  'assets/marker-icon-D7e8f9.png',
]

/** The worker template, read once — every test renders from the real source. */
const SOURCE = readFileSync(serviceWorkerSourcePath(), 'utf8')

/**
 * A stand-in `Request`. The worker only reads `method`, `url`, `headers.has`
 * and `mode`, so a literal with those is a faithful double and keeps the test
 * independent of jsdom's fetch primitives.
 */
function request(
  url: string,
  { method = 'GET', mode = 'no-cors', range = false } = {},
): { method: string; url: string; mode: string; headers: { has: (name: string) => boolean } } {
  return {
    method,
    url,
    mode,
    headers: { has: (name: string) => range && name === 'range' },
  }
}

/** One cache, backed by a Map keyed on the request path the worker passes. */
class FakeCache {
  readonly entries = new Map<string, unknown>()

  match(key: string): Promise<unknown> {
    return Promise.resolve(this.entries.get(key))
  }

  put(key: string, value: unknown): Promise<void> {
    this.entries.set(key, value)
    return Promise.resolve()
  }

  addAll(keys: string[]): Promise<void> {
    for (const key of keys) {
      this.entries.set(key, { url: key, cached: true })
    }
    return Promise.resolve()
  }
}

/** The `caches` global: named FakeCaches plus keys()/delete(). */
class FakeCacheStorage {
  readonly caches = new Map<string, FakeCache>()

  open(name: string): Promise<FakeCache> {
    let cache = this.caches.get(name)
    if (!cache) {
      cache = new FakeCache()
      this.caches.set(name, cache)
    }
    return Promise.resolve(cache)
  }

  keys(): Promise<string[]> {
    return Promise.resolve([...this.caches.keys()])
  }

  delete(name: string): Promise<boolean> {
    return Promise.resolve(this.caches.delete(name))
  }
}

/**
 * An open window as `clients.matchAll` reports it: its URL, whether it has
 * focus, and the two calls a notification click makes on it. `navigate` rejects
 * when `controlled` is false, as a browser does for a page the worker does not
 * control.
 */
class FakeWindowClient {
  url: string
  readonly focused: boolean
  private readonly controlled: boolean
  readonly focus = vi.fn(() => Promise.resolve(this))
  readonly navigate = vi.fn((url: string) =>
    this.controlled
      ? Promise.resolve(Object.assign(this, { url }))
      : Promise.reject(new TypeError('not controlled')),
  )

  constructor(url: string, focused = false, controlled = true) {
    this.url = url
    this.focused = focused
    this.controlled = controlled
  }
}

/** A worker under test: its global scope, its listeners and its caches. */
interface Harness {
  listeners: Map<string, (event: unknown) => void>
  cacheStorage: FakeCacheStorage
  cacheName: string
  skipWaiting: ReturnType<typeof vi.fn>
  claim: ReturnType<typeof vi.fn>
  fetch: ReturnType<typeof vi.fn>
  /** `registration.showNotification`; resolves unless a test makes it reject. */
  showNotification: ReturnType<typeof vi.fn>
  /** `clients.matchAll`, answering with {@link Harness.windows}. */
  matchAll: ReturnType<typeof vi.fn>
  /** `clients.openWindow`. */
  openWindow: ReturnType<typeof vi.fn>
  /** The windows `clients.matchAll` reports; tests push into it. */
  windows: FakeWindowClient[]
  /** Dispatches a fetch event and resolves with what the worker answered, or null. */
  handleFetch: (input: unknown) => Promise<unknown>
}

/**
 * A share POST as the phone's share sheet makes it: a multipart form whose
 * `files` field holds the shared files. Only `formData()` is read, so the double
 * stays independent of undici's multipart parsing.
 */
function shareRequest(
  parts: unknown[],
  { path = SHARE_TARGET_PATH, origin = 'https://kukatko.test', failing = false } = {},
) {
  return {
    method: 'POST',
    url: `${origin}${path}`,
    mode: 'navigate',
    headers: { has: () => false },
    formData: () =>
      failing
        ? Promise.reject(new Error('unreadable payload'))
        : Promise.resolve({
            getAll: (field: string) => (field === SHARE_FILES_FIELD ? parts : []),
          }),
  }
}

/** The staged entries of the share cache, as `{ id, index, response }`, in key order. */
function stagedEntries(harness: Harness) {
  const cache = harness.cacheStorage.caches.get(SHARE_CACHE)
  return [...(cache?.entries.entries() ?? [])].flatMap(([key, value]) => {
    const entry = parseShareEntry(key)
    return entry === null ? [] : [{ ...entry, response: value as Response }]
  })
}

/** The share id the worker redirected to, or null when it sent none. */
function redirectedShareId(response: unknown): string | null {
  const location = (response as Response).headers.get('location')
  return location === null ? null : new URL(location).searchParams.get(SHARE_PARAM)
}

/**
 * Renders the worker for `manifest` and runs it in a fabricated
 * ServiceWorkerGlobalScope, returning the handles a test needs to drive it.
 *
 * Running the real emitted source is the point: the caching rules are the thing
 * that must not regress, and asserting on the text of the file would prove
 * nothing about how it behaves.
 */
function runWorker(manifest: string[], origin = 'https://kukatko.test'): Harness {
  const code = renderServiceWorker(SOURCE, manifest)
  const listeners = new Map<string, (event: unknown) => void>()
  const cacheStorage = new FakeCacheStorage()
  const skipWaiting = vi.fn()
  const claim = vi.fn(() => Promise.resolve())
  const fetchMock = vi.fn(() => Promise.resolve({ ok: true, url: 'network', clone: () => ({}) }))
  const showNotification = vi.fn((_title: string, _options?: unknown) => Promise.resolve())
  const windows: FakeWindowClient[] = []
  const matchAll = vi.fn((_options?: unknown) => Promise.resolve([...windows]))
  const openWindow = vi.fn((_url: string) => Promise.resolve(null))

  const self = {
    location: { origin },
    clients: { claim, matchAll, openWindow },
    registration: { showNotification },
    skipWaiting,
    addEventListener: (type: string, handler: (event: unknown) => void) => {
      listeners.set(type, handler)
    },
  }

  // A classic worker script reads its globals off the scope it runs in, so give
  // it a real one: a fresh VM context holding exactly the globals it touches.
  // Set/Promise/Error come with the new realm; the host objects do not.
  runInNewContext(code, {
    self,
    caches: cacheStorage,
    fetch: fetchMock,
    Response,
    URL,
  })

  return {
    listeners,
    cacheStorage,
    cacheName: cacheNameFor(manifest),
    skipWaiting,
    claim,
    fetch: fetchMock,
    showNotification,
    matchAll,
    openWindow,
    windows,
    handleFetch: async (input: unknown) => {
      const handler = listeners.get('fetch')
      if (!handler) {
        throw new Error('worker registered no fetch handler')
      }
      // Captured on an object rather than in a `let`: the worker answers from
      // inside the callback, and TypeScript keeps narrowing a reassigned local
      // to its initialiser across the call that mutates it.
      const captured: { response: Promise<unknown> | null } = { response: null }
      handler({
        request: input,
        respondWith: (response: Promise<unknown>) => {
          captured.response = response
        },
      })
      return captured.response
    },
  }
}

/** Installs the worker (populating its cache) and returns the harness. */
async function installed(manifest: string[]): Promise<Harness> {
  const harness = runWorker(manifest)
  const install = harness.listeners.get('install')
  if (!install) {
    throw new Error('worker registered no install handler')
  }
  let waited: Promise<unknown> = Promise.resolve()
  install({
    waitUntil: (promise: Promise<unknown>) => {
      waited = promise
    },
  })
  await waited
  return harness
}

describe('buildPrecacheManifest', () => {
  it('lists the shell document first, then every hashed asset', () => {
    expect(buildPrecacheManifest(BUNDLE_FILES)).toEqual([
      '/index.html',
      '/assets/bootstrap-icons-C4d5e6.woff2',
      '/assets/index-A9z8y7.css',
      '/assets/index-B1c2d3.js',
      '/assets/marker-icon-D7e8f9.png',
    ])
  })

  it('never precaches the worker itself', () => {
    const manifest = buildPrecacheManifest([...BUNDLE_FILES, SERVICE_WORKER_FILE])

    expect(manifest).not.toContain(`/${SERVICE_WORKER_FILE}`)
  })

  it('ignores emitted artefacts that are not shell assets', () => {
    const manifest = buildPrecacheManifest([
      ...BUNDLE_FILES,
      'assets/index-B1c2d3.js.map',
      'stats.txt',
    ])

    expect(manifest).not.toContain('/stats.txt')
    expect(manifest).not.toContain('/assets/index-B1c2d3.js.map')
  })

  it('is order-independent, so the same bundle always yields the same manifest', () => {
    const reversed = [...BUNDLE_FILES].reverse()

    expect(buildPrecacheManifest(reversed)).toEqual(buildPrecacheManifest(BUNDLE_FILES))
  })
})

describe('cacheNameFor', () => {
  it('keeps the cache name stable for an unchanged shell', () => {
    const manifest = buildPrecacheManifest(BUNDLE_FILES)

    expect(cacheNameFor(manifest)).toBe(cacheNameFor([...manifest]))
  })

  it('changes the cache name as soon as any asset URL changes', () => {
    const before = buildPrecacheManifest(BUNDLE_FILES)
    const after = buildPrecacheManifest(['index.html', 'assets/index-NEWHASH.js'])

    expect(cacheNameFor(after)).not.toBe(cacheNameFor(before))
  })
})

describe('renderServiceWorker', () => {
  it('substitutes both build-time placeholders', () => {
    const manifest = buildPrecacheManifest(BUNDLE_FILES)

    const rendered = renderServiceWorker(SOURCE, manifest)

    expect(rendered).not.toContain('__KUKATKO_PRECACHE__')
    expect(rendered).not.toContain('__KUKATKO_CACHE_NAME__')
    expect(rendered).toContain(cacheNameFor(manifest))
  })

  it('refuses a source whose placeholders have gone missing', () => {
    expect(() => renderServiceWorker('const CACHE = 1', [])).toThrow(/placeholders/)
  })
})

describe('the rendered service worker', () => {
  const manifest = ['/index.html', '/assets/index-B1c2d3.js']

  it('precaches the whole manifest on install without taking over', async () => {
    const harness = await installed(manifest)

    const cache = harness.cacheStorage.caches.get(harness.cacheName)
    expect([...(cache?.entries.keys() ?? [])]).toEqual(manifest)
    expect(harness.skipWaiting).not.toHaveBeenCalled()
  })

  it('serves a precached asset from the cache without touching the network', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(
      request('https://kukatko.test/assets/index-B1c2d3.js'),
    )

    expect(response).toEqual({ url: '/assets/index-B1c2d3.js', cached: true })
    expect(harness.fetch).not.toHaveBeenCalled()
  })

  it('answers a navigation with the precached shell', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(
      request('https://kukatko.test/albums/42', { mode: 'navigate' }),
    )

    expect(response).toEqual({ url: '/index.html', cached: true })
  })

  it.each([
    ['the API', 'https://kukatko.test/api/v1/photos'],
    ['a thumbnail', 'https://kukatko.test/api/v1/photos/abc/thumb/tile_500'],
    ['an original download', 'https://kukatko.test/api/v1/photos/abc/download?original=true'],
    ['the health endpoint', 'https://kukatko.test/healthz'],
    ['the metrics endpoint', 'https://kukatko.test/metrics'],
    ['signed media on another origin', 'https://media.example.com/2026/08/abc.jpg'],
  ])('never intercepts %s', async (_label, url) => {
    const harness = await installed(manifest)

    expect(await harness.handleFetch(request(url))).toBeNull()
  })

  it('never intercepts a ranged request, so video scrubbing is untouched', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(
      request('https://kukatko.test/assets/index-B1c2d3.js', { range: true }),
    )

    expect(response).toBeNull()
  })

  it('never intercepts a non-GET request', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(
      request('https://kukatko.test/assets/index-B1c2d3.js', { method: 'POST' }),
    )

    expect(response).toBeNull()
  })

  it('leaves an unprecached same-origin asset to the browser', async () => {
    const harness = await installed(manifest)

    expect(await harness.handleFetch(request('https://kukatko.test/icons/kukatko.svg'))).toBeNull()
  })

  it('refetches and restores a precached asset the browser evicted', async () => {
    const harness = await installed(manifest)
    harness.cacheStorage.caches.get(harness.cacheName)?.entries.delete('/assets/index-B1c2d3.js')

    const response = await harness.handleFetch(
      request('https://kukatko.test/assets/index-B1c2d3.js'),
    )

    expect(harness.fetch).toHaveBeenCalledTimes(1)
    expect(response).toMatchObject({ url: 'network' })
    expect(
      harness.cacheStorage.caches.get(harness.cacheName)?.entries.has('/assets/index-B1c2d3.js'),
    ).toBe(true)
  })

  it('activates only when the page asks it to, and then claims its clients', async () => {
    const harness = await installed(manifest)

    harness.listeners.get('message')?.({ data: { type: 'SKIP_WAITING' } })
    expect(harness.skipWaiting).toHaveBeenCalledTimes(1)

    harness.cacheStorage.caches.set('kukatko-shell-oldbuild', new FakeCache())
    let waited: Promise<unknown> = Promise.resolve()
    harness.listeners.get('activate')?.({
      waitUntil: (promise: Promise<unknown>) => {
        waited = promise
      },
    })
    await waited

    expect([...harness.cacheStorage.caches.keys()]).toEqual([harness.cacheName])
    expect(harness.claim).toHaveBeenCalledTimes(1)
  })

  it('ignores a message that is not the update handshake', async () => {
    const harness = await installed(manifest)

    harness.listeners.get('message')?.({ data: { type: 'something-else' } })
    harness.listeners.get('message')?.({ data: null })

    expect(harness.skipWaiting).not.toHaveBeenCalled()
  })
})

/**
 * The share target: the one non-GET the worker answers. These drive the real
 * worker against the constants in src/pwa/shareContract.ts — the module the
 * *app* reads the staged files with — so the two halves cannot drift apart
 * without a test going red.
 */
describe('the rendered service worker, receiving a share', () => {
  const manifest = ['/index.html', '/assets/index-B1c2d3.js']

  /** A shared file as the share sheet hands it over. */
  function shared(name: string, type: string, modified = 1_700_000_000_000): NodeFile {
    return new NodeFile([`bytes of ${name}`], name, { type, lastModified: modified })
  }

  it('stages every shared file and redirects to the share page carrying its id', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(
      shareRequest([shared('a.jpg', 'image/jpeg'), shared('b.mp4', 'video/mp4')]),
    )

    expect((response as Response).status).toBe(303)
    const id = redirectedShareId(response)
    expect(id).not.toBeNull()
    expect(new URL((response as Response).headers.get('location') ?? '').pathname).toBe(
      SHARE_TARGET_PATH,
    )

    const entries = stagedEntries(harness)
    expect(entries.map((entry) => entry.index)).toEqual([0, 1])
    expect(entries.every((entry) => entry.id === id)).toBe(true)
  })

  it('carries the name and mtime a Response body cannot, and the bytes themselves', async () => {
    const harness = await installed(manifest)

    await harness.handleFetch(shareRequest([shared('dovolená (1).jpg', 'image/jpeg', 1234)]))

    const [entry] = stagedEntries(harness)
    expect(entry.response.headers.get(SHARE_NAME_HEADER)).toBe(
      encodeURIComponent('dovolená (1).jpg'),
    )
    expect(entry.response.headers.get(SHARE_MODIFIED_HEADER)).toBe('1234')
    expect(entry.response.headers.get('content-type')).toBe('image/jpeg')
    await expect(entry.response.text()).resolves.toBe('bytes of dovolená (1).jpg')
  })

  it('mints an id that carries the moment it was staged, so a share can expire', async () => {
    const before = Date.now()
    const harness = await installed(manifest)

    const id = redirectedShareId(await harness.handleFetch(shareRequest([shared('a.jpg', '')])))

    const stamp = shareIdStamp(id ?? '')
    expect(stamp).not.toBeNull()
    expect(stamp ?? 0).toBeGreaterThanOrEqual(before)
  })

  it('gives each share its own id, so two shares never overwrite each other', async () => {
    const harness = await installed(manifest)

    const first = redirectedShareId(await harness.handleFetch(shareRequest([shared('a.jpg', '')])))
    const second = redirectedShareId(await harness.handleFetch(shareRequest([shared('b.jpg', '')])))

    expect(first).not.toBe(second)
    expect(new Set(stagedEntries(harness).map((entry) => entry.id)).size).toBe(2)
  })

  it('ignores the shared title, text and url — there is nowhere to put a sentence', async () => {
    const harness = await installed(manifest)

    await harness.handleFetch(shareRequest(['a shared caption', shared('a.jpg', 'image/jpeg')]))

    const entries = stagedEntries(harness)
    expect(entries).toHaveLength(1)
    expect(entries[0].response.headers.get(SHARE_NAME_HEADER)).toBe('a.jpg')
  })

  it('never caches the POST itself, and leaves the shell cache untouched', async () => {
    const harness = await installed(manifest)

    await harness.handleFetch(shareRequest([shared('a.jpg', 'image/jpeg')]))

    const shell = harness.cacheStorage.caches.get(harness.cacheName)
    expect([...(shell?.entries.keys() ?? [])]).toEqual(manifest)
    expect([...(harness.cacheStorage.caches.get(SHARE_CACHE)?.entries.keys() ?? [])]).not.toContain(
      SHARE_TARGET_PATH,
    )
  })

  it('still redirects — with no id — when the shared payload cannot be read', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(shareRequest([], { failing: true }))

    expect((response as Response).status).toBe(303)
    expect(redirectedShareId(response)).toBeNull()
    expect(stagedEntries(harness)).toEqual([])
  })

  it('redirects with an id even for a share that turned out to hold no files', async () => {
    const harness = await installed(manifest)

    const response = await harness.handleFetch(shareRequest([]))

    expect(redirectedShareId(response)).not.toBeNull()
    expect(stagedEntries(harness)).toEqual([])
  })

  it('leaves a POST to any other path to the browser', async () => {
    const harness = await installed(manifest)

    expect(await harness.handleFetch(shareRequest([], { path: '/api/v1/upload' }))).toBeNull()
  })

  it('leaves a cross-origin POST to the share path to the browser', async () => {
    const harness = await installed(manifest)

    expect(
      await harness.handleFetch(shareRequest([], { origin: 'https://elsewhere.test' })),
    ).toBeNull()
  })

  it('keeps staged files across an activation that prunes the old shell', async () => {
    const harness = await installed(manifest)
    await harness.handleFetch(shareRequest([shared('a.jpg', 'image/jpeg')]))
    harness.cacheStorage.caches.set('kukatko-shell-oldbuild', new FakeCache())

    let waited: Promise<unknown> = Promise.resolve()
    harness.listeners.get('activate')?.({
      waitUntil: (promise: Promise<unknown>) => {
        waited = promise
      },
    })
    await waited

    // A deployment landing between the share and its collection must not eat
    // the user's photos: only shell caches are pruned.
    expect([...harness.cacheStorage.caches.keys()].sort()).toEqual([SHARE_CACHE, harness.cacheName])
    expect(stagedEntries(harness)).toHaveLength(1)
  })
})

/**
 * Push notifications: the `push` and `notificationclick` handlers. These drive
 * the real worker against src/pwa/pushContract.ts — the module that documents
 * the payload the server sends and what the worker makes of it — so the
 * worker's copy of those rules cannot drift from the contract without a test
 * going red.
 */
describe('the rendered service worker, receiving a push', () => {
  const manifest = ['/index.html', '/assets/index-B1c2d3.js']
  const origin = 'https://kukatko.test'

  /** Stands for a payload whose `text()` throws (one the browser cannot decrypt). */
  const UNREADABLE = Symbol('unreadable')

  /** The payload of a push, as the server writes it (`push.Notification`). */
  function payload(fields: Record<string, unknown>): string {
    return JSON.stringify(fields)
  }

  /**
   * Dispatches a push carrying `data` — the text of its payload, null for a
   * push without one, or {@link UNREADABLE} for a payload whose `text()` throws —
   * and resolves once the worker's waitUntil settles.
   */
  async function push(harness: Harness, data: string | null | typeof UNREADABLE): Promise<void> {
    let waited: Promise<unknown> = Promise.resolve()
    const pushData =
      data === null
        ? null
        : {
            text: () => {
              if (data === UNREADABLE) {
                throw new Error('undecryptable payload')
              }
              return data
            },
          }
    harness.listeners.get('push')?.({
      data: pushData,
      waitUntil: (promise: Promise<unknown>) => {
        waited = promise
      },
    })
    await waited
  }

  /** What the worker is expected to pass to showNotification for `display`. */
  function expectedCall(display: PushDisplay): [string, Record<string, unknown>] {
    const options: Record<string, unknown> = {
      body: display.body,
      icon: display.icon,
      badge: display.badge,
      data: { url: display.url, kind: display.kind },
    }
    if (display.tag !== null) {
      options.tag = display.tag
      options.renotify = true
    }
    return [display.title, options]
  }

  /** The one notification the worker showed, as [title, options]. */
  function shown(harness: Harness): [string, Record<string, unknown>] {
    expect(harness.showNotification).toHaveBeenCalledTimes(1)
    const [title, options] = harness.showNotification.mock.calls[0] as [
      string,
      Record<string, unknown>,
    ]
    // The options object was built in the worker's realm; bring it into this
    // one so the structural comparison sees plain objects.
    return [title, JSON.parse(JSON.stringify(options)) as Record<string, unknown>]
  }

  /** Dispatches a click on a notification carrying `data` and waits for it. */
  async function click(harness: Harness, data: unknown): Promise<ReturnType<typeof vi.fn>> {
    const close = vi.fn()
    let waited: Promise<unknown> = Promise.resolve()
    harness.listeners.get('notificationclick')?.({
      notification: { data, close },
      waitUntil: (promise: Promise<unknown>) => {
        waited = promise
      },
    })
    await waited
    return close
  }

  it('shows the title, body, app icon, badge, collapse tag and deeplink of a payload', async () => {
    const harness = await installed(manifest)

    await push(
      harness,
      payload({
        [PUSH_TITLE_FIELD]: 'Byli jste označeni',
        [PUSH_BODY_FIELD]: 'Na 3 fotkách',
        [PUSH_URL_FIELD]: '/notifications/nt_1',
        [PUSH_KIND_FIELD]: 'tagged',
        [PUSH_TAG_FIELD]: 'nt_1',
      }),
    )

    expect(shown(harness)).toEqual([
      'Byli jste označeni',
      {
        body: 'Na 3 fotkách',
        icon: PUSH_ICON,
        badge: PUSH_BADGE,
        tag: notificationTag('nt_1'),
        renotify: true,
        data: { url: '/notifications/nt_1', kind: 'tagged' },
      },
    ])
  })

  it('gives two pushes of one collapse key the same tag, so the second replaces the first', async () => {
    const harness = await installed(manifest)
    const first = payload({ [PUSH_TITLE_FIELD]: 'Jedna', [PUSH_TAG_FIELD]: 'tagged-u1' })
    const second = payload({ [PUSH_TITLE_FIELD]: 'Dvě', [PUSH_TAG_FIELD]: 'tagged-u1' })

    await push(harness, first)
    await push(harness, second)

    const tags = harness.showNotification.mock.calls.map(
      (call) => (call[1] as { tag?: string }).tag,
    )
    expect(tags).toEqual([notificationTag('tagged-u1'), notificationTag('tagged-u1')])
  })

  it.each([
    [
      'a complete payload',
      payload({ title: 'T', body: 'B', url: '/tasks/t1', kind: 'k', tag: 'x' }),
    ],
    ['a payload without a collapse key', payload({ title: 'T', body: 'B', url: '/', tag: '' })],
    ['a payload with a blank title', payload({ title: '   ', body: 'B', url: '/albums/a1' })],
    ['a payload with no fields at all', payload({})],
    ['fields of the wrong type', payload({ title: 7, body: null, url: 42, kind: [], tag: {} })],
    ['an absolute deeplink', payload({ title: 'T', url: 'https://evil.example/x' })],
    ['a scheme-relative deeplink', payload({ title: 'T', url: '//evil.example/x' })],
    ['a backslash deeplink', payload({ title: 'T', url: '/\\evil.example/x' })],
    ['a relative deeplink', payload({ title: 'T', url: 'photos/p1' })],
    ['text that is not JSON', 'not { json'],
    ['a JSON array', '["title"]'],
    ['a JSON string', '"title"'],
    ['JSON null', 'null'],
    ['an empty payload', ''],
    ['a push without a payload', null],
  ])('shows exactly what the contract says for %s', async (_label, text) => {
    const harness = await installed(manifest)

    await push(harness, text)

    expect(shown(harness)).toEqual(expectedCall(parsePushPayload(text)))
  })

  it('still shows a generic Kukátko notification for a payload it cannot read', async () => {
    const harness = await installed(manifest)

    await push(harness, UNREADABLE)

    const [title, options] = shown(harness)
    expect(title).toBe(PUSH_FALLBACK_TITLE)
    expect(options).toMatchObject({
      badge: PUSH_BADGE,
      tag: PUSH_FALLBACK_TAG,
      data: { url: PUSH_HOME_PATH },
    })
  })

  it('falls back to the bare generic notification when the browser rejects the options', async () => {
    const harness = await installed(manifest)
    harness.showNotification.mockImplementationOnce(() =>
      Promise.reject(new TypeError('bad options')),
    )

    await push(harness, payload({ title: 'T', tag: 'x' }))

    expect(harness.showNotification).toHaveBeenCalledTimes(2)
    expect(harness.showNotification.mock.calls[1][0]).toBe(PUSH_FALLBACK_TITLE)
  })

  it('focuses and navigates the open Kukátko window instead of opening a second one', async () => {
    const harness = await installed(manifest)
    const open = new FakeWindowClient(`${origin}/albums`)
    harness.windows.push(open)

    const close = await click(harness, { url: '/tasks/t1', kind: 'task' })

    expect(close).toHaveBeenCalledTimes(1)
    expect(open.focus).toHaveBeenCalledTimes(1)
    expect(open.navigate).toHaveBeenCalledWith(`${origin}/tasks/t1`)
    expect(harness.openWindow).not.toHaveBeenCalled()
    expect(harness.matchAll).toHaveBeenCalledWith({ type: 'window', includeUncontrolled: true })
  })

  it('prefers the window that has focus when several are open', async () => {
    const harness = await installed(manifest)
    const background = new FakeWindowClient(`${origin}/`)
    const foreground = new FakeWindowClient(`${origin}/people`, true)
    harness.windows.push(background, foreground)

    await click(harness, { url: '/photos/p1' })

    expect(foreground.navigate).toHaveBeenCalledWith(`${origin}/photos/p1`)
    expect(background.navigate).not.toHaveBeenCalled()
  })

  it('opens a window on the deeplink when no Kukátko window is open', async () => {
    const harness = await installed(manifest)
    harness.windows.push(new FakeWindowClient('https://elsewhere.test/'))

    const close = await click(harness, { url: '/notifications/nt_1' })

    expect(close).toHaveBeenCalledTimes(1)
    expect(harness.openWindow).toHaveBeenCalledWith(`${origin}/notifications/nt_1`)
  })

  it('opens a window when the open one refuses to navigate', async () => {
    const harness = await installed(manifest)
    harness.windows.push(new FakeWindowClient(`${origin}/`, true, false))

    await click(harness, { url: '/tasks/t1' })

    expect(harness.openWindow).toHaveBeenCalledWith(`${origin}/tasks/t1`)
  })

  it.each([
    ['an off-origin deeplink', { url: 'https://evil.example/' }],
    ['no deeplink', { kind: 'tagged' }],
    ['no data at all', null],
  ])('sends a click on %s to the home page', async (_label, data) => {
    const harness = await installed(manifest)

    await click(harness, data)

    const target = new URL(deeplinkPath((data as { url?: unknown } | null)?.url), origin).href
    expect(target).toBe(`${origin}${PUSH_HOME_PATH}`)
    expect(harness.openWindow).toHaveBeenCalledWith(target)
  })

  it('adds only the two push listeners, and leaves the fetch whitelist as it was', async () => {
    const harness = await installed(manifest)
    await push(harness, payload({ title: 'T' }))

    expect([...harness.listeners.keys()].sort()).toEqual([
      'activate',
      'fetch',
      'install',
      'message',
      'notificationclick',
      'push',
    ])
    expect(await harness.handleFetch(request(`${origin}/api/v1/push/config`))).toBeNull()
    expect(await harness.handleFetch(request(`${origin}${PUSH_ICON}`))).toBeNull()
    expect(await harness.handleFetch(request(`${origin}${PUSH_BADGE}`))).toBeNull()
    expect(await harness.handleFetch(request(`${origin}/assets/index-B1c2d3.js`))).toEqual({
      url: '/assets/index-B1c2d3.js',
      cached: true,
    })
  })
})

/**
 * The two notification images are plain files under web/public, so nothing but
 * this test notices when a path in the contract stops matching a real file.
 */
describe('the notification images', () => {
  const publicDir = resolve(dirname(fileURLToPath(import.meta.url)), '../public')

  it.each([
    ['the icon', PUSH_ICON, 192],
    ['the badge', PUSH_BADGE, 96],
  ])('ships %s as a square PNG of the promised size', (_label, path, size) => {
    const png = readFileSync(resolve(publicDir, `.${path}`))

    // The IHDR chunk: width and height as big-endian words at offsets 16 and 20.
    expect(png.subarray(1, 4).toString('ascii')).toBe('PNG')
    expect([png.readUInt32BE(16), png.readUInt32BE(20)]).toEqual([size, size])
  })
})

/**
 * The manifest is the other half of the share contract — it decides which POST
 * the worker will ever see — so it is held to the same constants.
 */
describe('the web app manifest', () => {
  interface ShareTarget {
    action: string
    method: string
    enctype: string
    params: { files: { name: string; accept: string[] }[] }
  }

  const manifest = JSON.parse(
    readFileSync(
      resolve(dirname(fileURLToPath(import.meta.url)), '../public/manifest.webmanifest'),
      'utf8',
    ),
  ) as { share_target?: ShareTarget; shortcuts?: { url: string }[] }

  it('declares a share target the worker actually intercepts', () => {
    expect(manifest.share_target?.action).toBe(SHARE_TARGET_PATH)
    expect(manifest.share_target?.method).toBe('POST')
    expect(manifest.share_target?.enctype).toBe('multipart/form-data')
  })

  it('names the file field the worker reads, and asks for photos and videos', () => {
    const files = manifest.share_target?.params.files[0]

    expect(files?.name).toBe(SHARE_FILES_FIELD)
    expect(files?.accept).toEqual(['image/*', 'video/*'])
  })

  it('offers a shortcut straight to the upload page', () => {
    expect(manifest.shortcuts?.map((shortcut) => shortcut.url)).toContain('/upload')
  })
})
