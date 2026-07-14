import { ApiError } from './auth'
import type { ImportRun } from './import'

/**
 * Admin system-status client, mirroring the backend JSON shapes from
 * `internal/systemapi` and `internal/system`. It powers the status dashboard:
 * one aggregated snapshot of embeddings reachability, job-queue depth, the
 * backup subsystem, the last import per source, storage usage, database
 * reachability and the map provider's health (a rejected mapy.com key shows up
 * here, not only as a grey map), plus the quick actions (trigger a backup,
 * requeue the dead-letter jobs). The session cookie is sent automatically
 * (same-origin); every call throws {@link ApiError} on a non-OK response so
 * callers can branch on `status`.
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

/** Issues a POST and returns nothing useful, throwing ApiError on a non-OK status. */
async function postVoid(path: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
}

/** Database reachability section (`system.Database`). */
export interface DatabaseStatus {
  reachable: boolean
  error?: string
}

/** Embeddings sidecar reachability section (`system.Embeddings`). */
export interface EmbeddingsStatus {
  online: boolean
  url: string
}

/** Job-queue depth section (`system.Jobs`). */
export interface JobsStatus {
  by_state: Record<string, number | undefined>
  by_type: Record<string, number | undefined>
  total: number
  dead_letter: number
  pending_embeddings: number
}

/** Backup subsystem section (`backup.Status`). */
export interface BackupStatus {
  configured: boolean
  running: boolean
  last_started_at?: string
  last_finished_at?: string
  last_error?: string
  last_result?: {
    dump_key: string
    originals_uploaded: number
    originals_skipped: number
    dumps_pruned: number
  }
}

/** Last-import-per-source section (`system.Imports`). */
export interface ImportsStatus {
  photoprism: ImportRun | null
  photosorter: ImportRun | null
}

/** On-disk storage usage section (`system.StorageUsage`). */
export interface StorageStatus {
  originals_bytes: number
  cache_bytes: number
  free_bytes: number
  total_bytes: number
}

/** Build version section (`version.Info`). */
export interface VersionInfo {
  version: string
  commit: string
}

/**
 * The map provider's last observed state (`mapy.HealthState`). `key_rejected`
 * means mapy.com is refusing the server's API key — the map has no tiles until a
 * human replaces the key in the mapy.com console.
 */
export type MapsState = 'unknown' | 'ok' | 'key_rejected' | 'rate_limited' | 'unavailable' | 'error'

/** Map-provider (mapy.com) section (`system.Maps`). */
export interface MapsStatus {
  configured: boolean
  state: MapsState
  degraded: boolean
  detail?: string
  checked_at?: string
}

/** The full system-status snapshot (`system.Status`). */
export interface SystemStatus {
  version: VersionInfo
  database: DatabaseStatus
  embeddings: EmbeddingsStatus
  jobs: JobsStatus
  backup: BackupStatus
  imports: ImportsStatus
  storage: StorageStatus
  maps: MapsStatus
}

/** One job from the admin listing (`jobs.Job`); only the id is needed here. */
interface JobSummary {
  id: number
}

/** Response body of `GET /api/v1/jobs`. */
interface JobListResponse {
  jobs: JobSummary[]
}

/** Fetches the aggregated system-status snapshot. */
export async function fetchSystemStatus(signal?: AbortSignal): Promise<SystemStatus> {
  return getJSON<SystemStatus>('/system/status', signal)
}

/**
 * Triggers an S3 backup in the background. Throws ApiError 409 when one is
 * already running and 503 when no backup destination is configured.
 */
export async function triggerBackup(signal?: AbortSignal): Promise<void> {
  return postVoid('/backup', signal)
}

/**
 * Requeues every dead-lettered job back onto the queue and returns how many were
 * requeued. It lists the dead-letter jobs, then requeues each one; a job that has
 * meanwhile been requeued by someone else (404/409) is skipped rather than
 * failing the whole action.
 */
export async function requeueDeadLetterJobs(signal?: AbortSignal): Promise<number> {
  const list = await getJSON<JobListResponse>('/jobs?state=dead&limit=500', signal)
  let requeued = 0
  for (const job of list.jobs) {
    try {
      await postVoid(`/jobs/${String(job.id)}/requeue`, signal)
      requeued += 1
    } catch (err) {
      // A job already requeued elsewhere (404/409) is fine to skip; anything else
      // aborts so the admin sees the failure.
      if (err instanceof ApiError && (err.status === 404 || err.status === 409)) {
        continue
      }
      throw err
    }
  }
  return requeued
}
