/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * Instance feature flags returned by `GET /api/v1/capabilities`. The shape is
 * deliberately open so future flags (e.g. whether maps are configured) can be
 * added without a new endpoint or a new provider.
 */
export interface Capabilities {
  /**
   * Whether semantic (embedding-backed) search is currently available. It tracks
   * the reachability of the embeddings sidecar, which is frequently offline by
   * design; full-text search works regardless, so this only gates the hint that
   * advertises semantic search.
   */
  semantic_search: boolean
}

/**
 * Fetches the instance feature flags from `GET /api/v1/capabilities`. The
 * endpoint is behind auth, so the session cookie is sent with the request.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws Error if the response status is not 2xx.
 */
export async function fetchCapabilities(signal?: AbortSignal): Promise<Capabilities> {
  const res = await fetch(`${API_BASE}/capabilities`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new Error(`capabilities request failed: ${res.status}`)
  }
  return (await res.json()) as Capabilities
}
