import { ApiError } from './auth'

/**
 * Tasks client, mirroring `internal/phototask` and `internal/phototaskapi`.
 *
 * A task is a question about a frozen group of photographs: what is being asked,
 * which pictures it is about, whose move it is, and — through the comment thread —
 * how it was settled. It exists because curating an inherited library arrives in
 * batches that end at something only a person can answer.
 *
 * Two properties of the backend shape this client:
 *
 *   - **The group is frozen, not a query.** A task keeps the search that produced
 *     it ({@link Task.query}) as evidence, but the server never re-runs it: the
 *     task is answered by fixing the data, which a live query would then empty.
 *     The photographs themselves are read through the catalogue with
 *     `GET /photos?task=…`, not from a route here.
 *   - **Answering is open to every signed-in role, curating is not.** Reading a
 *     task and writing in its thread are guarded by `RequireAuth`; opening,
 *     editing and closing need write access. So a page must gate its editing
 *     controls on `canWrite` but never its composer.
 *
 * Every call throws {@link ApiError} on a non-OK response so callers can branch on
 * `status`: 403 (a viewer trying to curate), 404 (deleted since) and 400 (a state
 * that does not exist, or closing without saying how it ended) all read differently.
 */

const API_BASE = '/api/v1'

/** Whose move it is. The last two are the closed states, and both are results. */
export type TaskState = 'question' | 'working' | 'review' | 'done' | 'rejected'

/** Every state, in workflow order — the order a filter row offers them in. */
export const TASK_STATES: readonly TaskState[] = [
  'question',
  'working',
  'review',
  'done',
  'rejected',
]

/** The states a live task can be in: the default listing. */
export const OPEN_TASK_STATES: readonly TaskState[] = ['question', 'working', 'review']

/** Whether a state ends the task. A closed task always carries a resolution. */
export function isClosedState(state: TaskState): boolean {
  return state === 'done' || state === 'rejected'
}

/** One task as read back from the API (`phototask.Task`). */
export interface Task {
  uid: string
  /** The question, in one line — the page heading and the text of the link. */
  title: string
  /** The context, in Markdown. Rendered through the sanitising renderer. */
  body: string
  state: TaskState
  /** How it ended; empty while open. */
  resolution: string
  /** The search that produced the group, kept as evidence and never re-run. */
  query: string
  /** Empty for a task whose author's account has since been deleted. */
  created_by: string
  created_by_name: string
  created_at: string
  updated_at: string
  /** When the state last moved — what `has_new_answer` is measured against. */
  state_at: string
  closed_at?: string
  closed_by?: string
  closed_by_name?: string
  photo_count: number
  /** The first photograph added, for the listing's thumbnail. */
  cover_photo_uid?: string
  comment_count: number
  last_comment_at?: string
  /** Somebody has written in the thread since the state last moved. */
  has_new_answer: boolean
}

/** The compact form of a task, as the photo detail carries it (`phototask.Ref`). */
export interface TaskRef {
  uid: string
  title: string
  state: TaskState
}

/** Response body of `GET /api/v1/tasks`. */
export interface TaskPage {
  tasks: Task[]
  /** How many match before paging, so a listing can say "1-25 of 63". */
  total: number
  limit: number
  offset: number
}

/** Query parameters of the listing. */
export interface TaskListParams {
  states?: readonly TaskState[]
  /** The shorthand for the three open states; ignored when `states` is given. */
  open?: boolean
  /** Only tasks answered since the state last moved. */
  answered?: boolean
  /** Match a substring of the question or its context. */
  q?: string
  /** Only tasks this photograph is part of. */
  photo?: string
  limit?: number
  offset?: number
}

/** Body of `POST /api/v1/tasks`. */
export interface TaskInput {
  title: string
  body?: string
  query?: string
  state?: TaskState
  photo_uids?: string[]
}

/**
 * Body of `PATCH /api/v1/tasks/{uid}`. An absent field is left alone and an
 * explicit empty string clears it, which is how a resolution is removed.
 */
export interface TaskEdit {
  title?: string
  body?: string
  query?: string
  state?: TaskState
  resolution?: string
}

/** Response body of the two membership endpoints. */
export interface TaskMembership {
  changed: number
  task: Task
}

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

/** Issues a request against the tasks API, throwing ApiError on a non-OK status. */
async function send<T>(
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

/** Builds the endpoint of one task, escaping the uid. */
function taskPath(uid: string): string {
  return `/tasks/${encodeURIComponent(uid)}`
}

/** Renders the listing parameters, omitting everything left at its default. */
function listQuery(params: TaskListParams): string {
  const search = new URLSearchParams()
  if (params.states !== undefined && params.states.length > 0) {
    search.set('state', params.states.join(','))
  } else if (params.open === true) {
    search.set('open', 'true')
  }
  if (params.answered === true) {
    search.set('answered', 'true')
  }
  if (params.q !== undefined && params.q !== '') {
    search.set('q', params.q)
  }
  if (params.photo !== undefined && params.photo !== '') {
    search.set('photo', params.photo)
  }
  if (params.limit !== undefined && params.limit > 0) {
    search.set('limit', String(params.limit))
  }
  if (params.offset !== undefined && params.offset > 0) {
    search.set('offset', String(params.offset))
  }
  const query = search.toString()
  return query === '' ? '' : `?${query}`
}

/**
 * Lists tasks: open ones first, the most recently touched at the top, where a
 * reply counts as a touch.
 */
export async function fetchTasks(
  params: TaskListParams = {},
  signal?: AbortSignal,
): Promise<TaskPage> {
  return send<TaskPage>('GET', `/tasks${listQuery(params)}`, undefined, signal)
}

/** Reads one task. @throws ApiError 404 when it has been deleted since. */
export async function fetchTask(uid: string, signal?: AbortSignal): Promise<Task> {
  return send<Task>('GET', taskPath(uid), undefined, signal)
}

/**
 * Opens a task over a group of photographs.
 *
 * @throws ApiError 400 (no question, or a closed state with no resolution), 403
 *   (the caller may not curate) or 404 (one of the photographs is gone).
 */
export async function createTask(input: TaskInput, signal?: AbortSignal): Promise<Task> {
  return send<Task>('POST', '/tasks', input, signal)
}

/**
 * Folds a partial change onto a task. Advancing the state is an ordinary edit;
 * closing one needs a resolution.
 *
 * @throws ApiError 400 (an unknown state, or closing without saying how it
 *   ended), 403 or 404.
 */
export async function updateTask(uid: string, edit: TaskEdit, signal?: AbortSignal): Promise<Task> {
  return send<Task>('PATCH', taskPath(uid), edit, signal)
}

/**
 * Deletes a task, its membership and its thread. The photographs survive.
 *
 * Finished work is normally closed rather than deleted: the frozen group is the
 * record of what a batch of edits touched.
 */
export async function deleteTask(uid: string, signal?: AbortSignal): Promise<void> {
  await send<undefined>('DELETE', taskPath(uid), undefined, signal)
}

/** Adds photographs to a task's group, ignoring the ones already in it. */
export async function addTaskPhotos(
  uid: string,
  photoUids: string[],
  signal?: AbortSignal,
): Promise<TaskMembership> {
  return send<TaskMembership>('POST', `${taskPath(uid)}/photos`, { photo_uids: photoUids }, signal)
}

/** Drops photographs from a task's group, ignoring the ones not in it. */
export async function removeTaskPhotos(
  uid: string,
  photoUids: string[],
  signal?: AbortSignal,
): Promise<TaskMembership> {
  return send<TaskMembership>(
    'DELETE',
    `${taskPath(uid)}/photos`,
    { photo_uids: photoUids },
    signal,
  )
}
