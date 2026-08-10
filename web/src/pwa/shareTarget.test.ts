import { describe, expect, it } from 'vitest'

import {
  SHARE_CACHE,
  SHARE_MODIFIED_HEADER,
  SHARE_NAME_HEADER,
  SHARE_TTL_MS,
  shareEntryPath,
} from './shareContract'
import { collectSharedFiles, discardSharedFiles, partitionSharedFiles } from './shareTarget'

/** "Now" for every test that cares about a share's age. */
const NOW = 1_800_000_000_000

/** One cache, keyed on the path the worker wrote (as the browser's is, by URL). */
class FakeCache {
  readonly entries = new Map<string, Response>()

  keys(): Promise<string[]> {
    return Promise.resolve([...this.entries.keys()])
  }

  match(key: string): Promise<Response | undefined> {
    return Promise.resolve(this.entries.get(key))
  }

  delete(key: string): Promise<boolean> {
    return Promise.resolve(this.entries.delete(key))
  }
}

/** The `caches` global, with only what this module calls. */
class FakeCacheStorage {
  readonly caches = new Map<string, FakeCache>()

  has(name: string): Promise<boolean> {
    return Promise.resolve(this.caches.has(name))
  }

  open(name: string): Promise<FakeCache> {
    let cache = this.caches.get(name)
    if (!cache) {
      cache = new FakeCache()
      this.caches.set(name, cache)
    }
    return Promise.resolve(cache)
  }
}

/**
 * A staged entry as the worker wrote it. Hand-built rather than a real
 * `Response`: jsdom's `Blob`/`File` and the ambient `Response` come from
 * different implementations here, so a real one would hand back a blob that
 * jsdom's `File` constructor stringifies instead of copying. In a browser both
 * are the same realm — this double is what models that faithfully.
 */
function stagedResponse(name: string, type: string, modified: number, body: string): Response {
  return {
    headers: new Headers({
      'Content-Type': type,
      [SHARE_NAME_HEADER]: encodeURIComponent(name),
      [SHARE_MODIFIED_HEADER]: String(modified),
    }),
    blob: () => Promise.resolve(new Blob([body], { type })),
  } as unknown as Response
}

/** The share cache of a fresh storage, with the given entries staged in it. */
function staged(
  entries: { id: string; index: number; name: string; type?: string; modified?: number }[],
): FakeCacheStorage {
  const storage = new FakeCacheStorage()
  const cache = new FakeCache()
  for (const entry of entries) {
    cache.entries.set(
      shareEntryPath(entry.id, entry.index),
      stagedResponse(
        entry.name,
        entry.type ?? 'image/jpeg',
        entry.modified ?? 1234,
        `bytes of ${entry.name}`,
      ),
    )
  }
  storage.caches.set(SHARE_CACHE, cache)
  return storage
}

/** Runs a collection against `storage` with the fixed clock. */
function collect(id: string, storage: FakeCacheStorage) {
  return collectSharedFiles(id, {
    storage: storage as unknown as CacheStorage,
    now: NOW,
  })
}

/** A share id minted `age` milliseconds before {@link NOW}. */
function idAged(age: number, sequence = 1): string {
  return `${String(NOW - age)}-${String(sequence)}`
}

describe('partitionSharedFiles', () => {
  /** A file as the app rebuilds it from a staged entry. */
  function file(name: string, type = ''): File {
    return new File(['x'], name, { type })
  }

  it('takes anything typed as an image or a video', () => {
    const result = partitionSharedFiles([file('a.jpg', 'image/jpeg'), file('b.mp4', 'video/mp4')])

    expect(result.accepted.map((f) => f.name)).toEqual(['a.jpg', 'b.mp4'])
    expect(result.rejected).toEqual([])
  })

  it('takes a known media extension even when the sender labelled it as nothing', () => {
    // A file manager or a messenger routinely hands a photo over as a blob of
    // bytes; refusing those would refuse most RAW and HEIC shares.
    const result = partitionSharedFiles([
      file('IMG_0042.HEIC', 'application/octet-stream'),
      file('DSC_1000.NEF'),
      file('clip.MOV', ''),
    ])

    expect(result.accepted.map((f) => f.name)).toEqual([
      'IMG_0042.HEIC',
      'DSC_1000.NEF',
      'clip.MOV',
    ])
    expect(result.rejected).toEqual([])
  })

  it('names what it will not take, instead of queueing it to fail on the server', () => {
    const result = partitionSharedFiles([
      file('a.jpg', 'image/jpeg'),
      file('smlouva.pdf', 'application/pdf'),
      file('notes.txt', 'text/plain'),
    ])

    expect(result.accepted.map((f) => f.name)).toEqual(['a.jpg'])
    expect(result.rejected).toEqual(['smlouva.pdf', 'notes.txt'])
  })
})

describe('collectSharedFiles', () => {
  it('rebuilds the shared files, in the order they were shared', async () => {
    const storage = staged([
      { id: 's1', index: 1, name: 'b.jpg' },
      { id: 's1', index: 0, name: 'a.jpg' },
      { id: 's1', index: 2, name: 'c.jpg' },
    ])

    const result = await collect('s1', storage)

    expect(result.accepted.map((file) => file.name)).toEqual(['a.jpg', 'b.jpg', 'c.jpg'])
  })

  it('restores the name, type and mtime the worker stashed in headers', async () => {
    const storage = staged([
      { id: 's1', index: 0, name: 'dovolená (1).jpg', type: 'image/jpeg', modified: 4321 },
    ])

    const [file] = (await collect('s1', storage)).accepted

    expect(file.name).toBe('dovolená (1).jpg')
    expect(file.type).toBe('image/jpeg')
    expect(file.lastModified).toBe(4321)
    // The bytes came through as bytes, rather than the blob being stringified.
    expect(file.size).toBe(new TextEncoder().encode('bytes of dovolená (1).jpg').length)
  })

  it('consumes the share, so a reload cannot queue the same photos twice', async () => {
    const storage = staged([{ id: 's1', index: 0, name: 'a.jpg' }])

    expect((await collect('s1', storage)).accepted).toHaveLength(1)
    expect((await collect('s1', storage)).accepted).toEqual([])
    expect(storage.caches.get(SHARE_CACHE)?.entries.size).toBe(0)
  })

  it('leaves another share alone while it is still fresh', async () => {
    const other = idAged(0, 2)
    const storage = staged([
      { id: 's1', index: 0, name: 'a.jpg' },
      { id: other, index: 0, name: 'b.jpg' },
    ])

    await collect('s1', storage)

    expect([...(storage.caches.get(SHARE_CACHE)?.entries.keys() ?? [])]).toEqual([
      shareEntryPath(other, 0),
    ])
  })

  it('sweeps out a share nobody ever came back for', async () => {
    const abandoned = idAged(SHARE_TTL_MS + 1)
    const storage = staged([
      { id: 's1', index: 0, name: 'a.jpg' },
      { id: abandoned, index: 0, name: 'old.jpg' },
    ])

    await collect('s1', storage)

    expect(storage.caches.get(SHARE_CACHE)?.entries.size).toBe(0)
  })

  it('skips entries that are not share keys at all', async () => {
    const storage = staged([{ id: 's1', index: 0, name: 'a.jpg' }])
    storage.caches.get(SHARE_CACHE)?.entries.set('/something/else', new Response('x'))

    const result = await collect('s1', storage)

    expect(result.accepted).toHaveLength(1)
    expect([...(storage.caches.get(SHARE_CACHE)?.entries.keys() ?? [])]).toEqual([
      '/something/else',
    ])
  })

  it('drops an entry whose name header went missing rather than uploading a blob', async () => {
    const storage = staged([{ id: 's1', index: 0, name: 'a.jpg' }])
    storage.caches.get(SHARE_CACHE)?.entries.set(shareEntryPath('s1', 1), new Response('x'))

    const result = await collect('s1', storage)

    expect(result.accepted.map((file) => file.name)).toEqual(['a.jpg'])
    expect(storage.caches.get(SHARE_CACHE)?.entries.size).toBe(0)
  })

  it('reports nothing — instead of throwing — when there is no share cache', async () => {
    await expect(collect('s1', new FakeCacheStorage())).resolves.toEqual({
      accepted: [],
      rejected: [],
    })
  })

  it('reports nothing when the browser has no cache storage at all', async () => {
    await expect(collectSharedFiles('s1', { storage: undefined, now: NOW })).resolves.toEqual({
      accepted: [],
      rejected: [],
    })
  })

  it('reports nothing for an empty id', async () => {
    const storage = staged([{ id: 's1', index: 0, name: 'a.jpg' }])

    await expect(collect('', storage)).resolves.toEqual({ accepted: [], rejected: [] })
    expect(storage.caches.get(SHARE_CACHE)?.entries.size).toBe(1)
  })

  it('survives storage that fails outright', async () => {
    const broken = {
      has: () => Promise.reject(new Error('quota')),
      open: () => Promise.reject(new Error('quota')),
    } as unknown as CacheStorage

    await expect(collectSharedFiles('s1', { storage: broken })).resolves.toEqual({
      accepted: [],
      rejected: [],
    })
  })
})

describe('discardSharedFiles', () => {
  it('throws away just that share, leaving other shares alone', async () => {
    const storage = staged([
      { id: 's1', index: 0, name: 'a.jpg' },
      { id: 's1', index: 1, name: 'b.jpg' },
      { id: 's2', index: 0, name: 'c.jpg' },
    ])

    await discardSharedFiles('s1', { storage: storage as unknown as CacheStorage })

    expect([...(storage.caches.get(SHARE_CACHE)?.entries.keys() ?? [])]).toEqual([
      shareEntryPath('s2', 0),
    ])
  })

  it('does nothing at all without a cache, an id, or a working storage', async () => {
    await expect(discardSharedFiles('')).resolves.toBeUndefined()
    await expect(
      discardSharedFiles('s1', { storage: new FakeCacheStorage() as unknown as CacheStorage }),
    ).resolves.toBeUndefined()
  })
})
