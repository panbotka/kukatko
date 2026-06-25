/** Build/version metadata reported by the backend, mirroring `version.Info`. */
export interface VersionInfo {
  version: string
  commit: string
}

/** Response body of the backend `GET /healthz` endpoint. */
export interface HealthResponse {
  status: string
  version: VersionInfo
}

/**
 * Fetches the backend health status from `GET /healthz`.
 *
 * @param signal optional AbortSignal to cancel the request (e.g. on unmount).
 * @throws Error if the response status is not 2xx.
 */
export async function fetchHealth(signal?: AbortSignal): Promise<HealthResponse> {
  const res = await fetch('/healthz', { signal })
  if (!res.ok) {
    throw new Error(`health request failed: ${res.status}`)
  }
  return (await res.json()) as HealthResponse
}
