import { ApiError, type InstanceRole, type ProjectRole } from './auth'
import { instancePath } from './api'
import type { RekeyState } from './rekey'

/**
 * The system administrator's client of the instance-level project API
 * (`internal/projectapi`): the projects of the instance, their memberships and
 * the account list with each account's instance role. Every route is
 * maintainer-only and sits outside any project, so every URL is built with
 * {@link instancePath}.
 *
 * A refusal throws {@link ProjectApiError}, which carries the backend's
 * machine-readable `code` (`slug_taken`, `invalid_slug`, `last_admin`, `self`…)
 * so a page can map it to a field message in its own words.
 */

/** The machine-readable refusal codes of `internal/projectapi`. */
export type ProjectErrorCode =
  | 'invalid_body'
  | 'not_found'
  | 'slug_taken'
  | 'invalid_slug'
  | 'slug_immutable'
  | 'invalid_name'
  | 'invalid_limit'
  | 'invalid_role'
  | 'user_not_found'
  | 'already_member'
  | 'not_member'
  | 'last_admin'
  | 'self'
  | 'last_maintainer'
  | 'invalid_email'
  | 'invalid_username'
  | 'username_taken'
  | 'email_ambiguous'
  | 'user_disabled'
  | 'password_set'
  | 'internal'

/**
 * A refusal of the project API. `message` (inherited) is the `code`, so a
 * caller that only knows {@link ApiError} still reads something meaningful;
 * `detail` is the English rule some refusals carry (`invalid_slug` does).
 */
export class ProjectApiError extends ApiError {
  readonly code: string
  readonly detail: string

  constructor(status: number, code: string, detail: string) {
    super(status, code)
    this.name = 'ProjectApiError'
    this.code = code
    this.detail = detail
  }
}

/** One project as the system administrator sees it (`GET /projects`). */
export interface AdminProject {
  uid: string
  slug: string
  name: string
  description: string
  /** False for a deactivated project: nobody but a maintainer enters it; its data stays. */
  active: boolean
  /** `""` for the storage root, or `projects/<uid>/`. */
  storage_prefix: string
  /** The largest original accepted, in bytes, or null for no limit. */
  max_file_bytes: number | null
  /** The quota of all originals together, in bytes, or null for no limit. */
  max_total_bytes: number | null
  /** Approved and waiting memberships. */
  members_count: number
  /** Every photo row of the project, archived included. */
  photos_count: number
  /** The bytes of those photos' originals. */
  originals_bytes: number
  created_at: string
  updated_at: string
  /**
   * The project's most recent storage move, or `null` when it never had one.
   * Carried by the list so it needs no rekey request per project; the full run
   * (errors, bytes, cursor) is `GET /projects/{uid}/rekey`.
   */
  rekey: AdminProjectRekey | null
}

/** What the project list shows of a storage move. */
export interface AdminProjectRekey {
  state: RekeyState
  done_photos: number
  total_photos: number
}

/**
 * The first admin of a project set up for somebody else: the address (required)
 * and, for a new account, its display name and username — derived from the
 * address when left out.
 */
export interface FirstAdminBody {
  email: string
  display_name?: string
  username?: string
}

/**
 * Body of `POST /projects`. An omitted limit means none. With `admin` the
 * project is set up for that person, who becomes its only member; without it
 * the caller becomes its admin.
 */
export interface CreateProjectBody {
  slug: string
  name: string
  description?: string
  max_file_bytes?: number
  max_total_bytes?: number
  admin?: FirstAdminBody
}

/**
 * What became of a project's first admin (or of a re-sent set-password link):
 * the account, whether it was `created` or already `existing`, whether the mail
 * went out and — while the account has no password — the one-time link, to be
 * passed on by hand when no mail was sent.
 */
export interface FirstAdmin {
  user_uid: string
  username: string
  display_name: string
  email: string
  account: 'created' | 'existing'
  mail_sent: boolean
  set_password_url?: string
  expires_at?: string
}

/** The answer to `POST /projects`: the project and, when set up for somebody, its first admin. */
export interface CreatedProject extends AdminProject {
  first_admin?: FirstAdmin
}

/** A member whose account has no password yet (`GET /projects/{uid}/awaiting-password`). */
export interface AwaitingMember {
  user_uid: string
  username: string
  display_name: string
  email: string
  role: ProjectRole
  /** When the outstanding link stops working, or null when none is usable any more. */
  link_expires_at: string | null
}

/**
 * Body of `PATCH /projects/{uid}`: a key left out keeps the value, `null`
 * clears a limit. The slug is not here — it is the URL and cannot change.
 */
export interface UpdateProjectBody {
  name?: string
  description?: string
  active?: boolean
  max_file_bytes?: number | null
  max_total_bytes?: number | null
}

/** A membership as `POST /projects/{uid}/members` returns it. */
export interface ProjectMember {
  project_uid: string
  user_uid: string
  role: ProjectRole
  approved_at: string | null
  username: string
  display_name: string
}

/** One membership of an account in the account list. */
export interface AccountMembership {
  project_uid: string
  project_slug: string
  project_name: string
  active: boolean
  role: ProjectRole
  approved: boolean
  /**
   * The person of this project the account says it is ("this is me"), or null
   * when nobody has said; `subject_name` is that person's name. The link is per
   * membership — the same account may be a different person, or nobody, in
   * another project — so it is shown per project and never edited as one value.
   */
  subject_uid: string | null
  subject_name: string
}

/**
 * One account of the instance (`GET /users`, unprefixed): the account record —
 * who it is, its instance role, whether it is blocked or still waiting for an
 * approval, the administrative note, its last sign-in — and its memberships.
 */
export interface InstanceAccount {
  uid: string
  username: string
  display_name: string
  email: string
  instance_role: InstanceRole
  disabled: boolean
  note: string
  /** When it was let in, null while it waits for an approval. */
  approved_at: string | null
  last_login_at: string | null
  memberships: AccountMembership[]
}

/** The refusal envelope of the project API. */
interface ErrorBody {
  error?: string
  message?: string
}

/** Turns a non-OK response into a {@link ProjectApiError}. */
async function toError(res: Response): Promise<ProjectApiError> {
  let code = ''
  let detail = ''
  try {
    const body = (await res.json()) as ErrorBody
    code = typeof body.error === 'string' ? body.error : ''
    detail = typeof body.message === 'string' ? body.message : ''
  } catch {
    // An empty or non-JSON body: the status text stands in for the code.
  }
  return new ProjectApiError(
    res.status,
    code !== '' ? code : res.statusText || `request failed: ${res.status}`,
    detail,
  )
}

/** Issues one request with an optional JSON body, throwing on a non-OK answer. */
async function request(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  const res = await fetch(instancePath(path), {
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

/** The project URL segment for `uid`. */
function projectRoute(uid: string): string {
  return `/projects/${encodeURIComponent(uid)}`
}

/** Every project of the instance, by name, with its usage. */
export async function fetchAdminProjects(signal?: AbortSignal): Promise<AdminProject[]> {
  const res = await request('GET', '/projects', undefined, signal)
  return (await res.json()) as AdminProject[]
}

/**
 * Creates a project; the caller becomes its approved admin — or, with
 * `body.admin`, the person named there does and the answer says how
 * (`first_admin`).
 *
 * @throws ProjectApiError `slug_taken` (409), `invalid_slug`, `invalid_name`,
 *   `invalid_limit` (422); with an admin also `invalid_email`,
 *   `invalid_username` (422), `username_taken`, `email_ambiguous`,
 *   `user_disabled` (409).
 */
export async function createProject(body: CreateProjectBody): Promise<CreatedProject> {
  const res = await request('POST', '/projects', body)
  return (await res.json()) as CreatedProject
}

/** The members of a project whose accounts have no password yet. */
export async function fetchAwaitingPassword(
  projectUid: string,
  signal?: AbortSignal,
): Promise<AwaitingMember[]> {
  const res = await request(
    'GET',
    `${projectRoute(projectUid)}/awaiting-password`,
    undefined,
    signal,
  )
  return (await res.json()) as AwaitingMember[]
}

/**
 * Issues and mails a fresh set-password link to a member who has no password
 * yet; the previous link stops working.
 *
 * @throws ProjectApiError `not_member` (404), `password_set`, `user_disabled` (409).
 */
export async function resendPasswordLink(projectUid: string, userUid: string): Promise<FirstAdmin> {
  const res = await request(
    'POST',
    `${projectRoute(projectUid)}/members/${encodeURIComponent(userUid)}/password-link`,
  )
  return (await res.json()) as FirstAdmin
}

/**
 * Edits a project and returns it as it now stands.
 *
 * @throws ProjectApiError `not_found`, `invalid_name`, `invalid_limit`.
 */
export async function updateProject(uid: string, body: UpdateProjectBody): Promise<AdminProject> {
  const res = await request('PATCH', projectRoute(uid), body)
  return (await res.json()) as AdminProject
}

/**
 * Adds an account to a project, approved at once.
 *
 * @throws ProjectApiError `already_member` (409), `user_not_found`, `invalid_role`.
 */
export async function addProjectMember(
  projectUid: string,
  userUid: string,
  role: ProjectRole,
): Promise<ProjectMember> {
  const res = await request('POST', `${projectRoute(projectUid)}/members`, {
    user_uid: userUid,
    role,
  })
  return (await res.json()) as ProjectMember
}

/** Every account of the instance, by username, with its memberships. */
export async function fetchInstanceAccounts(signal?: AbortSignal): Promise<InstanceAccount[]> {
  const res = await request('GET', '/users', undefined, signal)
  return (await res.json()) as InstanceAccount[]
}

/**
 * Promotes an account to a system administrator or demotes one.
 *
 * @throws ProjectApiError `self` (409, demoting oneself), `last_maintainer` (409).
 */
export async function setInstanceRole(uid: string, role: InstanceRole): Promise<InstanceAccount> {
  const res = await request('PATCH', `/users/${encodeURIComponent(uid)}`, { instance_role: role })
  return (await res.json()) as InstanceAccount
}

/** The slug rule of `project.ValidateSlug`, applied before a round trip. */
const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,39}$/

/**
 * Path segments a slug may not take (`project.reservedSlugs`): they name routes
 * of the application next to `/p/<slug>/`.
 */
export const RESERVED_SLUGS: readonly string[] = [
  'p',
  'api',
  'u',
  'invite',
  'system',
  'login',
  'register',
  'projects',
  'account',
  'healthz',
  'metrics',
]

/** Why a slug is refused, or null when it is valid. */
export type SlugProblem = 'empty' | 'format' | 'reserved'

/** Checks `slug` against the backend's rule; the server still has the last word. */
export function slugProblem(slug: string): SlugProblem | null {
  if (slug === '') {
    return 'empty'
  }
  if (!SLUG_PATTERN.test(slug)) {
    return 'format'
  }
  return RESERVED_SLUGS.includes(slug) ? 'reserved' : null
}
