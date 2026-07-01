import { ApiError } from './auth'

/**
 * Saved-searches ("smart albums") client, mirroring the backend JSON shapes from
 * `internal/savedsearch` and `internal/savedsearchapi`. A saved search is a named,
 * owner-private snapshot of a library/search view: its filters, sort, query and
 * mode. Every operation is scoped server-side to the signed-in user (the session
 * cookie is sent automatically), so this client never sends an owner. Each call
 * throws {@link ApiError} on a non-OK response so callers can branch on `status`.
 */

const API_BASE = '/api/v1'

/**
 * Opaque view-state blob stored with a saved search. It is exactly the object the
 * app serialises into the URL via `useUrlState` (a flat map of string-valued view
 * keys — filters, sort, `q`, `mode`), stored and restored verbatim so a saved
 * search reproduces the view exactly.
 */
export type SavedSearchParams = Record<string, string>

/** A stored saved search (`savedsearch.SavedSearch`); `owner_uid` is never surfaced. */
export interface SavedSearch {
  uid: string
  name: string
  params: SavedSearchParams
  created_at: string
  updated_at: string
}

/** Fields accepted by the update endpoint; an omitted field is left unchanged. */
export interface SavedSearchUpdate {
  name?: string
  params?: SavedSearchParams
}

/** Response body of `GET /api/v1/saved-searches`. */
interface SavedSearchesResponse {
  saved_searches: SavedSearch[]
}

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

/** Issues a GET and parses the JSON body, throwing ApiError on a non-OK status. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/**
 * Issues a body-carrying request (POST/PATCH/DELETE) and parses the JSON body,
 * throwing ApiError on a non-OK status. A 204 (or otherwise empty) response
 * resolves to `undefined`, so callers expecting no content can ignore the result.
 */
async function sendJSON<T>(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  return (text === '' ? undefined : JSON.parse(text)) as T
}

/** Lists the current user's saved searches, newest first. */
export async function fetchSavedSearches(signal?: AbortSignal): Promise<SavedSearch[]> {
  const body = await getJSON<SavedSearchesResponse>('/saved-searches', signal)
  return body.saved_searches
}

/** Creates a saved search from a name and the current view params. */
export async function createSavedSearch(
  name: string,
  params: SavedSearchParams,
  signal?: AbortSignal,
): Promise<SavedSearch> {
  return sendJSON<SavedSearch>('POST', '/saved-searches', { name, params }, signal)
}

/** Updates a saved search's name and/or params and returns the refreshed record. */
export async function updateSavedSearch(
  uid: string,
  update: SavedSearchUpdate,
  signal?: AbortSignal,
): Promise<SavedSearch> {
  return sendJSON<SavedSearch>(
    'PATCH',
    `/saved-searches/${encodeURIComponent(uid)}`,
    update,
    signal,
  )
}

/** Deletes a saved search owned by the current user. */
export async function deleteSavedSearch(uid: string, signal?: AbortSignal): Promise<void> {
  await sendJSON<undefined>(
    'DELETE',
    `/saved-searches/${encodeURIComponent(uid)}`,
    undefined,
    signal,
  )
}
