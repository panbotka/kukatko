import { notifyTaskChanged } from '../lib/taskChanges'

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
  /**
   * The answers the question offers, in order — at most {@link MAX_TASK_OPTIONS},
   * each short and distinct. Choosing one posts an ordinary comment whose body is
   * the option text verbatim ({@link answerOptions}). Always present; empty means
   * the question is answered in free text.
   */
  options: string[]
  /** Empty for a task whose author's account has since been deleted. */
  created_by: string
  created_by_name: string
  created_at: string
  updated_at: string
  /** When the state last moved — what `has_new_answer` is measured against. */
  state_at: string
  /**
   * Who last moved the state. Absent on a task from before it was recorded,
   * which the server reads as the creator.
   */
  state_by?: string
  closed_at?: string
  closed_by?: string
  closed_by_name?: string
  photo_count: number
  /** The first photograph added, for the listing's thumbnail. */
  cover_photo_uid?: string
  comment_count: number
  last_comment_at?: string
  /**
   * Somebody **other than the reader** has written in the thread since the
   * state last moved. Relative to the signed-in caller: one's own reply never
   * lights it, and a soft-deleted comment never counts.
   */
  has_new_answer: boolean
  /**
   * Who acted last and when: the newest of the last state change, the opening
   * and the newest live comment. Photo and participant changes do not count.
   * The uid is empty when the actor's account is gone.
   */
  last_activity_at: string
  last_activity_by: string
  last_activity_by_name: string
  /**
   * The move is the reader's: the task is open, the reader is on it, and the
   * last activity was somebody else's. Always false for a closed task and for
   * a reader who is not on it.
   */
  waiting_on_me: boolean
  /**
   * The people on this task, oldest membership first. Always present (possibly
   * empty) on every read, listing included.
   */
  participants: Participant[]
}

/**
 * One person on a task (`phototask.Participant`).
 *
 * Most participation is a side effect rather than a decision: opening a task,
 * answering it or moving it along joins its actor. {@link added_by} is what
 * tells the two apart — absent means "joined by acting", set means "was asked,
 * by this person" — which is the difference between "Anna is on this" and "you
 * put Anna on this".
 */
export interface Participant {
  user_uid: string
  /** The display name, falling back to the username. */
  name: string
  joined_at: string
  /** Absent for somebody who joined by acting on the task. */
  added_by?: string
  added_by_name?: string
}

/** Response body of the participant endpoints. */
export interface ParticipantList {
  participants: Participant[]
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

/**
 * The queue at a glance, as returned by `GET /api/v1/tasks/summary`
 * (`phototask.Summary`): how many tasks stand in each state, how many are open,
 * and — relative to the signed-in caller — how many wait on them and how many
 * have been answered. `by_state` always names every state, zero included.
 * `waiting_on_me` is the number on the navigation badge; `answered` is the open
 * tasks somebody else replied to since the state last moved, i.e. what the
 * `answered` filter over the default listing returns.
 */
export interface TaskSummary {
  by_state: Record<TaskState, number>
  open: number
  waiting_on_me: number
  answered: number
}

/** Query parameters of the listing. */
export interface TaskListParams {
  states?: readonly TaskState[]
  /** The shorthand for the three open states; ignored when `states` is given. */
  open?: boolean
  /** Only tasks somebody else answered since the state last moved. */
  answered?: boolean
  /** Only tasks whose move is the caller's (`waiting_on_me`), evaluated server-side. */
  waiting?: boolean
  /** Match a substring of the question or its context. */
  q?: string
  /** Only tasks this photograph is part of. */
  photo?: string
  /**
   * Only tasks this person is on. The literal `me` means the signed-in caller,
   * which is how the page asks "what am I involved in?" without first having to
   * learn its own uid.
   */
  participant?: string
  limit?: number
  offset?: number
}

/** Body of `POST /api/v1/tasks`. */
export interface TaskInput {
  title: string
  body?: string
  query?: string
  state?: TaskState
  /**
   * The frozen group. Absent or empty opens a task about the library rather
   * than about particular photographs; they can be added later.
   */
  photo_uids?: string[]
  /** The answers to offer as buttons; see {@link Task.options}. */
  options?: string[]
  /**
   * The people asked at the outset (user uids): each is put on the task as
   * asked by the creator, exactly as assigning them afterwards would.
   */
  participants?: string[]
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
  /** Replaces the whole set of answers; an empty array clears it. */
  options?: string[]
}

/** How many answer options a question may offer (`phototask.MaxOptions`). */
export const MAX_TASK_OPTIONS = 5

/** The longest answer option, in characters (`phototask.MaxOptionLen`). */
export const MAX_TASK_OPTION_LENGTH = 60

/**
 * The two built-in answers of a `review` task that offers no options of its own
 * (`phototask.ReviewApprove` / `phototask.ReviewReturn`). The page labels them in
 * the reader's language but **posts exactly these strings**, whatever the UI
 * language: the agent that opened the review matches the answer against them,
 * and it cannot know which language the person read the page in.
 */
export const REVIEW_APPROVE = 'Schvaluji.'
export const REVIEW_RETURN = 'Vrátit k přepracování.'

/** One answer a task page offers as a button. */
export interface AnswerOption {
  /** The comment body a tap posts, verbatim. */
  text: string
  /**
   * Set for the two built-in review answers, whose button label is localised
   * while {@link text} stays fixed. Absent for an option the task carries itself,
   * whose text is its own label.
   */
  builtin?: 'approve' | 'sendBack'
}

/**
 * The answers a task offers: its own options when it has any, otherwise — for a
 * task waiting in `review` — the built-in approve/return pair, otherwise none.
 * A task's own options always win, so an agent that asks a review question with
 * a specific choice ("1936 or 1938?") is not answered with a yes.
 */
export function answerOptions(task: Pick<Task, 'state' | 'options'>): AnswerOption[] {
  if (task.options.length > 0) {
    return task.options.map((text) => ({ text }))
  }
  if (task.state === 'review') {
    return [
      { text: REVIEW_APPROVE, builtin: 'approve' },
      { text: REVIEW_RETURN, builtin: 'sendBack' },
    ]
  }
  return []
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

/**
 * Issues a request against the tasks API, throwing ApiError on a non-OK status.
 * Every successful write announces itself on the task-change bus, so the counts
 * behind the navigation badge refresh at once rather than on their next poll.
 */
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
  if (method !== 'GET') {
    notifyTaskChanged()
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
  if (params.waiting === true) {
    search.set('waiting', 'true')
  }
  if (params.q !== undefined && params.q !== '') {
    search.set('q', params.q)
  }
  if (params.photo !== undefined && params.photo !== '') {
    search.set('photo', params.photo)
  }
  if (params.participant !== undefined && params.participant !== '') {
    search.set('participant', params.participant)
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

/**
 * Reads the queue's counts as the caller sees them. Cheap — one query server
 * side — and read by every signed-in role, so the shell may poll it.
 */
export async function fetchTaskSummary(signal?: AbortSignal): Promise<TaskSummary> {
  return send<TaskSummary>('GET', '/tasks/summary', undefined, signal)
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

/** Reads the people on a task, oldest membership first. */
export async function fetchTaskParticipants(
  uid: string,
  signal?: AbortSignal,
): Promise<Participant[]> {
  const body = await send<ParticipantList>(
    'GET',
    `${taskPath(uid)}/participants`,
    undefined,
    signal,
  )
  return body.participants
}

/**
 * Puts somebody on a task — asking a particular person, before they have done
 * anything about it. Answers with the participants as they now stand, so the
 * caller never has to follow the write with a read.
 *
 * @throws ApiError 403 (a viewer: naming somebody else is curation) or 404 (the
 *   task is gone, or the uid names no account).
 */
export async function assignTaskParticipant(
  uid: string,
  userUid: string,
  signal?: AbortSignal,
): Promise<Participant[]> {
  const body = await send<ParticipantList>(
    'POST',
    `${taskPath(uid)}/participants`,
    { user_uid: userUid },
    signal,
  )
  return body.participants
}

/**
 * Takes somebody off a task. Somebody who was not on it is not an error — the
 * end state is the same either way, and what they did stays in the audit trail.
 */
export async function unassignTaskParticipant(
  uid: string,
  userUid: string,
  signal?: AbortSignal,
): Promise<Participant[]> {
  const body = await send<ParticipantList>(
    'DELETE',
    `${taskPath(uid)}/participants/${encodeURIComponent(userUid)}`,
    undefined,
    signal,
  )
  return body.participants
}
