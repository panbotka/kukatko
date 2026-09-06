import { type PhotoDetail } from '../services/photos'

/**
 * The gaps between the polls that wait for a saved edit's thumbnail rebuild, in
 * order; once they run out the watch gives up (about a minute and a half). The
 * first poll is immediate. Short at first because the rebuild of one photo is
 * seconds of work on an idle worker, then sparser so a busy queue is not hammered.
 */
export const REBUILD_POLL_DELAYS_MS: readonly number[] = [
  1000, 1500, 2000, 3000, 5000, 5000, 8000, 10000, 10000, 15000, 15000, 15000,
]

/**
 * Whether the photo's thumbnails were built at or after `since` — the moment a
 * save stamped on the edit (`updated_at`) — according to the detail's
 * `processing` report. Both stamps are the server's own clock, so the comparison
 * never involves this device's.
 *
 * The thumbnail step reads `done` from the moment the first thumbnails exist,
 * whatever the queue holds (persisted evidence wins in `internal/processing`),
 * so `done` alone says nothing about a rebuild; its `at` is what moves, because
 * the job restamps the perceptual-hash row it writes right after the renditions.
 * A detail without a processing report (an instance that wires none) can never
 * answer yes, which errs on the safe side: the stage keeps its delta preview.
 */
export function thumbnailRebuiltSince(photo: PhotoDetail, since: string): boolean {
  const step = photo.processing?.find((row) => row.step === 'thumbnail')
  if (step?.state !== 'done' || step.at === undefined) {
    return false
  }
  const at = Date.parse(step.at)
  const from = Date.parse(since)
  return Number.isFinite(at) && Number.isFinite(from) && at >= from
}

/**
 * The rendition address with a cache-busting version appended, or the address
 * itself for version 0. A thumbnail URL is built from the photo's UID and served
 * as immutable for a year, so a rebuilt rendition under the same address would
 * never be fetched again; the version is what makes the browser ask.
 */
export function versionedUrl(url: string, version: number): string {
  if (version <= 0) {
    return url
  }
  return `${url}${url.includes('?') ? '&' : '?'}v=${String(version)}`
}

/**
 * The per-photo rendition versions this session has learned about, kept outside
 * React so they survive leaving the viewer and coming back: the browser's cache
 * does not know a photo's thumbnails were rebuilt, and a viewer reopened on the
 * plain address would paint the stale bytes for as long as the cache keeps them.
 *
 * The map is replaced, never mutated, so a `useSyncExternalStore` snapshot of it
 * is a value React can compare and depend on.
 */
let versions: ReadonlyMap<string, number> = new Map()
const listeners = new Set<() => void>()

/** The rendition version of `uid` this session has reached; 0 until a rebuild was seen. */
export function renditionVersion(
  uid: string,
  from: ReadonlyMap<string, number> = versions,
): number {
  return from.get(uid) ?? 0
}

/** Records that `uid`'s renditions were rebuilt once more, and returns the new version. */
export function bumpRenditionVersion(uid: string): number {
  const next = renditionVersion(uid) + 1
  const updated = new Map(versions)
  updated.set(uid, next)
  versions = updated
  for (const listener of listeners) {
    listener()
  }
  return next
}

/** Subscribes to every version bump, for `useSyncExternalStore`. */
export function subscribeRenditionVersions(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** The current versions, as the immutable snapshot `useSyncExternalStore` reads. */
export function renditionVersions(): ReadonlyMap<string, number> {
  return versions
}

/** Forgets every version — for tests, which must not leak a bump into each other. */
export function resetRenditionVersions(): void {
  versions = new Map()
}
