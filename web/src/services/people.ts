import { ApiError } from './auth'
import { buildPhotoQuery, type Photo, type PhotoListParams, type PhotoListResponse } from './photos'

/**
 * People/face client for the subject catalogue, on-photo face assignment, the
 * unnamed-face cluster review queue, and per-subject outlier detection. It
 * mirrors the backend JSON shapes from `internal/peopleapi`, `internal/facematch`,
 * `internal/clusterapi` and `internal/outlierapi`. The session cookie is sent
 * automatically (same-origin); every call throws {@link ApiError} on a non-OK
 * response so callers can branch on `status`.
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
 * Issues a body-carrying request (POST/PATCH/DELETE) and parses the JSON body,
 * throwing ApiError on a non-OK status. A 204 (or otherwise empty) response
 * resolves to `undefined`, so callers expecting no content can ignore the result.
 */
async function sendJSON<T>(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  return (text === '' ? undefined : JSON.parse(text)) as T
}

/** Normalised bounding box `[x, y, w, h]` in display space, each value in 0..1. */
export type Bbox = [number, number, number, number]

/** Subject classification, mirroring the backend `people.SubjectType`. */
export type SubjectType = 'person' | 'pet' | 'other'

/** The recognised subject types, for building selectors. */
export const SUBJECT_TYPES: readonly SubjectType[] = ['person', 'pet', 'other']

/** A named subject (person, pet, other), mirroring `people.Subject`. */
export interface Subject {
  uid: string
  slug: string
  name: string
  type: SubjectType
  favorite: boolean
  private: boolean
  notes: string
  cover_photo_uid?: string
  /**
   * The year the person was born, `null` when unknown — which is the normal
   * case. A year rather than a date, because a year is what anybody actually
   * knows about the people in a family archive; every age derived from it is an
   * approximation and shown as one (`lib/lifeYears`).
   */
  birth_year: number | null
  /** The year the person died, `null` when unknown (or still alive). */
  death_year: number | null
  created_at: string
  updated_at: string
}

/**
 * The face the backend picked to illustrate a subject (`people.SubjectFace`): a
 * photo, the normalised `[x, y, w, h]` box of the subject's face on it, and that
 * photo's stored frame. There is no face-thumbnail endpoint, so the client crops
 * a cached thumbnail to the box itself — which needs the frame, since a
 * normalised box says nothing about its own proportions.
 */
export interface SubjectFace {
  photo_uid: string
  x: number
  y: number
  w: number
  h: number
  /** The source photo's stored pixel width (before EXIF orientation). */
  width: number
  /** The source photo's stored pixel height (before EXIF orientation). */
  height: number
  /** The raw EXIF orientation tag (1–8), 0 when absent. */
  orientation: number
}

/**
 * A subject paired with how much of the library it appears on
 * (`people.SubjectCount`). The two counts are different questions and the call
 * site picks the one it means — see the fields.
 */
export interface SubjectCount extends Subject {
  /**
   * How many non-invalid markers on visible photos point at the subject. This is
   * the figure the face tools want: they work one marker at a time.
   */
  marker_count: number
  /**
   * On how many visible photos the subject appears — at most `marker_count`, and
   * lower whenever one photo carries several of the subject's faces. This is what
   * a "N photos" label must use, because it is what the subject's gallery shows.
   */
  photo_count: number
  /**
   * The automatically picked face, absent when the subject has no usable marker.
   * It is a *fallback* for a subject with no `cover_photo_uid` and never overrides
   * one — see `subjectTileImage`, which owns that rule.
   */
  cover_face?: SubjectFace
}

/** Response body of `GET /api/v1/subjects`. */
interface SubjectsResponse {
  subjects: SubjectCount[]
}

/**
 * Editable subject fields sent to create (`POST /subjects`) and update
 * (`PATCH /subjects/{uid}`). A `null` cover clears it; omitting it leaves it as
 * the caller supplied.
 */
export interface SubjectInput {
  name: string
  type: SubjectType
  favorite: boolean
  private: boolean
  notes: string
  cover_photo_uid: string | null
  /**
   * Birth year, `null` for unknown. The body rewrites the whole editable set, so
   * `null` **clears** a stored year rather than leaving it alone — every caller
   * has to send what the subject should end up with. The backend rejects a year
   * outside 1800…this year, or a death before the birth, with a 400.
   */
  birth_year: number | null
  /** Death year, `null` for unknown; cleared by `null` exactly like the birth. */
  death_year: number | null
}

/** Lists every subject with its photo and marker counts, ordered by name. */
export async function fetchSubjects(signal?: AbortSignal): Promise<SubjectCount[]> {
  const body = await getJSON<SubjectsResponse>('/subjects', signal)
  return body.subjects
}

/**
 * The address of a subject's avatar: the small square JPEG the backend cuts from
 * the person's face (or from the cover photo somebody chose for them).
 *
 * It is a plain `<img>` source, not a fetch — the session cookie rides along with
 * the request, and the browser caches the picture. It exists because cropping a
 * face in the page means downloading a whole-frame preview measured in megapixels
 * to paint a 150 px square; this URL answers with about 15 kB. A subject with no
 * picture at all answers 404, so ask `subjectTileImage` before rendering one.
 */
export function subjectAvatarUrl(uid: string): string {
  return `${API_BASE}/subjects/${encodeURIComponent(uid)}/avatar`
}

/**
 * How many decimals of a normalised box travel in a face-crop URL.
 *
 * It matches the precision the backend's cache key is a digest of, so two
 * requests for the same face are one cache entry there and one entry in the
 * browser's cache — and rounding here rather than letting floating-point noise
 * through is what keeps the URL stable across renders.
 */
const FACE_BOX_PRECISION = 4

/**
 * The address of one detected face as a small square JPEG, cut server-side from
 * the given normalised `[x, y, w, h]` box (the photo's *display* space, which is
 * where every marker box lives).
 *
 * It is the per-face twin of {@link subjectAvatarUrl} and shares its renderer, so
 * the padding, the squaring and the choice of source preview happen once and in
 * one place. Use it wherever a face is shown as a small square: cropping one in
 * the page means downloading the entire photograph — measured on a person's
 * page, 290 `fit_1280` previews to paint 290 tiles of 96 px — where this URL
 * answers with the crop itself.
 *
 * It is a plain `<img>` source, not a fetch: the session cookie rides along, and
 * the response is cached like any other photo imagery. The box is not validated
 * here — a degenerate one answers 400, which the caller shows as a missing
 * picture rather than as an error.
 */
export function faceCropUrl(photoUid: string, bbox: Bbox): string {
  const box = bbox.map((value) => value.toFixed(FACE_BOX_PRECISION)).join(',')
  return `${API_BASE}/photos/${encodeURIComponent(photoUid)}/face?box=${encodeURIComponent(box)}`
}

/** Fetches one subject by UID; throws ApiError 404 when missing. */
export async function fetchSubject(uid: string, signal?: AbortSignal): Promise<Subject> {
  return getJSON<Subject>(`/subjects/${encodeURIComponent(uid)}`, signal)
}

/** Creates a subject from the editable fields and returns the stored record. */
export async function createSubject(input: SubjectInput, signal?: AbortSignal): Promise<Subject> {
  return sendJSON<Subject>('POST', '/subjects', input, signal)
}

/** Updates a subject's editable fields and returns the refreshed record. */
export async function updateSubject(
  uid: string,
  input: SubjectInput,
  signal?: AbortSignal,
): Promise<Subject> {
  return sendJSON<Subject>('PATCH', `/subjects/${encodeURIComponent(uid)}`, input, signal)
}

/** Deletes a subject; its markers are detached server-side. */
export async function deleteSubject(uid: string, signal?: AbortSignal): Promise<void> {
  await sendJSON<undefined>('DELETE', `/subjects/${encodeURIComponent(uid)}`, undefined, signal)
}

/**
 * What a merge moved, mirroring `people.MergeResult`. The source subject is gone
 * by the time this arrives — a merge cannot be undone — so these counts (and the
 * audit entry behind them) are the only account of what it did.
 */
export interface MergeResult {
  keeper_uid: string
  source_uid: string
  markers_moved: number
  faces_moved: number
  confirmations_moved: number
  rejections_moved: number
  /** Rejections discarded because the merged person is assigned to that face. */
  rejections_dropped: number
  dismissals_moved: number
  /**
   * Photos that carried a marker of both people. Both markers are kept, so each
   * such photo becomes a repeated-marker group for the review page to settle.
   */
  shared_photos: number
}

/**
 * Merges the subject `sourceUid` into `keeperUid`: everything the source carried
 * moves to the keeper and the source is deleted. Irreversible — the caller must
 * confirm first.
 */
export async function mergeSubject(
  sourceUid: string,
  keeperUid: string,
  signal?: AbortSignal,
): Promise<MergeResult> {
  return sendJSON<MergeResult>(
    'POST',
    `/subjects/${encodeURIComponent(sourceUid)}/merge`,
    { keeper_uid: keeperUid },
    signal,
  )
}

/**
 * Fetches a page of a subject's photos via `GET /subjects/{uid}/photos`. The
 * shape matches the library list so it can drive the same paginated grid hook.
 */
export async function fetchSubjectPhotos(
  uid: string,
  params: PhotoListParams,
  signal?: AbortSignal,
): Promise<PhotoListResponse> {
  const query = buildPhotoQuery(params)
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  return getJSON<PhotoListResponse>(`/subjects/${encodeURIComponent(uid)}/photos${suffix}`, signal)
}

/**
 * The action the UI should take for a detected face, mirroring
 * `facematch.FaceView.Action`: draw and name a new marker, assign the matched
 * marker to a person, clear it, or nothing (already named).
 */
export type FaceAction = 'create_marker' | 'assign_person' | 'unassign_person' | 'already_done'

/**
 * A candidate identity for an unnamed face/cluster (`facematch.Suggestion`):
 * `confidence` is `1 - distance`, so higher is a closer match.
 */
export interface Suggestion {
  subject_uid: string
  subject_name: string
  distance: number
  confidence: number
}

/**
 * A detected face on a photo with its current assignment and suggested identities
 * (`facematch.FaceView`). `bbox` is normalised display-space `[x, y, w, h]`.
 */
export interface FaceView {
  face_index: number
  bbox: Bbox
  det_score: number
  action: FaceAction
  marker_uid?: string
  subject_uid?: string
  subject_name?: string
  iou?: number
  suggestions: Suggestion[]
}

/** Response body of `GET /api/v1/photos/{uid}/faces` (`facematch.FacesResponse`). */
export interface FacesResponse {
  photo_uid: string
  width: number
  height: number
  orientation: number
  faces: FaceView[]
}

/**
 * A face-assignment request (`facematch.AssignRequest`). `create_marker` needs a
 * `bbox` and a subject (by UID or name); `assign_person`/`unassign_person` act on
 * an existing `marker_uid`.
 */
export interface AssignRequest {
  action: FaceAction
  face_index?: number
  marker_uid?: string
  subject_uid?: string
  subject_name?: string
  bbox?: Bbox
}

/** Fetches the faces and identity suggestions for a photo. */
export async function fetchFaces(photoUid: string, signal?: AbortSignal): Promise<FacesResponse> {
  return getJSON<FacesResponse>(`/photos/${encodeURIComponent(photoUid)}/faces`, signal)
}

/**
 * Applies a face-assignment action via `POST /photos/{uid}/faces/assign`. The
 * caller refetches the faces afterwards; the result body is intentionally not
 * modelled here.
 */
export async function assignFace(
  photoUid: string,
  req: AssignRequest,
  signal?: AbortSignal,
): Promise<void> {
  await sendJSON<unknown>(
    'POST',
    `/photos/${encodeURIComponent(photoUid)}/faces/assign`,
    req,
    signal,
  )
}

/** A representative or sample face within a cluster (`cluster.ExampleFace`). */
export interface ExampleFace {
  photo_uid: string
  face_index: number
  bbox: Bbox
  det_score: number
}

/**
 * An unnamed face cluster awaiting a single-tap naming (`cluster.View`). It
 * carries a representative face, a few samples, and an optional nearest-subject
 * suggestion.
 */
export interface ClusterView {
  uid: string
  size: number
  representative: ExampleFace
  examples: ExampleFace[]
  suggestion?: Suggestion
  created_at: string
}

/**
 * One page of `GET /api/v1/faces/clusters` (`cluster.Listing` plus `grouping`).
 *
 * `total` counts the groups that are *ready* — the ones whose cached summary the
 * server has already built — and `pending` the ones it is still preparing in the
 * background. A page load asks the server for whichever pass the library is
 * missing (grouping the unassigned faces of a library that has none, preparing
 * the summaries of the groups that have none), and `grouping` says whether one
 * is queued or running. The three together are what the page says instead of
 * spinning — and, on a library that has never been grouped, instead of claiming
 * there is nothing to see.
 */
export interface ClusterPage {
  clusters: ClusterView[]
  total: number
  pending: number
  grouping: boolean
  limit: number
  offset: number
  next_offset: number | null
}

/** Query parameters for {@link fetchClusters}. */
export interface ClusterPageParams {
  limit?: number
  offset?: number
}

/** A cluster-naming request: assign by existing subject UID or by name. */
export interface ClusterAssignRequest {
  subject_uid?: string
  subject_name?: string
}

/** Request body for detaching a stray face from a cluster. */
export interface RemoveFaceRequest {
  photo_uid: string
  face_index: number
}

/** Response body of `POST /faces/clusters/{id}/remove-face`. */
interface RemoveFaceResponse {
  cluster: ClusterView | null
}

/**
 * Fetches one page of the unnamed face clusters awaiting review, newest first.
 * The page is bounded on purpose: a real library holds far more groups than a
 * reader looks at, and the server prepares them in the background rather than
 * rebuilding every group's view for every visit.
 */
export async function fetchClusters(
  params: ClusterPageParams = {},
  signal?: AbortSignal,
): Promise<ClusterPage> {
  const query = new URLSearchParams()
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  if (params.offset !== undefined && params.offset > 0) {
    query.set('offset', String(params.offset))
  }
  const search = query.toString()
  return getJSON<ClusterPage>(`/faces/clusters${search === '' ? '' : `?${search}`}`, signal)
}

/**
 * Names an entire cluster, assigning every face to one subject (found or created
 * by name). The cluster is consumed server-side on success.
 */
export async function assignCluster(
  clusterUid: string,
  req: ClusterAssignRequest,
  signal?: AbortSignal,
): Promise<void> {
  await sendJSON<unknown>(
    'POST',
    `/faces/clusters/${encodeURIComponent(clusterUid)}/assign`,
    req,
    signal,
  )
}

/**
 * Detaches a stray face from a cluster before naming it, returning the refreshed
 * cluster, or `null` when the removal emptied it.
 */
export async function removeClusterFace(
  clusterUid: string,
  req: RemoveFaceRequest,
  signal?: AbortSignal,
): Promise<ClusterView | null> {
  const body = await sendJSON<RemoveFaceResponse>(
    'POST',
    `/faces/clusters/${encodeURIComponent(clusterUid)}/remove-face`,
    req,
    signal,
  )
  return body.cluster
}

/**
 * A suspected mis-assigned face within a subject (`outliers.OutlierFace`),
 * ranked by cosine `distance` from the subject's embedding centroid.
 */
export interface OutlierFace {
  photo_uid: string
  face_index: number
  bbox: Bbox
  det_score: number
  distance: number
  marker_uid?: string
  width: number
  height: number
  orientation: number
}

/**
 * Response body of `GET /api/v1/subjects/{uid}/outliers`. `meaningful` is false
 * when too few faces exist to single any out (the faces are still returned,
 * ranked). `count` and `avg_distance` describe the full scored set even when a
 * threshold/limit narrows `faces`; `no_embedding` is how many of the subject's
 * assignments have no embedding and cannot be checked at all.
 */
export interface OutlierResult {
  subject_uid: string
  count: number
  meaningful: boolean
  avg_distance: number
  no_embedding: number
  faces: OutlierFace[]
}

/** Optional narrowing of an outlier query; omitted values mean "everything". */
export interface OutlierParams {
  /** Minimum cosine distance from the centroid (0 = return everything). */
  threshold?: number
  /** Maximum number of faces returned (0 = all). */
  limit?: number
}

/** Fetches a subject's faces ranked most-suspicious first, optionally narrowed. */
export async function fetchOutliers(
  subjectUid: string,
  params?: OutlierParams,
  signal?: AbortSignal,
): Promise<OutlierResult> {
  const query = new URLSearchParams()
  if (params?.threshold !== undefined && params.threshold > 0) {
    query.set('threshold', String(params.threshold))
  }
  if (params?.limit !== undefined && params.limit > 0) {
    query.set('limit', String(params.limit))
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : ''
  return getJSON<OutlierResult>(
    `/subjects/${encodeURIComponent(subjectUid)}/outliers${suffix}`,
    signal,
  )
}

/** Re-export so people views can render photos without importing two modules. */
export type { Photo }
