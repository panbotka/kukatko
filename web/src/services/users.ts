import { ApiError, type Role, type User } from './auth'

/**
 * Admin user-management client, mirroring the backend JSON shapes from
 * `internal/auth/handlers_admin.go`. It backs the `/users` administration page:
 * list, create, update, enable/disable and reset another user's password. The
 * session cookie is sent automatically (same-origin); every call throws
 * {@link ApiError} on a non-OK response so callers can branch on `status` and
 * map a 400/409 onto the offending form field.
 *
 * Password hashes never cross the wire: the backend excludes them from every
 * payload, so no type here has a place to put one.
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

/** Issues a request with an optional JSON body, throwing ApiError on non-OK. */
async function request(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<Response> {
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
  return res
}

/**
 * The admin-only view of a user (`auth.adminUserResponse`): every field of
 * `auth.User` plus the free-text administrative `note`, which the login and
 * `/auth/me` payloads withhold.
 */
export interface AdminUser extends User {
  note: string
}

/**
 * Body of `POST /admin/users` (`auth.createUserRequest`). `display_name`,
 * `email` and `note` are optional to the backend but always sent, so an omitted
 * field reads as an explicit empty value rather than a silent default.
 */
export interface CreateUserBody {
  username: string
  password: string
  display_name: string
  email: string
  role: Role
  note: string
}

/**
 * Body of `PATCH /admin/users/{uid}` (`auth.updateUserRequest`). The update
 * *replaces* the mutable profile, so every field must be sent — including the
 * ones the edit dialog does not offer (`email`, `disabled`), echoed back from
 * the row being edited.
 */
export interface UpdateUserBody {
  display_name: string
  email: string
  role: Role
  disabled: boolean
  note: string
}

/** The assignable roles, in descending order of privilege (`auth.Role`). */
export const ROLES: readonly Role[] = ['admin', 'editor', 'viewer']

/** Maximum length of a user note in characters (`auth.MaxNoteLen`). */
export const MAX_NOTE_LENGTH = 1000

/** Fetches every user, ordered by username. */
export async function fetchUsers(signal?: AbortSignal): Promise<AdminUser[]> {
  const res = await request('GET', '/admin/users', undefined, signal)
  return (await res.json()) as AdminUser[]
}

/**
 * Creates a user.
 *
 * @throws ApiError with `status` 409 (username taken) or 400 (weak password,
 *   invalid role, over-length note) so the caller can flag the offending field.
 */
export async function createUser(body: CreateUserBody, signal?: AbortSignal): Promise<AdminUser> {
  const res = await request('POST', '/admin/users', body, signal)
  return (await res.json()) as AdminUser
}

/**
 * Replaces a user's mutable profile fields.
 *
 * @throws ApiError with `status` 400 (invalid role, over-length note) or 404.
 */
export async function updateUser(
  uid: string,
  body: UpdateUserBody,
  signal?: AbortSignal,
): Promise<AdminUser> {
  const res = await request('PATCH', `/admin/users/${uid}`, body, signal)
  return (await res.json()) as AdminUser
}

/**
 * Enables or disables `user`, returning the refreshed row.
 *
 * Disabling goes through the dedicated `POST /admin/users/{uid}/disable`, which
 * needs no profile fields and therefore cannot clobber a concurrent edit.
 * Enabling has no endpoint of its own, so it is a profile update that clears the
 * flag — the remaining fields are echoed back from `user` unchanged. Both paths
 * invalidate the user's sessions on the way to `disabled`.
 */
export async function setUserDisabled(
  user: AdminUser,
  disabled: boolean,
  signal?: AbortSignal,
): Promise<AdminUser> {
  if (disabled) {
    const res = await request('POST', `/admin/users/${user.uid}/disable`, undefined, signal)
    return (await res.json()) as AdminUser
  }
  return updateUser(
    user.uid,
    {
      display_name: user.display_name,
      email: user.email,
      role: user.role,
      disabled: false,
      note: user.note,
    },
    signal,
  )
}

/**
 * Sets a new password for another user, invalidating all of their sessions.
 *
 * @throws ApiError with `status` 400 (password too short) or 404.
 */
export async function resetUserPassword(
  uid: string,
  newPassword: string,
  signal?: AbortSignal,
): Promise<void> {
  await request('POST', `/admin/users/${uid}/password`, { new_password: newPassword }, signal)
}
