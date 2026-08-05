/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * The running binary's build metadata (`version.Info`), as the backend reports
 * it. A build without linker stamps carries the placeholders `dev` / `none`.
 */
export interface VersionInfo {
  version: string
  commit: string
}

/**
 * What this instance is, as returned by `GET /api/v1/capabilities`: its optional
 * feature flags plus the build the server runs. The shape is deliberately open
 * so future flags (e.g. whether maps are configured) can be added without a new
 * endpoint or a new provider.
 */
export interface Capabilities {
  /**
   * Whether semantic (embedding-backed) search is currently available. It tracks
   * the reachability of the embeddings sidecar, which is frequently offline by
   * design; full-text search works regardless, so this only gates the hint that
   * advertises semantic search.
   */
  semantic_search: boolean
  /**
   * The build the server runs, so the UI can print it without a second source of
   * truth (a version compiled into this bundle would drift from the binary that
   * embeds it). Optional because it is absent before the first response — and
   * after a failed one — in which case the UI simply shows no version.
   */
  version?: VersionInfo
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
