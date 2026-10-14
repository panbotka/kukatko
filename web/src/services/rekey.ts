import { ApiError } from './auth'
import { instancePath } from './api'

/**
 * The system administrator's client of the storage move
 * (`internal/rekeyapi`): one project's move from the root of the object store
 * under `projects/<uid>/`. The routes are instance-level and maintainer-only,
 * addressed by the project's uid, so every URL is built with
 * {@link instancePath}. The worker does the moving; these calls only read the
 * run and steer it.
 *
 * A refusal throws {@link RekeyApiError}, which carries the backend's
 * machine-readable `code` (`already_running`, `already_prefixed`,
 * `invalid_state`, `not_found`).
 */

/** Where a run is. */
export type RekeyState = 'running' | 'paused' | 'done' | 'failed'

/** The machine-readable refusal codes of `internal/rekeyapi`. */
export type RekeyErrorCode =
  | 'not_found'
  | 'already_running'
  | 'already_prefixed'
  | 'invalid_state'
  | 'internal'

/** A refusal of the rekey API; `message` (inherited) and `code` are the code. */
export class RekeyApiError extends ApiError {
  readonly code: string

  constructor(status: number, code: string) {
    super(status, code)
    this.name = 'RekeyApiError'
    this.code = code
  }
}

/** One error a run met: the key it was working on, what went wrong and when. */
export interface RekeyRunError {
  key: string
  message: string
  at: string
}

/** One storage move of one project (`GET /projects/{uid}/rekey`). */
export interface RekeyRun {
  uid: string
  project_uid: string
  state: RekeyState
  /** The prefix the objects move under: `projects/<uid>/`. */
  target_prefix: string
  /** What was in the old layout when the run started. */
  total_photos: number
  total_bytes: number
  /** What has moved so far. */
  done_photos: number
  done_bytes: number
  /** The old key of the last photo the pass finished — the cursor. */
  last_key: string
  /** The most recent errors, oldest first (at most 50). */
  errors: RekeyRunError[] | null
  /** Every error the run ever met. */
  error_count: number
  /**
   * Moved photos whose thumbnails or streaming segments are still at the root,
   * waiting for a retry. A run ends `done` only when this is 0.
   */
  derived_failed: number
  started_at: string
  updated_at: string
  /** Absent until the run is done. */
  finished_at?: string
}

/** The refusal envelope of the rekey API. */
interface ErrorBody {
  error?: string
}

/** Turns a non-OK response into a {@link RekeyApiError}. */
async function toError(res: Response): Promise<RekeyApiError> {
  let code = ''
  try {
    const body = (await res.json()) as ErrorBody
    code = typeof body.error === 'string' ? body.error : ''
  } catch {
    // An empty or non-JSON body: the status text stands in for the code.
  }
  return new RekeyApiError(
    res.status,
    code !== '' ? code : res.statusText || `request failed: ${res.status}`,
  )
}

/** The rekey URL of the project `uid`, with an optional action segment. */
function rekeyPath(uid: string, action = ''): string {
  return instancePath(`/projects/${encodeURIComponent(uid)}/rekey${action}`)
}

/** Issues one request and decodes the run it answers with. */
async function request(method: string, url: string, signal?: AbortSignal): Promise<RekeyRun> {
  const res = await fetch(url, { method, credentials: 'same-origin', signal })
  if (!res.ok) {
    throw await toError(res)
  }
  return (await res.json()) as RekeyRun
}

/**
 * The project's latest storage move, or `null` when it never had one (the
 * API's 404 — which is also its answer for an unknown project).
 */
export async function fetchRekeyRun(uid: string, signal?: AbortSignal): Promise<RekeyRun | null> {
  try {
    return await request('GET', rekeyPath(uid), signal)
  } catch (error: unknown) {
    if (error instanceof RekeyApiError && error.status === 404) {
      return null
    }
    throw error
  }
}

/**
 * Starts the move: the project's prefix switches at once (new uploads land
 * under it) and a `running` run is queued.
 *
 * @throws RekeyApiError `already_running` (409, a run is running or paused),
 *   `already_prefixed` (409, a project that never lived in the root).
 */
export async function startRekey(uid: string): Promise<RekeyRun> {
  return request('POST', rekeyPath(uid))
}

/**
 * Pauses a running move; the job stops after the photo in hand.
 *
 * @throws RekeyApiError `invalid_state` (409), `not_found` (404).
 */
export async function pauseRekey(uid: string): Promise<RekeyRun> {
  return request('POST', rekeyPath(uid, '/pause'))
}

/**
 * Resumes a paused move.
 *
 * @throws RekeyApiError `invalid_state` (409), `not_found` (404).
 */
export async function resumeRekey(uid: string): Promise<RekeyRun> {
  return request('POST', rekeyPath(uid, '/resume'))
}

/**
 * Puts a failed move back to running with its cursor reset; its error count
 * and the list of its last errors are kept.
 *
 * @throws RekeyApiError `invalid_state` (409), `not_found` (404).
 */
export async function retryRekey(uid: string): Promise<RekeyRun> {
  return request('POST', rekeyPath(uid, '/retry'))
}
