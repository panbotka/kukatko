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

/** Lifecycle state of an import run (`importer.Status`). */
export type RunStatus = 'running' | 'done' | 'failed'

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
