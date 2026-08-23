/**
 * User roles mirrored from the backend (`internal/auth/role.go`), on a strict
 * ladder `viewer < editor < admin < maintainer` where each role inherits every
 * permission of the ones below it. viewer is read-only; editor adds write access
 * to media and metadata; admin adds governance (user management, audit, emptying
 * the trash); maintainer adds operations (imports, maintenance, system status,
 * backup, restore, jobs, processing) and is the most powerful role.
 */
export type Role = 'viewer' | 'editor' | 'admin' | 'maintainer'

/**
 * The roles that are meaningful as a route-guard threshold. `viewer` is the
 * floor of the ladder — every authenticated user already holds it — so guarding
 * on it would never deny anyone, and the 403 page would have no honest sentence
 * to print about what is missing.
 */
export type GuardRole = Exclude<Role, 'viewer'>

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
  /**
   * The person of the library this account belongs to, or null when nobody has
   * said. It is what makes "my photos", `person:me` and the account's own face
   * on a comment possible; almost every account has none, so every reader of it
   * has to work without it.
   */
  subject_uid?: string | null
  /**
   * When an administrator let this account in, or null while nobody has —
   * "registered, waiting". It is **not** the inverse of `disabled`: an account
   * that was never approved and one that was approved and later blocked are
   * different states, so a reader that shows one must show the other too.
   */
  approved_at?: string | null
  /**
   * When the account's owner last dismissed or completed the first-run welcome,
   * or null when they never have. `POST /auth/welcome-seen` stamps it once and
   * never moves it.
   */
  welcome_seen_at?: string | null
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

/**
 * Error meaning the request never reached the backend at all: a device with no
 * network, a DNS or TLS failure, a dropped connection, a server not listening.
 * `fetch` rejects with a bare `TypeError` for every one of those, which at the
 * call site is indistinguishable from a mistake in the call itself — so each
 * request in this module goes through `apiFetch`, which re-throws it as this.
 *
 * The distinction is not cosmetic: "we could not ask" is not "the answer was
 * no". Without it an unreachable backend reached the reader as a rejected
 * password on the login form and as a signed-out session in `AuthProvider`, so a
 * phone with no signal accused its owner of forgetting a password they knew.
 */
export class NetworkError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options)
    this.name = 'NetworkError'
  }
}

/**
 * True when a rejected call means "there is no such thing" (HTTP 404) rather
 * than "the request failed". A detail page uses it to tell a purged photo or a
 * deleted album apart from a broken connection: one is gone for good, the other
 * is worth retrying. It matters most for links out of the audit log, which
 * outlives what it audits.
 */
export function isNotFound(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404
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
 * Performs one call against the auth API, translating a transport-level
 * rejection into a {@link NetworkError} so callers can tell an unreachable
 * backend from one that answered.
 *
 * An abort passes through untouched: callers cancel on unmount and must keep
 * recognising `AbortError`, since their own cancellation is not an outage.
 */
async function apiFetch(path: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(`${API_BASE}${path}`, init)
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new NetworkError(
      error instanceof Error ? error.message : 'the server could not be reached',
      { cause: error },
    )
  }
}

/**
 * Authenticates with username + password. On success the backend sets the
 * HttpOnly session cookie and returns the user plus a download token.
 *
 * @throws ApiError with `status` 401 (bad credentials), 429 (rate limited),
 *   400 (malformed) or 5xx so the caller can render the matching message.
 * @throws NetworkError when the request never reached the backend, which the
 *   login form must say out loud instead of blaming the password.
 */
export async function login(
  username: string,
  password: string,
  signal?: AbortSignal,
): Promise<AuthSession> {
  const res = await apiFetch('/auth/login', {
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
  const res = await apiFetch('/auth/logout', {
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
 * @throws NetworkError when the backend could not be reached, which is *not* the
 *   same as `null`: the visitor may well still be signed in.
 */
export async function fetchMe(signal?: AbortSignal): Promise<AuthSession | null> {
  const res = await apiFetch('/auth/me', {
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
  const res = await apiFetch('/auth/password', {
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

/**
 * Says which person of the library the signed-in user is, or takes it back when
 * `subjectUid` is null (`PUT /api/v1/auth/subject`).
 *
 * It is self-service: the account written to is the session's, never one named
 * in the body. Setting it publishes that person's cover photo — when they have
 * one — next to everything this account has written, which is why the form that
 * calls this says so.
 *
 * @returns the refreshed user, so the caller can re-render without a round trip.
 * @throws ApiError with `status` 400 when the UID names nobody in the library.
 */
export async function setMySubject(subjectUid: string | null, signal?: AbortSignal): Promise<User> {
  const res = await apiFetch('/auth/subject', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ subject_uid: subjectUid }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as User
}

/** Minimum password length enforced by the backend (`internal/auth`). */
export const MIN_PASSWORD_LENGTH = 8

/**
 * A personal API token, mirroring the backend `auth.APIToken` JSON shape. It is
 * a long-lived bearer credential for non-interactive clients (the `kukatko ctl`
 * CLI, scripts, agents) that inherits its owner's role — there is no second
 * permission system behind it.
 *
 * The plaintext secret is **not** part of this record: the server keeps only a
 * SHA-256 hash and discloses the credential exactly once, in the create
 * response. `expires_at` is absent on a token that never expires, `last_used_at`
 * on one that has never authenticated a request (and it is rewritten at most
 * once a minute, so it is "roughly when", not an access log).
 */
export interface ApiToken {
  id: string
  user_uid: string
  name: string
  created_at: string
  expires_at?: string
  last_used_at?: string
  revoked_at?: string
}

/** Response body of `POST /api/v1/auth/tokens`. */
export interface CreatedApiToken {
  token: ApiToken
  /** The plaintext `kkt_…` credential — returned once and never again. */
  secret: string
}

/** Response body of `GET /api/v1/auth/tokens`. */
interface ApiTokensResponse {
  tokens: ApiToken[]
}

/**
 * Longest token name the backend accepts (`apiTokenNameMaxLen` in
 * `internal/auth`). Mirrored here so the input stops at the limit instead of
 * letting a long name travel to the server only to come back a 400.
 */
export const API_TOKEN_NAME_MAX_LENGTH = 100

/**
 * Lists the signed-in user's own API tokens, newest first. The backend scopes
 * the listing to the caller, so this never sends an owner, and it includes
 * revoked and expired tokens — filtering those is the caller's decision.
 *
 * @throws ApiError on any non-OK status.
 */
export async function fetchApiTokens(signal?: AbortSignal): Promise<ApiToken[]> {
  const res = await apiFetch('/auth/tokens', {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as ApiTokensResponse
  return body.tokens
}

/**
 * Mints a named API token for the signed-in user and returns it together with
 * its plaintext secret — the only time the secret is ever disclosed.
 *
 * @throws ApiError with `status` 400 (empty name), 429 (the creation rate limit,
 *   shared with login) or 403 (a role that may not mint tokens).
 */
export async function createApiToken(name: string, signal?: AbortSignal): Promise<CreatedApiToken> {
  const res = await apiFetch('/auth/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ name }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as CreatedApiToken
}

/**
 * Revokes one of the signed-in user's tokens. Revocation is idempotent — an
 * already-revoked token still answers 204 — and somebody else's token is
 * reported as a 404, never a 403.
 *
 * @throws ApiError with `status` 404 (no such token of the caller's) or 5xx.
 */
export async function revokeApiToken(id: string, signal?: AbortSignal): Promise<void> {
  const res = await apiFetch(`/auth/tokens/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

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
