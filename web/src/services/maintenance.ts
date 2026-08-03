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

/**
 * One subject whose name identifies nobody, with how much of the catalogue
 * currently points at it (`people.NamelessSubject`). Such a subject cannot be
 * created deliberately — it is a catch-all an importer minted — and its counts
 * are what let a maintainer tell it apart from a real person before deciding.
 */
export interface NamelessSubject {
  uid: string
  slug: string
  name: string
  type: string
  created_at: string
  marker_count: number
  face_count: number
}

/** The read-only nameless-subject report with the totals across its subjects. */
export interface NamelessReport {
  subjects: NamelessSubject[]
  marker_total: number
  face_total: number
}

/** Runs the read-only nameless-subject report. Safe: it never writes. */
export async function fetchNamelessSubjects(signal?: AbortSignal): Promise<NamelessReport> {
  const res = await fetch(`${API_BASE}/maintenance/nameless-subjects`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as NamelessReport
}

/** The undo file a detach handed over, with what it covers. */
export interface NamelessUndoFile {
  filename: string
  subjects: number
  markers: number
  faces: number
}

/** Reads a positive integer response header, defaulting to 0. */
function headerCount(res: Response, name: string): number {
  const raw = Number(res.headers.get(name))
  return Number.isFinite(raw) && raw > 0 ? raw : 0
}

/** Extracts the server-chosen filename from Content-Disposition, if present. */
function attachmentName(res: Response, fallback: string): string {
  const match = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')
  return match?.[1] ?? fallback
}

/**
 * Applies the nameless-subject repair: the response body *is* the undo file, so
 * it is saved to the user's downloads before this resolves. The backend writes
 * the file first and schedules the detach only once it has gone out — the HTTP
 * form of the CLI refusing `--apply` without `--undo-file` — and the stream ends
 * after the scheduling, so holding the file means the work is queued.
 *
 * Destructive and maintainer-only. The detach itself runs in the background job
 * queue; keep the returned file, it is the only way back.
 *
 * @throws ApiError with `status` 409 (nothing to detach), 503 (repair not wired)
 *   or 5xx so the caller can render the matching message.
 */
export async function detachNamelessSubjects(signal?: AbortSignal): Promise<NamelessUndoFile> {
  const res = await fetch(`${API_BASE}/maintenance/nameless-subjects/detach`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const filename = attachmentName(res, 'kukatko-nameless-undo.json')
  saveBlob(await res.blob(), filename)
  return {
    filename,
    subjects: headerCount(res, 'X-Kukatko-Nameless-Subjects'),
    markers: headerCount(res, 'X-Kukatko-Nameless-Markers'),
    faces: headerCount(res, 'X-Kukatko-Nameless-Faces'),
  }
}

/** How many restore jobs an uploaded undo file scheduled. */
export interface NamelessRestoreResult {
  queued: number
}

/**
 * Replays an undo file: every subject it records is re-created under its original
 * uid and the markers and faces it owned are re-assigned. The work runs in the
 * background job queue, so the result reports what was scheduled, not what is
 * already done.
 *
 * @throws ApiError with `status` 400 (not a usable undo file), 503 or 5xx.
 */
export async function restoreNamelessSubjects(
  file: File,
  signal?: AbortSignal,
): Promise<NamelessRestoreResult> {
  const res = await fetch(`${API_BASE}/maintenance/nameless-subjects/restore`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: file,
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as NamelessRestoreResult
}

/**
 * Saves a Blob to the user's downloads as `filename` by clicking a temporary
 * anchor pointed at an object URL, revoking the URL afterwards so the blob can be
 * garbage-collected.
 */
function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}
