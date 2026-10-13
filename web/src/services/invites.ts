import { ApiError, type ProjectRole } from './auth'
import { apiPath, instancePath } from './api'

/**
 * Client of the invitation links (`internal/inviteapi`). Two halves:
 *
 * - a project admin's management of the current project's invites
 *   (`/api/v1/p/<slug>/invites`, built with {@link apiPath}): list, create,
 *   revoke. The code stays readable for the admin, so an invite's address can be
 *   copied again from the list at any time, not only right after creating it;
 * - the public page behind an invite (`/api/v1/invite/<code>`, built with
 *   {@link instancePath}, no project): what it grants, and accepting it signed in.
 *
 * A refusal throws {@link InviteApiError}, which carries the backend's
 * machine-readable `code` (`invalid_days`, `not_found`…). Every unusable code —
 * unknown, expired, revoked, used up — is the same 404 on the public half.
 */

/** Where an invite stands now. */
export type InviteState = 'active' | 'expired' | 'revoked' | 'used_up'

/** One invite as the admin's list shows it (`GET /invites`). */
export interface Invite {
  uid: string
  /** What accepting it grants. */
  role: ProjectRole
  expires_at: string
  /** The cap on accepts; `null` is no cap. */
  max_uses: number | null
  uses: number
  revoked_at: string | null
  created_by: string | null
  /** The creator's name; empty once the account is gone. */
  created_by_name: string
  created_at: string
  /** The readable short code. */
  code: string
  /** `mail.base_url` + `/invite/<code>`, or just the path without a base URL. */
  url: string
  state: InviteState
}

/** The body of `GET /invites`: the invites, newest first, and the validity bounds. */
export interface InviteList {
  invites: Invite[]
  default_days: number
  max_days: number
}

/** What creating an invite sends; an absent `max_uses` is no cap. */
export interface NewInvite {
  role: ProjectRole
  days?: number
  max_uses?: number
}

/** What the public page learns about a live invite (`GET /invite/<code>`). */
export interface PublicInvite {
  project_name: string
  /** What accepting it signed in grants. */
  role: ProjectRole
  /** What registering through it grants — the role, capped at editor. */
  registration_role: ProjectRole
  /** Whether the caller is signed in: accept, or register / sign in first. */
  signed_in: boolean
}

/** The answer of accepting an invite: where to go and what it changed. */
export interface AcceptedInvite {
  /** The project's slug; navigate to `/p/<slug>/`. */
  slug: string
  role: ProjectRole
  /** False when the account already held the role (or a higher one). */
  changed: boolean
}

/** A refusal of the invites API; `message` (inherited) is the `code`. */
export class InviteApiError extends ApiError {
  readonly code: string

  constructor(status: number, code: string) {
    super(status, code)
    this.name = 'InviteApiError'
    this.code = code
  }
}

/** The refusal envelope of `internal/inviteapi`. */
interface ErrorBody {
  error?: string
}

/** Turns a non-OK response into an {@link InviteApiError}. */
async function toError(res: Response): Promise<InviteApiError> {
  let code = ''
  try {
    const body = (await res.json()) as ErrorBody
    code = typeof body.error === 'string' ? body.error : ''
  } catch {
    // An empty or non-JSON body: the status text stands in for the code.
  }
  return new InviteApiError(
    res.status,
    code !== '' ? code : res.statusText || `request failed: ${res.status}`,
  )
}

/** Issues one request, throwing an {@link InviteApiError} on a non-OK answer. */
async function request(
  method: string,
  url: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw await toError(res)
  }
  return res
}

/** The URL segment of one public invite code. */
function codeRoute(code: string): string {
  return instancePath(`/invite/${encodeURIComponent(code)}`)
}

/** The current project's invites, newest first, with the create form's bounds. */
export async function fetchInvites(signal?: AbortSignal): Promise<InviteList> {
  const res = await request('GET', apiPath('/invites'), undefined, signal)
  return (await res.json()) as InviteList
}

/**
 * Creates an invite in the current project and returns it with its code and URL.
 *
 * @throws InviteApiError `invalid_role`, `invalid_days` (outside 1..max days) or
 *   `invalid_max_uses` (below 1), all 422.
 */
export async function createInvite(input: NewInvite): Promise<Invite> {
  const res = await request('POST', apiPath('/invites'), input)
  return (await res.json()) as Invite
}

/**
 * Revokes an invite for good; it stays listed as revoked.
 *
 * @throws InviteApiError `not_found` (404).
 */
export async function revokeInvite(uid: string): Promise<void> {
  await request('DELETE', apiPath(`/invites/${encodeURIComponent(uid)}`))
}

/**
 * What the live invite `code` grants, for anybody — signed in or not.
 *
 * @throws InviteApiError with status 404 for every unusable code (one answer for
 *   unknown, expired, revoked and used up), 429 when the address is throttled.
 */
export async function fetchPublicInvite(code: string, signal?: AbortSignal): Promise<PublicInvite> {
  const res = await request('GET', codeRoute(code), undefined, signal)
  return (await res.json()) as PublicInvite
}

/**
 * Accepts the invite `code` as the signed-in account: a new membership, or a
 * lower role raised. Accepting twice changes nothing.
 *
 * @throws InviteApiError 404 for an unusable code, 401 when signed out.
 */
export async function acceptInvite(code: string): Promise<AcceptedInvite> {
  const res = await request('POST', `${codeRoute(code)}/accept`)
  return (await res.json()) as AcceptedInvite
}

/**
 * The absolute address of an invite to send: the backend's `url` as it is when
 * it already names a host, otherwise resolved against this page's origin.
 */
export function inviteAddress(invite: Pick<Invite, 'url'>): string {
  return new URL(invite.url, window.location.origin).toString()
}
