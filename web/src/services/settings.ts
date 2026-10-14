import { ApiError } from './auth'
import { apiPath, instancePath } from './api'

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
 * The two facts about this instance an anonymous visitor may learn
 * (`GET /api/v1/settings/public`): whether this instance can run a passkey
 * ceremony, and whether it sends any mail at all.
 *
 * Whether self-service registration is open is not here: that is a project's
 * decision, read from {@link fetchProjectPublicSettings}. Nothing else about the
 * instance leaks from this endpoint, which is why the sign-in screen may ask it
 * before anybody has signed in.
 *
 * `passkeys_enabled` is here rather than only in `GET /capabilities` for exactly
 * that reason: capabilities are behind `RequireAuth`, and the screen that has to
 * decide whether to offer passkey sign-in is the one nobody has signed in on.
 * `mail_enabled` is here for the same reason — the screens that would promise an
 * e-mail (registration succeeded, this account is waiting for approval) are
 * screens nobody is signed in on.
 */
export interface PublicSettings {
  passkeys_enabled: boolean
  /**
   * Whether this instance really sends transactional mail. It is `mail.enabled`
   * in the server's configuration: with it off the mailer is the no-op sender,
   * so anything the UI promised to send would silently go nowhere.
   */
  mail_enabled: boolean
}

/**
 * Reads the instance's public settings from `GET /api/v1/settings/public`.
 *
 * The endpoint has no guard, so this sends no credentials and works on the
 * sign-in and registration screens alike.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws Error if the response status is not 2xx, or the request never got
 *   there — the caller decides what an unanswered question means.
 */
export async function fetchPublicSettings(signal?: AbortSignal): Promise<PublicSettings> {
  const res = await fetch(instancePath('/settings/public'), { signal })
  if (!res.ok) {
    throw new Error(`public settings request failed: ${res.status}`)
  }
  return (await res.json()) as PublicSettings
}

/**
 * What anybody may learn about one project before signing in
 * (`GET /api/v1/p/<slug>/settings/public`): its name, which its registration
 * page greets with, and whether it takes self-service registrations. The
 * server answers only while registration is open (so `registration_enabled` is
 * `true` in practice); a closed project is the same 404 as a slug nobody has,
 * so the endpoint cannot be used to list projects. Never the secret, never the
 * welcome text.
 */
export interface ProjectPublicSettings {
  project_name: string
  registration_enabled: boolean
}

/**
 * Reads a project's public settings from
 * `GET /api/v1/p/<slug>/settings/public`.
 *
 * It names the project explicitly rather than through {@link apiPath}: the
 * pages that ask it — the project's registration page, the sign-in screen —
 * are anonymous and have no current project. The endpoint has no guard, so no
 * credentials travel.
 *
 * @param slug the project's URL slug.
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx — a 404 for a slug that
 *   names no project, a deactivated one, or one whose registration is closed
 *   (the server does not tell the three apart); a 429 when this address asked
 *   too often.
 */
export async function fetchProjectPublicSettings(
  slug: string,
  signal?: AbortSignal,
): Promise<ProjectPublicSettings> {
  const res = await fetch(instancePath(`/p/${encodeURIComponent(slug)}/settings/public`), {
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as ProjectPublicSettings
}

/**
 * The full settings record a project's administrator reads from
 * `GET /api/v1/p/<slug>/settings` (`settingsapi.adminResponse`). It is the only
 * response that carries `registration_secret` — the secret is stored readable
 * precisely so the administrator can read it back and tell people what it is,
 * which is why the endpoint is behind `RequireAdmin`.
 */
export interface ProjectSettings {
  registration_enabled: boolean
  registration_secret: string
  welcome_markdown: string
  /** RFC 3339 timestamp of the last save; the seed time until the first one. */
  updated_at: string
  /** UID of the administrator who saved last; absent when nobody has. */
  updated_by_uid?: string
}

/**
 * The three values `PUT /api/v1/p/<slug>/settings` replaces. All three travel
 * together because the backend writes them together: the registration flag and
 * the secret guard each other, so a partial update has no meaning.
 */
export interface ProjectSettingsUpdate {
  registration_enabled: boolean
  registration_secret: string
  welcome_markdown: string
}

/**
 * Reads the current project's full settings from
 * `GET /api/v1/p/<slug>/settings`.
 *
 * Admin-only server-side; the session cookie goes along, so a caller below that
 * role gets an {@link ApiError} with status 403 rather than a filtered record.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function fetchProjectSettings(signal?: AbortSignal): Promise<ProjectSettings> {
  const res = await fetch(apiPath('/settings'), {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as ProjectSettings
}

/**
 * Replaces all three of the current project's settings via
 * `PUT /api/v1/p/<slug>/settings` and returns the persisted record.
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
export async function updateProjectSettings(
  update: ProjectSettingsUpdate,
  signal?: AbortSignal,
): Promise<ProjectSettings> {
  const res = await fetch(apiPath('/settings'), {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as ProjectSettings
}

/**
 * The current project's first-sign-in greeting as any member may read it
 * (`GET /api/v1/p/<slug>/settings/welcome`, `settingsapi.welcomeResponse`). One field:
 * a viewer has no business reading the registration secret that travels with
 * the administrator's record.
 */
export interface WelcomeSettings {
  welcome_markdown: string
}

/**
 * Reads the current project's first-sign-in welcome text from
 * `GET /api/v1/p/<slug>/settings/welcome`.
 *
 * A project where nobody wrote one answers 200 with an empty string rather
 * than a 404 — "there is no greeting" is an answer, not a failure — so a caller
 * that gets a rejection here really could not ask, and must not treat that as
 * an empty greeting.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function fetchWelcomeMarkdown(signal?: AbortSignal): Promise<string> {
  const res = await fetch(apiPath('/settings/welcome'), {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return ((await res.json()) as WelcomeSettings).welcome_markdown
}

/**
 * How much of the instance the current project occupies, next to the two
 * limits the system administrator set on it (`GET /api/v1/p/<slug>/settings/usage`,
 * admin-only). A limit is `null` when there is none. The project's admin only
 * reads these: limits are set on the system administrator's projects page.
 */
export interface ProjectUsage {
  photos: number
  originals_bytes: number
  max_file_bytes: number | null
  max_total_bytes: number | null
}

/**
 * Reads the current project's usage and limits from
 * `GET /api/v1/p/<slug>/settings/usage`.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws ApiError when the response status is not 2xx.
 */
export async function fetchProjectUsage(signal?: AbortSignal): Promise<ProjectUsage> {
  const res = await fetch(apiPath('/settings/usage'), {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as ProjectUsage
}
