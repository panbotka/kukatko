import { ApiError } from './auth'

/**
 * Admin library-maintenance client, mirroring the backend JSON shapes from
 * `internal/maintenanceapi` and `internal/maintenance`. It drives the maintenance
 * admin UI: running an integrity scan and triggering the opt-in repairs. The
 * session cookie is sent automatically (same-origin); every call throws
 * {@link ApiError} on a non-OK response so callers can branch on `status`.
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

/** One class of integrity problem: a total count and a bounded sample of ids. */
export interface Finding {
  count: number
  samples: string[]
}

/** Result of an integrity scan (`maintenance.Report`). */
export interface ScanReport {
  photos: number
  files_in_db: number
  originals_on_disk: number
  missing_originals: Finding
  orphan_files: Finding
  missing_thumbnails: Finding
  missing_embeddings: Finding
  missing_faces: Finding
  missing_phashes: Finding
}

/** The opt-in repairs (`maintenance.RepairOptions`). */
export interface RepairOptions {
  thumbnails?: boolean
  embeddings?: boolean
  faces?: boolean
  phashes?: boolean
  import_orphans?: boolean
}

/** What each selected repair scheduled or did (`maintenance.RepairResult`). */
export interface RepairResult {
  thumbnails_enqueued: number
  embeddings_enqueued: number
  faces_enqueued: number
  phashes_enqueued: number
  orphans_imported: number
  orphans_skipped: number
  orphans_failed: number
}

/** Runs an integrity scan and returns the report. */
export async function fetchMaintenanceScan(signal?: AbortSignal): Promise<ScanReport> {
  const res = await fetch(`${API_BASE}/maintenance/scan`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as ScanReport
}

/** Runs the selected repairs and returns the result. */
export async function runMaintenanceRepair(
  options: RepairOptions,
  signal?: AbortSignal,
): Promise<RepairResult> {
  const res = await fetch(`${API_BASE}/maintenance/repair`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(options),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as RepairResult
}

/** Outcome of an audit-log retention purge (`maintenanceapi` audit purge). */
export interface AuditPurgeResult {
  deleted: number
  older_than_days: number
  cutoff: string
}

/**
 * Purges audit-log entries older than the given retention window (in days),
 * returning how many were deleted. Destructive and maintainer-only; the purge is
 * self-audited on the backend.
 */
export async function purgeAuditLog(
  olderThanDays: number,
  signal?: AbortSignal,
): Promise<AuditPurgeResult> {
  const res = await fetch(`${API_BASE}/maintenance/audit/purge`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ older_than_days: olderThanDays }),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as AuditPurgeResult
}
