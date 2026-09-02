import { ApiError } from './auth'

/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * The two facts about this instance an anonymous visitor may learn
 * (`GET /api/v1/settings/public`): whether self-service registration is open, and
 * whether this instance can run a passkey ceremony.
 *
 * It is deliberately not the full settings record — that one carries the shared
 * registration secret and is behind `RequireAdmin`. Nothing else about the
 * instance leaks from this endpoint, which is why the sign-in screen may ask it
 * before anybody has signed in.
 *
 * `passkeys_enabled` is here rather than only in `GET /capabilities` for exactly
 * that reason: capabilities are behind `RequireAuth`, and the screen that has to
 * decide whether to offer passkey sign-in is the one nobody has signed in on.
 */
export interface PublicSettings {
  registration_enabled: boolean
  passkeys_enabled: boolean
}

/**
 * Reads the public settings from `GET /api/v1/settings/public`.
 *
 * The endpoint has no guard, so this sends no credentials and works on the
 * sign-in and registration screens alike.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws Error if the response status is not 2xx, or the request never got
 *   there — the caller decides what an unanswered question means, and for
 *   registration "we could not ask" is not "the door is shut".
 */
export async function fetchPublicSettings(signal?: AbortSignal): Promise<PublicSettings> {
  const res = await fetch(`${API_BASE}/settings/public`, { signal })
  if (!res.ok) {
    throw new Error(`public settings request failed: ${res.status}`)
  }
  return (await res.json()) as PublicSettings
}

/**
 * The full settings record an administrator reads from `GET /api/v1/settings`
 * (`settingsapi.adminResponse`). It is the only response that carries
 * `registration_secret` — the secret is stored readable precisely so the
 * administrator can read it back and tell people what it is, which is why the
 * endpoint is behind `RequireAdmin`.
 */
export interface InstanceSettings {
  registration_enabled: boolean
  registration_secret: string
  welcome_markdown: string
  /** RFC 3339 timestamp of the last save; the seed time until the first one. */
  updated_at: string
  /** UID of the administrator who saved last; absent when nobody has. */
  updated_by_uid?: string
}

/**
 * The three values `PUT /api/v1/settings` replaces. All three travel together
 * because the backend writes them together: the registration flag and the
 * secret guard each other, so a partial update has no meaning.
 */
export interface InstanceSettingsUpdate {
  registration_enabled: boolean
  registration_secret: string
  welcome_markdown: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/**
 * Reads the full instance settings from `GET /api/v1/settings`.
 *
 * Admin-only server-side; the session cookie goes along, so a caller below that
 * role gets an {@link ApiError} with status 403 rather than a filtered record.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function fetchInstanceSettings(signal?: AbortSignal): Promise<InstanceSettings> {
  const res = await fetch(`${API_BASE}/settings`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as InstanceSettings
}

/**
 * Replaces all three instance settings via `PUT /api/v1/settings` and returns
 * the persisted record.
 *
 * The backend refuses to enable registration while the secret is blank (400,
 * `settings.ErrSecretRequired`). The message of the resulting {@link ApiError}
 * is the server's own wording, which the caller is expected to show — the
 * browser refuses that combination first, so reaching this is the belt to the
 * client-side braces.
 *
 * @param update the new values for all three settings.
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function updateInstanceSettings(
  update: InstanceSettingsUpdate,
  signal?: AbortSignal,
): Promise<InstanceSettings> {
  const res = await fetch(`${API_BASE}/settings`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as InstanceSettings
}

/**
 * The first-sign-in greeting as any signed-in user may read it
 * (`GET /api/v1/settings/welcome`, `settingsapi.welcomeResponse`). One field:
 * a viewer has no business reading the registration secret that travels with
 * the administrator's record.
 */
export interface WelcomeSettings {
  welcome_markdown: string
}

/**
 * Reads the administrator's first-sign-in welcome text from
 * `GET /api/v1/settings/welcome`.
 *
 * An instance where nobody wrote one answers 200 with an empty string rather
 * than a 404 — "there is no greeting" is an answer, not a failure — so a caller
 * that gets a rejection here really could not ask, and must not treat that as
 * an empty greeting.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function fetchWelcomeMarkdown(signal?: AbortSignal): Promise<string> {
  const res = await fetch(`${API_BASE}/settings/welcome`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return ((await res.json()) as WelcomeSettings).welcome_markdown
}
