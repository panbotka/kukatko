import { ApiError } from './auth'
import type { ImportRun } from './import'

/**
 * System client, mirroring the backend JSON shapes from `internal/systemapi` and
 * `internal/system`. It covers two endpoints: the maintainer-only status
 * snapshot and the library statistics every signed-in user may read
 * ({@link fetchLibraryStats}). The former powers the status dashboard:
 * one aggregated snapshot of embeddings reachability, job-queue depth, the
 * backup subsystem, the last import per source, storage usage, database
 * reachability, the map provider's health (a rejected mapy.com key shows up
 * here, not only as a grey map) and the reverse-geocode credit budget, plus
 * the quick actions (trigger a backup, requeue the dead-letter jobs).
 * The session cookie is sent automatically
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

/**
 * Job-queue section (`system.Jobs`). The queue table keeps finished jobs, so
 * `by_type` and `total` are lifetime tallies ("jobs ever run") rather than queue
 * depth — only `by_type_state` says what is actually waiting, which is why it is
 * what the dashboard renders and the totals are only ever shown labelled as
 * history.
 */
export interface JobsStatus {
  by_state: Record<string, number | undefined>
  by_type: Record<string, number | undefined>
  /** Per job type, its counts per lifecycle state; absent pairs mean zero. */
  by_type_state: Record<string, Record<string, number | undefined> | undefined>
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

/**
 * Last-import section (`system.Imports`): the most recent `kukatko import dir`
 * run, the only import that can still happen. Null when none has ever run.
 */
export interface ImportsStatus {
  folder: ImportRun | null
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

/**
 * Reverse-geocode credit section (`system.Geocode`). Every geocode the `places`
 * job performs costs a metered mapy.com credit, so the budget caps how many one
 * window may spend; when it runs out the queued jobs wait for `resets_at`
 * instead of failing. `budget_enabled` is false when the cap is switched off
 * (`maps.geocode_budget <= 0`), leaving only the per-second rate limiter.
 */
export interface GeocodeStatus {
  configured: boolean
  budget_enabled: boolean
  limit: number
  spent: number
  remaining: number
  window_seconds: number
  resets_at?: string
}

/** How much arrived recently (`system.Uploads`); the windows nest. */
export interface Uploads {
  day: number
  week: number
  month: number
  year: number
}

/**
 * The dashboard's library summary (`system.LibrarySummary`): what is in the
 * library, what is kept out of it, what arrived recently and what it all weighs.
 *
 * `photos` is the browsable catalogue — the trash is `trashed` and is NOT part of
 * it — while `hidden` and `private` are subsets of `photos` (both are in the
 * library, just kept out of the grid or marked private).
 *
 * The two byte sums are the **catalogue's** arithmetic over the originals'
 * recorded sizes, which is a different question from {@link StorageStatus}'s
 * measurement of the server's disk: this instance keeps its originals in an
 * object store, so the disk holds none of them. `derived_bytes` is the exception
 * — derived media is never in the catalogue, so that one is measured.
 */
export interface LibrarySummary {
  photos: number
  videos: number
  trashed: number
  hidden: number
  private: number
  uploads: Uploads
  albums: number
  labels: number
  people: number
  faces: number
  embeddings: number
  library_bytes: number
  trash_bytes: number
  derived_bytes: number
}

/**
 * The near-duplicate scan's last answer (`system.DuplicateScan`). It is the one
 * dashboard number that is not counted while the request is served: the scan is
 * far too expensive for a polled endpoint, so the backend refreshes it in the
 * background. `available` false means "no answer yet" — `groups` is then not a
 * count of zero and must not be rendered as one.
 */
export interface DuplicateScan {
  configured: boolean
  available: boolean
  groups: number
  computed_at?: string
}

/**
 * The dashboard's remaining-work section (`system.RemainingWork`): the backlogs
 * of human and machine work. Every number is one where zero is the good value.
 */
export interface RemainingWork {
  faces_unassigned: number
  clusters: number
  photos_without_taken_at: number
  photos_without_gps: number
  photos_without_place: number
  photos_without_ocr: number
  duplicate_markers: number
  duplicates: DuplicateScan
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
  geocode: GeocodeStatus
  library: LibrarySummary
  remaining: RemainingWork
}

/**
 * The library-statistics snapshot (`system.Library`): instance-wide counts of
 * what the catalogue holds and how much of it has been processed. Unlike the
 * status snapshot above it is readable by every signed-in user, not just
 * maintainers. `photos_without_embedding` / `photos_without_faces` /
 * `faces_unassigned` are the coverage gaps the backend derives, so the page
 * never has to subtract by hand.
 *
 * Three counts describe faces and only two of them share a grain: `faces` is
 * every detection (`faces_assigned + faces_unassigned`), while `markers` is the
 * boxes drawn on photos (`markers_assigned + markers_unassigned`). The two
 * families overlap and must never be added across.
 *
 * Four counts describe how many photos there are, and only the last of them is
 * the number the library grid reports: `photos` is the whole catalogue,
 * `photos_live` is it without the trash, and `photos_listed` is `photos_live`
 * minus `photos_hidden` and `photos_stacked` — the photos the user hid and the
 * variants folded behind a stack's primary. The three parts are disjoint and add
 * back up to `photos_live`, which is what lets the statistics page show the
 * subtraction instead of leaving the reader to wonder why two screens disagree.
 */
export interface LibraryStats {
  photos: number
  videos: number
  photos_live: number
  photos_archived: number
  photos_hidden: number
  photos_stacked: number
  photos_listed: number
  photos_with_embedding: number
  photos_with_faces: number
  photos_without_embedding: number
  photos_without_faces: number
  photos_with_gps: number
  embeddings: number
  faces: number
  faces_assigned: number
  faces_unassigned: number
  subjects: number
  subjects_person: number
  subjects_pet: number
  subjects_other: number
  markers: number
  markers_assigned: number
  markers_unassigned: number
  albums: number
  labels: number
}

/** One bar of the capture-year histogram (`system.YearPhotos`). */
export interface YearPhotos {
  year: number
  photos: number
}

/** One bar of the "added to the library" chart (`system.MonthPhotos`). */
export interface MonthPhotos {
  /** The calendar month as `YYYY-MM` — a bucket label, not a timestamp. */
  month: string
  photos: number
}

/** One bar of the top-cameras chart (`system.CameraPhotos`). */
export interface CameraPhotos {
  /** Display name, the make and model folded into one ("Canon EOS 5D"). */
  camera: string
  /** The bare `camera_model`, which is what the library's `camera` filter matches. */
  model: string
  photos: number
}

/** One slice of the storage-by-media breakdown (`system.MediaStorage`). */
export interface MediaStorage {
  /** The bucket: `image`, `live`, `video` or `raw`. */
  media: string
  photos: number
  bytes: number
}

/** One bar of the library-growth chart (`system.YearStorage`). */
export interface YearStorage {
  /** The year the photos were **added** in, not the year they were taken. */
  year: number
  photos: number
  bytes: number
  /** The library's size at the end of that year; derived by the backend. */
  cumulative_bytes: number
}

/**
 * The chart series behind the statistics page (`system.Charts`), a separate
 * endpoint from the counts above: heavier to aggregate, slower to change, and
 * cached for longer server-side, so the cheap numbers are never held up by them.
 *
 * Every series covers the **browsable** library (the trash is excluded
 * throughout), is gap-filled and ascending, and is never null — so a chart can
 * walk it as a time axis without reconstructing the missing buckets.
 */
export interface LibraryCharts {
  photos_by_year: YearPhotos[]
  added_by_month: MonthPhotos[]
  top_cameras: CameraPhotos[]
  storage_by_media: MediaStorage[]
  storage_by_year: YearStorage[]
}

/** Response body of `POST /api/v1/jobs/requeue-dead`. */
interface RequeueDeadResponse {
  requeued: number
}

/** Fetches the aggregated system-status snapshot. */
export async function fetchSystemStatus(signal?: AbortSignal): Promise<SystemStatus> {
  return getJSON<SystemStatus>('/system/status', signal)
}

/**
 * Fetches the library statistics. Open to every signed-in user (unlike the
 * status snapshot) and the single source for both the statistics page and the
 * System page's Library section. It throws {@link ApiError} when the backend
 * cannot aggregate the counts, so callers show an error rather than zeroes.
 */
export async function fetchLibraryStats(signal?: AbortSignal): Promise<LibraryStats> {
  return getJSON<LibraryStats>('/system/stats', signal)
}

/**
 * Fetches the chart series behind the statistics page. Open to the same readers
 * as {@link fetchLibraryStats} and deliberately a second request: the counts
 * render as soon as they arrive, while these heavier aggregates fill the charts
 * in behind them. It throws {@link ApiError} when the backend cannot aggregate,
 * so callers show an error rather than an empty-looking library.
 */
export async function fetchLibraryCharts(signal?: AbortSignal): Promise<LibraryCharts> {
  return getJSON<LibraryCharts>('/system/stats/charts', signal)
}

/**
 * Triggers an S3 backup in the background. Throws ApiError 409 when one is
 * already running and 503 when no backup destination is configured.
 */
export async function triggerBackup(signal?: AbortSignal): Promise<void> {
  return postVoid('/backup', signal)
}

/**
 * Requeues dead-lettered jobs back onto the queue and returns how many went
 * back. With no `jobType` it empties the whole dead letter; with one it retries
 * only that kind of work, so the one thing that broke can be retried without also
 * retrying everything ever given up on.
 *
 * It is a single call: the backend does it in one statement, which matters
 * because the case this exists for is a dead letter of thousands.
 */
export async function requeueDeadLetterJobs(
  jobType?: string,
  signal?: AbortSignal,
): Promise<number> {
  const query = jobType === undefined ? '' : `?type=${encodeURIComponent(jobType)}`
  const body = await postJSON<RequeueDeadResponse>(`/jobs/requeue-dead${query}`, signal)
  return body.requeued
}
