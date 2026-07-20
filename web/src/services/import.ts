import { ApiError } from './auth'

/**
 * Admin import client, mirroring the backend JSON shapes from `internal/importapi`
 * and `internal/jobsapi`. It drives the import admin UI: triggering a PhotoPrism
 * import or photo-sorter migration, reading the run history, and polling the job
 * queue stats. The session cookie is sent automatically (same-origin); every call
 * throws {@link ApiError} on a non-OK response so callers can branch on `status`.
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

/** Issues a GET and parses the JSON body, throwing ApiError on a non-OK status. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** Issues a POST and parses the JSON body, throwing ApiError on a non-OK status. */
async function postJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** Import sources that can be triggered from the UI (`importer.Source`). */
export type ImportSource = 'photoprism' | 'photosorter'

/**
 * Every source a recorded run can carry: the triggerable ones plus `folder`, a
 * `kukatko import dir` run. A folder import is driven from the CLI (it reads a
 * directory on the server's disk), so it has no start button — but its runs show
 * up in the same history.
 */
export type RunSource = ImportSource | 'folder'

/**
 * Lifecycle state of an import run (`importer.Status`). `partial` means the run
 * finished its scan but recorded at least one unresolved per-photo/per-file
 * failure, so it is deliberately not reported as a clean `done`.
 */
export type RunStatus = 'running' | 'done' | 'partial' | 'failed'

/** Per-run tally of photos handled (`importer.Counts`). */
export interface ImportCounts {
  imported: number
  updated: number
  skipped: number
  failed: number
}

/** One import or migration run from the history (`importer.Run`). */
export interface ImportRun {
  id: number
  source: RunSource
  started_at: string
  finished_at: string | null
  status: RunStatus
  high_watermark: string | null
  counts: ImportCounts
  last_error: string
}

/** Which import sources are configured on the backend. */
export interface ImportSources {
  photoprism: boolean
  photosorter: boolean
}

/** Response body of `GET /api/v1/import/runs`. */
export interface ImportRunsResponse {
  runs: ImportRun[]
  limit: number
  offset: number
  sources: ImportSources
}

/** Response body of an import trigger (`importapi.importResponse`). */
export interface StartImportResult {
  job_id: number
  status: string
}

/**
 * Aggregate job-queue counts (`jobsapi.statsResponse`). A state or type with no
 * jobs is simply absent from the map, so lookups may be undefined.
 */
export interface JobStats {
  by_state: Record<string, number | undefined>
  by_type: Record<string, number | undefined>
  total: number
}

/**
 * Fetches the import-run history together with which sources are configured. The
 * runs are ordered most recently started first.
 */
export async function fetchImportRuns(signal?: AbortSignal): Promise<ImportRunsResponse> {
  return getJSON<ImportRunsResponse>('/import/runs', signal)
}

/** Fetches the aggregate job-queue stats (counts by state and type). */
export async function fetchJobStats(signal?: AbortSignal): Promise<JobStats> {
  return getJSON<JobStats>('/jobs/stats', signal)
}

/**
 * Triggers an import run for the given source by enqueuing a background job.
 * Throws ApiError 409 when a run of that source is already in progress.
 */
export async function startImport(
  source: ImportSource,
  signal?: AbortSignal,
): Promise<StartImportResult> {
  return postJSON<StartImportResult>(`/import/${source}`, signal)
}

/** The import step a failure happened in (`importer.Stage`). */
export type FailureStage =
  | 'photo'
  | 'file'
  | 'marker'
  | 'album_member'
  | 'label'
  | 'thumbnail'
  | 'embedding'
  | 'faces'
  | 'phash'
  | 'edit'
  | 'metadata'

/** Every source a failure can be recorded under (`importer.Source`), which unlike
 * a triggerable run source also includes the feeds import. */
export type FailureSource = ImportSource | 'photosorter_feeds' | 'folder'

/** One persisted per-photo/per-file import failure (`importer.Failure`). */
export interface ImportFailure {
  id: number
  run_id: number
  source: FailureSource
  stage: FailureStage
  photo_uid: string
  source_ref: string
  detail: string
  error: string
  created_at: string
  resolved_at: string | null
}

/** Response body of `GET /api/v1/import/failures`. */
export interface ImportFailuresResponse {
  failures: ImportFailure[]
  limit: number
  offset: number
}

/**
 * Fetches recorded import failures, most recently recorded first. When
 * `unresolvedOnly` is set only outstanding failures are returned.
 */
export async function fetchImportFailures(
  opts: { unresolvedOnly?: boolean; limit?: number } = {},
  signal?: AbortSignal,
): Promise<ImportFailuresResponse> {
  const params = new URLSearchParams()
  if (opts.unresolvedOnly) params.set('unresolved', 'true')
  if (opts.limit) params.set('limit', String(opts.limit))
  const query = params.toString()
  return getJSON<ImportFailuresResponse>(`/import/failures${query ? `?${query}` : ''}`, signal)
}

/** PhotoPrism photo/file reconciliation (`importverify.PhotoPrismReport`). */
export interface PhotoPrismReport {
  source_total: number
  source_by_type: Record<string, number | undefined>
  imported_count: number
  deduplicated_count: number
  missing_count: number
  missing_uids: string[]
  file_gap_count: number
  file_gaps: { photoprism_uid: string; expected: number; actual: number }[]
}

/** photo-sorter vectors reconciliation (`importverify.VectorsReport`). */
export interface VectorsReport {
  not_configured: boolean
  source_total_photos: number
  source_photos_with_embeddings: number
  source_photos_with_faces: number
  source_total_faces: number
  catalog_embeddings: number
  catalog_face_photos: number
  catalog_faces: number
  missing_embeddings_count: number
  missing_embeddings: string[]
  missing_faces_count: number
  missing_faces: string[]
}

/** Source-vs-catalogue counts for one entity kind (`importverify.EntityReport`). */
export interface EntityReport {
  source_count: number
  catalog_count: number
  missing_count: number
  missing: string[]
}

/** Album/label/subject reconciliation (`importverify.StructureReport`). */
export interface StructureReport {
  albums: EntityReport
  labels: EntityReport
  subjects: EntityReport
}

/** Full completeness report (`importverify.Report`) from `GET /import/verify`. */
export interface VerifyReport {
  photoprism: PhotoPrismReport
  vectors: VectorsReport
  structure: StructureReport
  complete: boolean
}

/**
 * Runs the import-completeness reconciliation and returns its report. This may
 * take a while (it walks the whole source library); throws ApiError 503 when no
 * import source is configured.
 */
export async function fetchVerifyReport(signal?: AbortSignal): Promise<VerifyReport> {
  return getJSON<VerifyReport>('/import/verify', signal)
}
