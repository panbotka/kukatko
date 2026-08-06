import { ApiError } from './auth'

/**
 * Admin import client, mirroring the backend JSON shapes from `internal/importapi`
 * and `internal/jobsapi`. It drives the read-only import admin UI: the run
 * history, the recorded per-photo failures, and the job queue stats. There is
 * nothing to trigger — the only remaining import is `kukatko import dir`, run
 * from the CLI; the PhotoPrism/photo-sorter migration finished in August 2026 and
 * its triggers are gone. The session cookie is sent automatically (same-origin);
 * every call throws {@link ApiError} on a non-OK response so callers can branch
 * on `status`.
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

/**
 * Every source a recorded run can carry (`importer.Source`): `folder` is a
 * `kukatko import dir` run, the only one still produced; the other three are the
 * finished migration, whose runs stay in the history as the catalogue's
 * provenance record.
 */
export type RunSource = 'folder' | 'photoprism' | 'photosorter' | 'photosorter_feeds'

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
  /**
   * Source photos whose content was already catalogued under a different source
   * photo, so they collapsed onto that row instead of getting one of their own.
   * Runs recorded before this bucket existed have no key for it, hence optional.
   */
  deduplicated?: number
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

/** Response body of `GET /api/v1/import/runs`. */
export interface ImportRunsResponse {
  runs: ImportRun[]
  limit: number
  offset: number
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

/** Fetches the import-run history, most recently started first. */
export async function fetchImportRuns(signal?: AbortSignal): Promise<ImportRunsResponse> {
  return getJSON<ImportRunsResponse>('/import/runs', signal)
}

/** Fetches the aggregate job-queue stats (counts by state and type). */
export async function fetchJobStats(signal?: AbortSignal): Promise<JobStats> {
  return getJSON<JobStats>('/jobs/stats', signal)
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

/** Every source a failure can be recorded under (`importer.Source`). */
export type FailureSource = RunSource

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
