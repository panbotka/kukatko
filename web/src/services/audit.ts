import { ApiError } from './auth'

/**
 * Admin audit-log client, mirroring the backend JSON shapes from
 * `internal/auditapi` and `internal/audit`. It powers the read-only `/audit`
 * administration page: a newest-first, filterable, offset-paginated listing of
 * the durable audit trail. The session cookie is sent automatically
 * (same-origin); every call throws {@link ApiError} on a non-OK response so
 * callers can branch on `status`.
 *
 * Note the field/param naming: the query params use the endpoint's names
 * (`user`, `entity_type`, `entity_uid`) while the returned records use the
 * underlying column names (`actor_uid`, `target_type`, `target_uid`).
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
 * One audit entry (`audit.Record`). The nullable columns serialize as JSON
 * `null`: `actor_uid`/`target_uid`/`ip`/`user_agent` when absent, and `details`
 * when the mutation recorded no structured payload.
 */
export interface AuditRecord {
  id: number
  actor_uid: string | null
  action: string
  target_type: string
  target_uid: string | null
  details: Record<string, unknown> | null
  ip: string | null
  user_agent: string | null
  created_at: string
}

/**
 * Body of `GET /audit` (`auditapi.listResponse`). `next_offset` is the offset to
 * request for the following page, or `null` on the last page — the same
 * pagination convention as the photo list.
 */
export interface AuditListResponse {
  entries: AuditRecord[]
  total: number
  limit: number
  offset: number
  next_offset: number | null
}

/**
 * Query parameters for `GET /audit`. String filters are omitted from the request
 * when empty; `since`/`until` must already be RFC 3339 timestamps (the backend
 * rejects anything else with 400). `limit` is capped at 500 server-side.
 */
export interface AuditListParams {
  user?: string
  action?: string
  entity_type?: string
  entity_uid?: string
  /** Restricts to the review game's decisions (audit rows tagged via=review). */
  via?: 'review'
  /** Restricts to the Ano (`yes`) or Ne (`no`) review-decision bucket. */
  decision?: 'yes' | 'no'
  since?: string
  until?: string
  limit?: number
  offset?: number
}

/**
 * Serializes audit filter params into a query string, dropping empty string
 * filters and a zero/absent offset so the URL stays minimal.
 */
function buildAuditQuery(params: AuditListParams): string {
  const query = new URLSearchParams()
  const setString = (key: string, value: string | undefined) => {
    if (value !== undefined && value !== '') {
      query.set(key, value)
    }
  }
  setString('user', params.user)
  setString('action', params.action)
  setString('entity_type', params.entity_type)
  setString('entity_uid', params.entity_uid)
  setString('via', params.via)
  setString('decision', params.decision)
  setString('since', params.since)
  setString('until', params.until)
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  if (params.offset !== undefined && params.offset > 0) {
    query.set('offset', String(params.offset))
  }
  return query.toString()
}

/** Fetches a page of audit entries newest-first, filtered by `params`. */
export async function fetchAuditLog(
  params: AuditListParams = {},
  signal?: AbortSignal,
): Promise<AuditListResponse> {
  const query = buildAuditQuery(params)
  const suffix = query === '' ? '' : `?${query}`
  return getJSON<AuditListResponse>(`/audit${suffix}`, signal)
}
