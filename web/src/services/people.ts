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
  created_at: string
  updated_at: string
}

/** A subject paired with its non-invalid marker count (`people.SubjectCount`). */
export interface SubjectCount extends Subject {
  marker_count: number
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
}

/** Lists every subject with its photo (marker) count, ordered by name. */
export async function fetchSubjects(signal?: AbortSignal): Promise<SubjectCount[]> {
  const body = await getJSON<SubjectsResponse>('/subjects', signal)
  return body.subjects
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

/** Response body of `GET /api/v1/faces/clusters`. */
interface ClustersResponse {
  clusters: ClusterView[]
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

/** Lists the unnamed face clusters awaiting review, largest impact first. */
export async function fetchClusters(signal?: AbortSignal): Promise<ClusterView[]> {
  const body = await getJSON<ClustersResponse>('/faces/clusters', signal)
  return body.clusters
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
