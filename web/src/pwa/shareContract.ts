/**
 * The contract between the service worker and the app for one share arriving
 * from the phone's share sheet (Android/Chromium Web Share Target).
 *
 * The flow, end to end:
 *
 *  1. The manifest declares `share_target` with `action: /share-target`,
 *     `method: POST`, `multipart/form-data` and a `files` field, so the
 *     installed app appears in the system share sheet for images and videos.
 *  2. The system POSTs the files to `/share-target`. `build/service-worker.js`
 *     intercepts that POST — it is the only non-GET the worker ever answers —
 *     writes each file into its own {@link SHARE_CACHE} entry and redirects
 *     (303) to `GET /share-target?share=<id>`. The POST body itself is consumed
 *     and never cached.
 *  3. That GET is an ordinary navigation, so it resolves to the app shell and
 *     `ShareTargetPage` renders: an editor is forwarded to `/upload?share=<id>`,
 *     a viewer is told their account cannot upload, and an unauthenticated
 *     visitor goes through login first — the files sit in the cache untouched
 *     until they come back, which is what makes them survive the round trip.
 *  4. `UploadPage` collects them (`shareTarget.ts`), stages them in the ordinary
 *     upload queue, and deletes the cache entries.
 *
 * This module holds the names and paths both halves have to agree on. It is
 * deliberately free of DOM types: the worker is plain, un-bundled JavaScript and
 * cannot import it, so `build/pwa.test.ts` — which runs the *real* worker
 * against these constants — imports it instead, and that test project is
 * typechecked without the DOM lib. The browser-side half lives in
 * `shareTarget.ts`.
 */

/**
 * The manifest's `share_target.action`, and also where the worker sends the
 * browser afterwards: one path, POST to hand the files over and GET to pick them
 * up. Sharing the path is what makes the no-worker case behave — a POST that
 * reaches the server (worker not yet installed, or unregistered) is answered by
 * the SPA fallback with the very page that explains the files did not come
 * through and offers the picker.
 */
export const SHARE_TARGET_PATH = '/share-target'

/** The manifest's `share_target.params.files[].name`: the form field to read. */
export const SHARE_FILES_FIELD = 'files'

/** Query parameter carrying the id of a staged share, on both hops. */
export const SHARE_PARAM = 'share'

/**
 * Cache holding the staged files. Kept apart from the shell cache on purpose:
 * `activate` prunes every cache whose name starts with `kukatko-shell-`, and a
 * deployment landing between the share and its collection must not eat the
 * user's photos.
 */
export const SHARE_CACHE = 'kukatko-share'

/**
 * Path prefix of one staged file. The full key is
 * `<prefix><share id>/<index>`, the index being the file's position in the
 * share, so the queue is built in the order the user picked them.
 *
 * The prefix is not a route: nothing ever requests it over the network, the
 * entries are read straight out of the Cache Storage by the page.
 */
export const SHARE_ENTRY_PREFIX = '/__kukatko-share__/'

/** Header carrying the original file name, `encodeURIComponent`-escaped. */
export const SHARE_NAME_HEADER = 'x-kukatko-share-name'

/** Header carrying the file's `lastModified` timestamp, in milliseconds. */
export const SHARE_MODIFIED_HEADER = 'x-kukatko-share-modified'

/**
 * How long a staged share is kept before it is treated as abandoned. A share
 * whose user never made it to the upload page (they closed the app, or logged in
 * as somebody who may not upload) would otherwise sit in the cache forever;
 * collecting any share sweeps the expired ones out. A day is generous enough to
 * survive "I'll do it in the morning" and short enough to not hoard photos.
 */
export const SHARE_TTL_MS = 24 * 60 * 60 * 1000

/** The cache key of one file of a share. */
export function shareEntryPath(id: string, index: number): string {
  return `${SHARE_ENTRY_PREFIX}${id}/${String(index)}`
}

/** One staged entry, as identified by its cache key. */
export interface ShareEntry {
  /** The share the file belongs to. */
  id: string
  /** The file's position within that share. */
  index: number
}

/**
 * Parses a cache key back into the share it belongs to, or null when the key is
 * not a share entry at all (or is malformed, which a foreign writer could make
 * it — the cache is shared per origin, so nothing here may assume its own shape).
 */
export function parseShareEntry(pathname: string): ShareEntry | null {
  if (!pathname.startsWith(SHARE_ENTRY_PREFIX)) {
    return null
  }
  const rest = pathname.slice(SHARE_ENTRY_PREFIX.length)
  const slash = rest.lastIndexOf('/')
  if (slash <= 0) {
    return null
  }
  const index = Number(rest.slice(slash + 1))
  if (!Number.isInteger(index) || index < 0) {
    return null
  }
  return { id: rest.slice(0, slash), index }
}

/**
 * The moment a share id was minted, in epoch milliseconds, or null for an id
 * that does not carry one.
 *
 * The worker mints ids as `<Date.now()>-<sequence>` (it cannot import this
 * module, so the format is stated here and parsed here). Encoding the time in
 * the id means expiry needs no bookkeeping of its own: the key alone says how
 * old the entry is.
 */
export function shareIdStamp(id: string): number | null {
  const dash = id.indexOf('-')
  const head = dash === -1 ? id : id.slice(0, dash)
  if (head === '' || !/^\d+$/.test(head)) {
    return null
  }
  return Number(head)
}

/** Reports whether a share id was minted longer than {@link SHARE_TTL_MS} ago. */
export function isShareExpired(id: string, now: number): boolean {
  const stamp = shareIdStamp(id)
  if (stamp === null) {
    // An id of an unknown shape has no age to judge; leave it alone rather than
    // delete somebody else's data.
    return false
  }
  return now - stamp > SHARE_TTL_MS
}
