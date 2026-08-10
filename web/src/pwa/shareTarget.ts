/**
 * Browser-side half of the share target: reading back the files the service
 * worker staged for a share, and deciding which of them Kukátko can take.
 *
 * See `shareContract.ts` for the whole flow and for the names both halves agree
 * on. Everything here is defensive by design: the cache is per-origin storage
 * that a stale worker, a previous release or a half-finished share can all have
 * written into, so a malformed entry is skipped rather than allowed to throw on
 * the upload page.
 */
import { isMediaFile } from '../lib/mediaFiles'

import {
  isShareExpired,
  parseShareEntry,
  SHARE_CACHE,
  SHARE_MODIFIED_HEADER,
  SHARE_NAME_HEADER,
} from './shareContract'

/** What one share turned out to hold, once triaged. */
export interface SharedFiles {
  /** Files that can go into the upload queue, in the order they were shared. */
  accepted: File[]
  /** Names of files that are neither photo nor video, for an honest message. */
  rejected: string[]
}

/** An empty result, for every "there is nothing to collect" branch. */
const NOTHING: SharedFiles = { accepted: [], rejected: [] }

/** Overrides for the tests, which have neither a real Cache Storage nor a clock. */
export interface CollectOptions {
  /** The Cache Storage to read; defaults to the global one. */
  storage?: CacheStorage
  /** "Now" in epoch milliseconds, for expiring abandoned shares. */
  now?: number
}

/** The Cache Storage to use, or null where the browser exposes none (old iOS, http). */
function storageOf(options: CollectOptions): CacheStorage | null {
  const storage = options.storage ?? (typeof caches === 'undefined' ? undefined : caches)
  return storage ?? null
}

/** The pathname of a cache key, whether it came back as a Request or a URL string. */
function pathnameOf(key: Request | string): string | null {
  const url = typeof key === 'string' ? key : key.url
  try {
    // The stored keys are root-absolute paths; the base only matters for parsing.
    return new URL(url, 'https://kukatko.invalid').pathname
  } catch {
    return null
  }
}

/**
 * Rebuilds the original `File` from a staged response: the body is the bytes,
 * the name and modification time ride along in headers (a `Response` cannot
 * carry them any other way). Returns null for an entry that lost its name, which
 * would upload as an anonymous blob.
 */
async function fileFrom(response: Response): Promise<File | null> {
  const encoded = response.headers.get(SHARE_NAME_HEADER)
  if (encoded === null || encoded === '') {
    return null
  }
  let name: string
  try {
    name = decodeURIComponent(encoded)
  } catch {
    return null
  }
  const modified = Number(response.headers.get(SHARE_MODIFIED_HEADER))
  const blob = await response.blob()
  return new File([blob], name, {
    type: blob.type,
    lastModified: Number.isFinite(modified) && modified > 0 ? modified : Date.now(),
  })
}

/**
 * Splits shared files into the ones the upload queue should take and the names
 * of the ones it should not. Pure, so the rule is unit-testable on its own; the
 * rule itself (MIME type or known extension) lives in `lib/mediaFiles`.
 */
export function partitionSharedFiles(files: readonly File[]): SharedFiles {
  const accepted: File[] = []
  const rejected: string[] = []
  for (const file of files) {
    if (isMediaFile(file)) {
      accepted.push(file)
    } else {
      rejected.push(file.name)
    }
  }
  return { accepted, rejected }
}

/**
 * Collects the files staged for `id`, removing them from the cache as it goes: a
 * share is handed over exactly once, so a reload of the upload page cannot queue
 * the same photos twice.
 *
 * The sweep doubles as housekeeping — any *other* share older than the TTL is
 * dropped on the way past, which is the only place abandoned shares are ever
 * cleaned up (see `shareContract.ts`).
 *
 * Never rejects: a browser without Cache Storage, a missing cache, an entry
 * written by something else — all of them mean "no files", and the page says so
 * rather than breaking.
 */
export async function collectSharedFiles(
  id: string,
  options: CollectOptions = {},
): Promise<SharedFiles> {
  const storage = storageOf(options)
  if (storage === null || id === '') {
    return NOTHING
  }
  const now = options.now ?? Date.now()

  try {
    if (!(await storage.has(SHARE_CACHE))) {
      return NOTHING
    }
    const cache = await storage.open(SHARE_CACHE)
    const mine: { index: number; key: Request | string }[] = []

    for (const key of await cache.keys()) {
      const pathname = pathnameOf(key)
      const entry = pathname === null ? null : parseShareEntry(pathname)
      if (entry === null) {
        continue
      }
      if (entry.id === id) {
        mine.push({ index: entry.index, key })
      } else if (isShareExpired(entry.id, now)) {
        await cache.delete(key)
      }
    }

    // The files go into the queue in the order they were shared, not in whatever
    // order the cache happens to enumerate its keys.
    mine.sort((a, b) => a.index - b.index)

    const files: File[] = []
    for (const { key } of mine) {
      const response = await cache.match(key)
      if (response) {
        const file = await fileFrom(response)
        if (file !== null) {
          files.push(file)
        }
      }
      await cache.delete(key)
    }
    return partitionSharedFiles(files)
  } catch {
    // Storage can fail for reasons the page cannot fix (quota, a private-mode
    // browser). Behaving as if the share were empty keeps the upload page usable.
    return NOTHING
  }
}

/**
 * Throws away a staged share without reading it. Used when the share can never
 * be uploaded — a viewer's account may not — so their photos do not linger in
 * the cache until the TTL sweeps them.
 */
export async function discardSharedFiles(id: string, options: CollectOptions = {}): Promise<void> {
  const storage = storageOf(options)
  if (storage === null || id === '') {
    return
  }
  try {
    if (!(await storage.has(SHARE_CACHE))) {
      return
    }
    const cache = await storage.open(SHARE_CACHE)
    for (const key of await cache.keys()) {
      const pathname = pathnameOf(key)
      const entry = pathname === null ? null : parseShareEntry(pathname)
      if (entry !== null && entry.id === id) {
        await cache.delete(key)
      }
    }
  } catch {
    // Nothing to do: the TTL sweep will get to it.
  }
}
