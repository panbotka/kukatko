/**
 * User roles mirrored from the backend (`internal/auth/role.go`), on a strict
 * ladder `viewer < editor < admin < maintainer` where each role inherits every
 * permission of the ones below it. viewer is read-only; editor adds write access
 * to media and metadata; admin adds governance (user management, audit, emptying
 * the trash); maintainer adds operations (imports, maintenance, system status,
 * backup, restore, jobs, processing) and is the most powerful role.
 */
export type Role = 'viewer' | 'editor' | 'admin' | 'maintainer'

/** Authenticated user, mirroring the backend `auth.User` JSON shape. */
export interface User {
  uid: string
  username: string
  display_name: string
  email: string
  role: Role
  disabled: boolean
  created_at: string
  updated_at: string
  last_login_at?: string
}

/** Successful auth response body (`POST /auth/login`, `GET /auth/me`). */
export interface AuthSession {
  user: User
  download_token: string
}

/**
 * Error carrying the HTTP status of a failed API call so callers can map
 * specific statuses (401, 429, …) to user-facing, translated messages.
 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/auth/http.go`). */
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
 * Authenticates with username + password. On success the backend sets the
 * HttpOnly session cookie and returns the user plus a download token.
 *
 * @throws ApiError with `status` 401 (bad credentials), 429 (rate limited),
 *   400 (malformed) or 5xx so the caller can render the matching message.
 */
export async function login(
  username: string,
  password: string,
  signal?: AbortSignal,
): Promise<AuthSession> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ username, password }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as AuthSession
}

/** Ends the current session. Idempotent: the backend always returns 204. */
export async function logout(signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/logout`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/**
 * Loads the current session from `GET /auth/me`.
 *
 * @returns the session, or `null` when no valid session exists (HTTP 401).
 * @throws ApiError on any other non-OK status.
 */
export async function fetchMe(signal?: AbortSignal): Promise<AuthSession | null> {
  const res = await fetch(`${API_BASE}/auth/me`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (res.status === 401) {
    return null
  }
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as AuthSession
}

/**
 * Changes the current user's password. The backend revokes all other sessions
 * on success.
 *
 * @throws ApiError with `status` 401 (wrong current password) or 400 (new
 *   password too short / malformed).
 */
export async function changePassword(
  currentPassword: string,
  newPassword: string,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Minimum password length enforced by the backend (`internal/auth`). */
export const MIN_PASSWORD_LENGTH = 8

/**
 * Relative rank of each role on the strict privilege ladder; higher means more
 * privileges. Every capability below is expressed as a threshold on this rank,
 * so the ladder is the single source of truth for "at least this role".
 */
const ROLE_RANK: Record<Role, number> = {
  viewer: 0,
  editor: 1,
  admin: 2,
  maintainer: 3,
}

/** Reports whether `role` meets or exceeds the `required` role. */
export function roleAtLeast(role: Role, required: Role): boolean {
  return ROLE_RANK[role] >= ROLE_RANK[required]
}

/** Reports whether a role may perform write actions (editor and above). */
export function canWrite(role: Role): boolean {
  return roleAtLeast(role, 'editor')
}

/**
 * Reports whether a role holds the governance privileges — user management, the
 * audit log, emptying/purging the trash. This is admin-or-higher: a maintainer
 * inherits every admin power, so it qualifies too. Mirrors backend `Role.IsAdmin`.
 */
export function isAdmin(role: Role): boolean {
  return roleAtLeast(role, 'admin')
}

/**
 * Reports whether a role holds the operations privileges at the top of the
 * ladder: imports, maintenance, system status, backup, restore, jobs and
 * processing backfills. Only a maintainer qualifies. Mirrors backend
 * `Role.CanMaintain`.
 */
export function isMaintainer(role: Role): boolean {
  return roleAtLeast(role, 'maintainer')
}

/**
 * Reports whether a role may trigger imports/migrations. Import is an operations
 * capability, so only a maintainer qualifies. Mirrors backend `Role.CanImport`
 * and guards the `/import` route and nav entry.
 */
export function canImport(role: Role): boolean {
  return isMaintainer(role)
}
