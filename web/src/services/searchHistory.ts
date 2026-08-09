import { ApiError } from './auth'

/**
 * Search-history client, mirroring the backend JSON shapes from
 * `internal/searchhistory` and `internal/searchhistoryapi`. The history is the
 * short, ordered list of what the signed-in user actually searched for; it lives
 * on the server rather than in the browser precisely so a query composed on a
 * laptop is offered on the phone.
 *
 * Every operation is scoped server-side to the signed-in user (the session cookie
 * is sent automatically), so this client never sends an owner and there is nothing
 * to address: one GET, one POST, one DELETE, all about the caller's own history.
 * Each call throws {@link ApiError} on a non-OK response so callers can branch on
 * `status`.
 */

const API_BASE = '/api/v1'

/** How many entries the backend keeps (`searchhistory.MaxEntries`). */
export const SEARCH_HISTORY_LIMIT = 20

/** One remembered search (`searchhistory.Entry`). */
export interface SearchHistoryEntry {
  /** The query text, verbatim — exactly what to put back into the search box. */
  query: string
  /** RFC3339 timestamp of when the query was most recently run. */
  searched_at: string
}

/** Response body of `GET /api/v1/search-history`. */
interface SearchHistoryResponse {
  searches: SearchHistoryEntry[]
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

/**
 * Lists the current user's recent searches, most recent first, at most
 * {@link SEARCH_HISTORY_LIMIT} of them.
 */
export async function fetchSearchHistory(signal?: AbortSignal): Promise<SearchHistoryEntry[]> {
  const res = await fetch(`${API_BASE}/search-history`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as SearchHistoryResponse
  return body.searches
}

/**
 * Remembers a query the user just ran, moving it to the front of their history.
 * Recording the same query again is not an error — it re-orders rather than
 * duplicating — and the response carries no body.
 */
export async function recordSearch(query: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/search-history`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Forgets the current user's whole search history. Idempotent. */
export async function clearSearchHistory(signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/search-history`, {
    method: 'DELETE',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}
