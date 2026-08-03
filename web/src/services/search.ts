import { ApiError } from './auth'
import { type Photo } from './photos'

/**
 * Grouped global-search client, mirroring the backend JSON shape from
 * `internal/globalsearchapi` (`GET /api/v1/search/global?q=`). The endpoint runs
 * one query across several entity kinds at once — albums, labels, people
 * (subjects) and photos — capped at a small top-N per group, so it is light
 * enough to power the navbar type-ahead dropdown and the search page's
 * cross-entity sections. This is separate from the photo full-text/semantic
 * search on `/search` (`searchPhotos` in `photos.ts`), which stays the main photo
 * result set.
 */

const API_BASE = '/api/v1'

/** Standard backend error envelope shared by every API group. */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/** A single album match: enough to link to and render a compact row/card. */
export interface GlobalSearchAlbum {
  uid: string
  title: string
  /** UID of the album's cover photo, if it has one (for a thumbnail). */
  cover?: string
  photo_count: number
}

/** A single label match. */
export interface GlobalSearchLabel {
  uid: string
  name: string
  photo_count: number
}

/** A single person/subject match. */
export interface GlobalSearchPerson {
  uid: string
  name: string
  /** UID of the subject's cover photo, if it has one (for an avatar). */
  cover?: string
}

/** What a pasted UID names, as reported by the backend's direct hit. */
export type GlobalSearchUidKind =
  | 'photo'
  | 'album'
  | 'label'
  | 'person'
  | 'marker'
  | 'stack'
  | 'photoprism'

/** The entity a direct hit opens — always one of the four routable kinds. */
export type GlobalSearchTargetKind = 'photo' | 'album' | 'label' | 'person'

/** A photo state a direct hit reports, when the photo is outside the library view. */
export type GlobalSearchPhotoState = 'archived' | 'hidden' | 'private' | 'stack_member'

/**
 * The answer to a UID pasted into the search box: an exact reference, not a
 * fuzzy match. `found: false` means the id was well-formed but names nothing —
 * the UI says so rather than showing an empty result set, which reads as a
 * broken search. `kind` is what the id itself names; `target_kind`/`target_uid`
 * are what to open, which differs for the ids that stand for something else (a
 * marker → its photo, a stack → its primary, a PhotoPrism id → the catalogue
 * photo holding it).
 */
export interface GlobalSearchDirect {
  uid: string
  kind: GlobalSearchUidKind
  found: boolean
  target_kind?: GlobalSearchTargetKind
  target_uid?: string
  title?: string
  photo?: Photo
  cover?: string
  states?: GlobalSearchPhotoState[]
}

/**
 * Grouped global-search result. Every group is always present as an array
 * (possibly empty), matching the backend envelope which serialises absent groups
 * as `[]` rather than `null`. `direct` is present only for a query that names an
 * entity by its UID; the groups are then all empty, because the backend resolves
 * the id instead of running the fuzzy fan-out.
 */
export interface GlobalSearchResult {
  query: string
  direct?: GlobalSearchDirect
  albums: GlobalSearchAlbum[]
  labels: GlobalSearchLabel[]
  people: GlobalSearchPerson[]
  photos: Photo[]
}

/** The app route each routable target kind lives under. */
const TARGET_ROUTES: Record<GlobalSearchTargetKind, string> = {
  photo: '/photos',
  album: '/albums',
  label: '/labels',
  person: '/people',
}

/**
 * The route a resolved direct hit opens, or `null` when it resolved to nothing
 * (or to a kind this build does not route).
 */
export function directHitRoute(direct: GlobalSearchDirect): string | null {
  if (!direct.found || direct.target_kind === undefined || direct.target_uid === undefined) {
    return null
  }
  const base = TARGET_ROUTES[direct.target_kind] as string | undefined
  return base === undefined ? null : `${base}/${direct.target_uid}`
}

/**
 * Runs a grouped global search via `GET /api/v1/search/global?q=`. The query is
 * required by the backend (an empty/whitespace value yields a 400), so callers
 * should skip the call for an empty query and treat that as an idle state.
 *
 * @throws ApiError with `status` 400 (missing query) or 5xx so the caller can
 *   render the matching message.
 */
export async function globalSearch(q: string, signal?: AbortSignal): Promise<GlobalSearchResult> {
  const query = new URLSearchParams({ q })
  const res = await fetch(`${API_BASE}/search/global?${query.toString()}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as GlobalSearchResult
}

/**
 * Whether a grouped result carries any non-photo entity match (album, label or
 * person). The search page uses this to decide whether to render the
 * cross-entity sections above the photo grid.
 */
export function hasEntityMatches(result: GlobalSearchResult): boolean {
  return result.albums.length > 0 || result.labels.length > 0 || result.people.length > 0
}

/**
 * Whether a grouped result has no matches at all (every group empty and no
 * direct UID hit). The navbar dropdown uses this to show its "nothing found"
 * line. An unresolved direct hit still counts as "something to say" — the UI
 * reports the unknown id explicitly.
 */
export function isEmptyResult(result: GlobalSearchResult): boolean {
  return (
    result.direct === undefined &&
    result.albums.length === 0 &&
    result.labels.length === 0 &&
    result.people.length === 0 &&
    result.photos.length === 0
  )
}
