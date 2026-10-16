import { ApiError, type ProjectRole } from './auth'
import { apiPath } from './api'

/**
 * A project admin's client of the project's members (`internal/memberapi`,
 * `/api/v1/p/<slug>/members`): who belongs to the current project, with what
 * role, whether a membership still waits for approval, and the changes an
 * admin makes — role, approve, remove, add an existing account and withdraw
 * such an add.
 *
 * Every route is project-scoped and admin-only, so every URL is built with
 * {@link apiPath}. A refusal throws {@link MemberApiError}, which carries the
 * backend's machine-readable `code` (`last_admin`, `self`, `not_member`…) so the
 * page can say it in its own words.
 */

/** The roles a membership can hold, lowest first (`project.Role`). */
export const MEMBER_ROLES: readonly ProjectRole[] = ['viewer', 'curator', 'editor', 'admin']

/** One member of the project (`GET /members`). */
export interface ProjectMemberRow {
  user_uid: string
  username: string
  display_name: string
  role: ProjectRole
  /** When an admin let the membership in; `null` while it waits for approval. */
  approved_at: string | null
  /**
   * The member's last visit to this project; `null` for never. Never the
   * account's last sign-in, which would tell the project when the account was
   * active in other projects.
   */
  last_seen_at: string | null
  /** True when `GET /users/{uid}/avatar` has a picture to answer with. */
  has_picture: boolean
}

/** A refusal of the members API; `message` (inherited) is the `code`. */
export class MemberApiError extends ApiError {
  readonly code: string

  constructor(status: number, code: string) {
    super(status, code)
    this.name = 'MemberApiError'
    this.code = code
  }
}

/** The refusal envelope of `internal/memberapi`. */
interface ErrorBody {
  error?: string
}

/** Turns a non-OK response into a {@link MemberApiError}. */
async function toError(res: Response): Promise<MemberApiError> {
  let code = ''
  try {
    const body = (await res.json()) as ErrorBody
    code = typeof body.error === 'string' ? body.error : ''
  } catch {
    // An empty or non-JSON body: the status text stands in for the code.
  }
  return new MemberApiError(
    res.status,
    code !== '' ? code : res.statusText || `request failed: ${res.status}`,
  )
}

/** Issues one request to the members API without judging the answer. */
function send(method: string, path: string, body?: unknown, signal?: AbortSignal) {
  return fetch(apiPath(`/members${path}`), {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
}

/** Issues one request, throwing a {@link MemberApiError} on a non-OK answer. */
async function request(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  const res = await send(method, path, body, signal)
  if (!res.ok) {
    throw await toError(res)
  }
  return res
}

/** The URL segment of one member. */
function memberRoute(userUid: string): string {
  return `/${encodeURIComponent(userUid)}`
}

/**
 * Every member of the current project, waiting ones included. An account added
 * by its exact username or e-mail is not among them until it opens the project
 * itself: the list must not tell an add that found somebody from one that did
 * not.
 */
export async function fetchMembers(signal?: AbortSignal): Promise<ProjectMemberRow[]> {
  const res = await request('GET', '', undefined, signal)
  return (await res.json()) as ProjectMemberRow[]
}

/**
 * Changes a member's role and returns the membership as it now stands.
 *
 * @throws MemberApiError `self` (409, one's own role), `last_admin` (409,
 *   demoting the only admin), `not_member` (404), `invalid_role` (422).
 */
export async function setMemberRole(userUid: string, role: ProjectRole): Promise<ProjectMemberRow> {
  const res = await request('PATCH', memberRoute(userUid), { role })
  return (await res.json()) as ProjectMemberRow
}

/**
 * Lets a waiting membership in; the backend mails the person the project's
 * link. Approving an approved membership is a no-op.
 *
 * @throws MemberApiError `not_member` (404).
 */
export async function approveMember(userUid: string): Promise<ProjectMemberRow> {
  const res = await request('POST', `${memberRoute(userUid)}/approve`)
  return (await res.json()) as ProjectMemberRow
}

/**
 * Takes a member out of the project. The account itself stays.
 *
 * @throws MemberApiError `self` (409, the only admin removing themselves),
 *   `last_admin` (409), `not_member` (404).
 */
export async function removeMember(userUid: string): Promise<void> {
  await request('DELETE', memberRoute(userUid))
}

/**
 * Adds an existing account, named by its **exact** username or e-mail address,
 * with `role` (`POST /members`).
 *
 * The backend answers 202 whether such an account exists or not, so nobody can
 * probe for accounts here — and this function keeps it that way: it resolves for
 * **any** answer and returns nothing, so no caller can tell a hit from a miss.
 * Only a request that never got an answer (no network) rejects.
 */
export async function addMember(identifier: string, role: ProjectRole): Promise<void> {
  await send('POST', '', { identifier, role })
}

/**
 * Undoes {@link addMember} (`POST /members/withdraw`): takes the project away
 * from the account named by its **exact** username or e-mail address, as long
 * as that account has not opened the project yet — which is also why the list
 * cannot show it. A member the list already shows is never touched here.
 *
 * Blind like the add: it resolves for **any** answer and returns nothing; only
 * a request that never got an answer (no network) rejects.
 */
export async function withdrawMember(identifier: string): Promise<void> {
  await send('POST', '/withdraw', { identifier })
}
