import { ApiError, type InstanceRole, type User } from './auth'
import { instancePath } from './api'

/**
 * The system administrator's account-record client, mirroring the backend JSON
 * shapes from `internal/auth/handlers_admin.go` (every route maintainer-only).
 * It backs the actions of `/system/users`: create, edit, rename, block/unblock,
 * set another account's password and issue a password-reset link. It speaks the
 * account record only — the instance role and nothing project-shaped: an
 * account's role and linked person in a project are that project's membership
 * (`/p/:slug/members`), and a new account has none. The
 * session cookie is sent automatically (same-origin); every call throws
 * {@link ApiError} on a non-OK response so callers can branch on `status` and
 * map a 400/409 onto the offending form field.
 *
 * Password hashes never cross the wire: the backend excludes them from every
 * payload, so no type here has a place to put one.
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

/** Issues a request with an optional JSON body, throwing ApiError on non-OK. */
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
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return res
}

/**
 * The admin-only view of an account (`auth.adminUserResponse`): every field of
 * `auth.User` plus the free-text administrative `note`, which the login and
 * `/auth/me` payloads withhold.
 */
export interface AdminUser extends User {
  instance_role: InstanceRole
  note: string
}

/**
 * The fields of an account a profile update echoes back: what
 * {@link setUserDisabled} needs to re-enable it without touching anything else.
 * Both an {@link AdminUser} and a `/system/users` row satisfy it.
 */
export type AccountProfile = Pick<
  AdminUser,
  'uid' | 'display_name' | 'email' | 'instance_role' | 'note'
>

/**
 * Body of `POST /admin/users` (`auth.createUserRequest`). `display_name` and
 * `note` are optional to the backend but always sent, so an omitted field reads
 * as an explicit empty value rather than a silent default. `email` is not
 * optional: every account receives mail, so the backend refuses a missing or
 * malformed address with a 400 (`auth.ErrInvalidEmail`). The account is
 * created with no membership: it joins a project when that project's admin adds
 * it on the members page, or by an invitation.
 */
export interface CreateUserBody {
  username: string
  password: string
  display_name: string
  email: string
  /** `user`, or `maintainer` for a new system administrator. */
  instance_role: InstanceRole
  note: string
}

/**
 * Body of `PATCH /admin/users/{uid}` (`auth.updateUserRequest`). The update
 * *replaces* the mutable profile, so every field must be sent — including the
 * ones the edit dialog does not offer (`disabled`, `instance_role`), echoed
 * back from the row being edited. It never touches a membership. `email` must still be a valid address: an update may not leave
 * an account without one.
 */
export interface UpdateUserBody {
  display_name: string
  email: string
  instance_role: InstanceRole
  disabled: boolean
  note: string
}

/** Maximum length of a username in characters (`auth.MaxUsernameLen`). */
export const MAX_USERNAME_LENGTH = 64

/** Maximum length of a user note in characters (`auth.MaxNoteLen`). */
export const MAX_NOTE_LENGTH = 1000

/**
 * Creates a user.
 *
 * @throws ApiError with `status` 409 (username taken) or 400 (weak password,
 *   missing or malformed e-mail address, invalid instance role, over-length
 *   note) so the
 *   caller can flag the offending field.
 */
export async function createUser(body: CreateUserBody, signal?: AbortSignal): Promise<AdminUser> {
  const res = await request('POST', '/admin/users', body, signal)
  return (await res.json()) as AdminUser
}

/**
 * Replaces a user's mutable profile fields.
 *
 * @throws ApiError with `status` 400 (invalid instance role, missing or malformed e-mail
 *   address, over-length note), 404, or
 *   409 when the change would demote or disable the instance's last enabled
 *   maintainer — a state with no way back through the API, so the backend
 *   refuses it (`auth.ErrLastMaintainer`).
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
 * Gives an account a new username (`PUT /admin/users/{uid}/username`),
 * returning the refreshed row. The backend normalises the name exactly as on
 * creation (trimmed, lower-cased), keeps the account's sessions, tokens and
 * passkeys — they are keyed on the uid — and mails the person the new name when
 * mail is on. Sending the name the account already has is a no-op 200.
 *
 * @throws ApiError with `status` 400 (empty or over-length username), 403 (the
 *   account is a maintainer's and the actor is not), 404, or 409 (another
 *   account holds the name, in any letter case).
 */
export async function renameUser(
  uid: string,
  username: string,
  signal?: AbortSignal,
): Promise<AdminUser> {
  const res = await request('PUT', `/admin/users/${uid}/username`, { username }, signal)
  return (await res.json()) as AdminUser
}

/**
 * Blocks or unblocks the account `user`, returning the refreshed record.
 *
 * Disabling goes through the dedicated `POST /admin/users/{uid}/disable`, which
 * needs no profile fields and therefore cannot clobber a concurrent edit.
 * Enabling has no endpoint of its own, so it is a profile update that clears the
 * flag — the remaining fields are echoed back from `user` unchanged. Both paths
 * invalidate the user's sessions on the way to `disabled`.
 *
 * @throws ApiError with `status` 409 when disabling would take away the last
 *   enabled maintainer, leaving the instance's operations surfaces unreachable.
 */
export async function setUserDisabled(
  user: AccountProfile,
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
      instance_role: user.instance_role,
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

/**
 * How long an issued reset link stays usable, in days (`auth.PasswordResetTTL`).
 * The response carries the exact instant; this is what the UI *says*, and the
 * two must agree.
 */
export const PASSWORD_RESET_TTL_DAYS = 7

/**
 * What an administrator gets back for a reset they started
 * (`auth.IssuedPasswordReset`): the whole one-time link, so it can also be
 * passed on by hand, plus the address it was mailed to and when it dies.
 */
export interface IssuedPasswordReset {
  reset_url: string
  expires_at: string
  email: string
}

/**
 * Starts a password reset for another user
 * (`POST /admin/users/{uid}/password-reset`): the backend mints a one-time link
 * valid for {@link PASSWORD_RESET_TTL_DAYS} days, invalidates the account's
 * earlier unused ones, mails the new one to the account's address and answers
 * with it.
 *
 * Unlike {@link resetUserPassword} it sets no password: the person chooses their
 * own behind the link, and the administrator never learns it.
 *
 * @throws ApiError with `status` 403 (setting a maintainer's password is
 *   maintainer-only, and the link ultimately does exactly that), 404, or 409 for
 *   a blocked account, which could never use the link.
 */
export async function issuePasswordReset(
  uid: string,
  signal?: AbortSignal,
): Promise<IssuedPasswordReset> {
  const res = await request('POST', `/admin/users/${uid}/password-reset`, undefined, signal)
  return (await res.json()) as IssuedPasswordReset
}
