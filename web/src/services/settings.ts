/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * The one fact about this instance an anonymous visitor may learn
 * (`GET /api/v1/settings/public`): whether self-service registration is open.
 *
 * It is deliberately not the full settings record — that one carries the shared
 * registration secret and is behind `RequireAdmin`. Nothing else about the
 * instance leaks from this endpoint, which is why the sign-in screen may ask it
 * before anybody has signed in.
 */
export interface PublicSettings {
  registration_enabled: boolean
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
