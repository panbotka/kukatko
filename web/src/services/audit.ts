import { ApiError } from './auth'
import { apiPath, instancePath } from './api'

/**
 * Audit-log client, mirroring the backend JSON shapes from `internal/auditapi`
 * and `internal/audit`. It powers three read-only pages over the same records: a
 * project's `/p/<slug>/audit` administration listing of that project's trail
 * ({@link fetchAuditLog}, the project-scoped `GET /p/<slug>/audit`, which never
 * holds another project's entries), the system administrator's `/system/audit`
 * over the whole instance ({@link fetchInstanceAuditLog}, `GET /audit`, every
 * project's entries and the instance's own), and every user's own activity page
 * ({@link fetchMyActivity}, the instance `GET /audit/mine`, across every
 * project), which the server narrows to the caller. Both are
 * newest-first, filterable and offset-paginated. The session cookie is sent
 * automatically (same-origin); every call throws {@link ApiError} on a non-OK
 * response so callers can branch on `status`.
 *
 * Note the field/param naming: the query params use the endpoint's names
 * (`user`, `entity_type`, `entity_uid`) while the returned records use the
 * underlying column names (`actor_uid`, `target_type`, `target_uid`).
 */

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

/** Issues a GET of `url` and parses the JSON body, throwing ApiError on a non-OK status. */
async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
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
 * One field's before/after values in an edit's `details.changes` map (the
 * `internal/audit` ChangeSet convention). A cleared field (date, coordinate,
 * cover) serializes its side as JSON `null`. Only edit actions carry this shape,
 * and only for the fields that actually changed.
 */
export interface AuditChange {
  old: unknown
  new: unknown
}

/**
 * The `details.changes` map an edit records: field name → its old/new values.
 * Legacy rows and non-edit actions have no `changes` key, so consumers must
 * treat it as optional.
 */
export type AuditChanges = Record<string, AuditChange>

/**
 * One audit entry (`audit.Record`). The nullable columns serialize as JSON
 * `null`: `actor_uid`/`target_uid`/`ip`/`user_agent` when absent, and `details`
 * when the mutation recorded no structured payload. An edit's `details` may carry
 * a `changes` map ({@link AuditChanges}) alongside its other keys.
 */
export interface AuditRecord {
  id: number
  /** The project the entry is filed under; `null` for an instance-level entry. */
  project_uid: string | null
  /** That project's slug; `null` for an instance entry (or a project since deleted). */
  project: string | null
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
  /**
   * The display name of every account that acted in one of `entries`, by uid —
   * still-waiting members too, whom no directory lists. An account since deleted
   * is absent (its entries no longer carry an actor anyway). On a project's
   * listing an actor who is no member of the project any more is absent too and
   * listed in `former_actors` instead.
   */
  actors: Record<string, string>
  /**
   * The actors of `entries` who are no member of the project any more, on a
   * project's listing: shown as a former member, never by their live name. The
   * instance listings name everybody and send it empty.
   */
  former_actors?: string[]
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
  /** The instance listing only: one project's entries, by slug. */
  project?: string
  /** The instance listing only: just the entries filed under no project. */
  instance_only?: boolean
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
  setString('project', params.project)
  if (params.instance_only === true) {
    query.set('instance_only', 'true')
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

/**
 * Fetches a page of the current project's audit entries newest-first, filtered
 * by `params`. `project` and `instance_only` belong to the instance listing
 * ({@link fetchInstanceAuditLog}); the project's refuses both by name.
 */
export async function fetchAuditLog(
  params: AuditListParams = {},
  signal?: AbortSignal,
): Promise<AuditListResponse> {
  const query = buildAuditQuery(params)
  const suffix = query === '' ? '' : `?${query}`
  return getJSON<AuditListResponse>(apiPath(`/audit${suffix}`), signal)
}

/**
 * Fetches a page of the whole instance's trail newest-first (`GET /audit`,
 * maintainer only) — every project's entries and the instance's own — filtered
 * by `params`, `project` and `instance_only` included.
 */
export async function fetchInstanceAuditLog(
  params: AuditListParams = {},
  signal?: AbortSignal,
): Promise<AuditListResponse> {
  const query = buildAuditQuery(params)
  const suffix = query === '' ? '' : `?${query}`
  return getJSON<AuditListResponse>(instancePath(`/audit${suffix}`), signal)
}

/**
 * Query parameters for `GET /audit/mine` — the same filters as the admin listing
 * except `user`, which the endpoint takes from the session instead. It is left
 * out on purpose: the server narrows every page to the caller and answers a
 * request naming somebody else with 403, so there is nothing for a client to say
 * about whose activity it wants.
 */
export type MyActivityParams = Omit<AuditListParams, 'user' | 'project' | 'instance_only'>

/**
 * Fetches a page of the caller's own audit entries, newest-first. The narrowing
 * is the server's (see `internal/auditapi`), so this listing can never contain
 * another user's actions — nor the system's, which have no actor at all.
 */
export async function fetchMyActivity(
  params: MyActivityParams = {},
  signal?: AbortSignal,
): Promise<AuditListResponse> {
  const query = buildAuditQuery(params)
  const suffix = query === '' ? '' : `?${query}`
  return getJSON<AuditListResponse>(instancePath(`/audit/mine${suffix}`), signal)
}
