import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

import { describe, expect, it, vi } from 'vitest'

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

/** A worker under test: its global scope, its listeners and its caches. */
interface Harness {
  listeners: Map<string, (event: unknown) => void>
  cacheStorage: FakeCacheStorage
  cacheName: string
  skipWaiting: ReturnType<typeof vi.fn>
  claim: ReturnType<typeof vi.fn>
  fetch: ReturnType<typeof vi.fn>
  /** Dispatches a fetch event and resolves with what the worker answered, or null. */
  handleFetch: (input: ReturnType<typeof request>) => Promise<unknown>
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

  const self = {
    location: { origin },
    clients: { claim },
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
    handleFetch: async (input) => {
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
