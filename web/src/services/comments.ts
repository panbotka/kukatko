import { notifyTaskChanged } from '../lib/taskChanges'

import { ApiError } from './auth'

/**
 * Comments client, mirroring the backend JSON shapes of `internal/comments` and
 * the comment routes of `internal/photoapi` and `internal/phototaskapi`. A comment
 * is one short plain-text note by one user on one subject: a photograph — the
 * family conversation around a picture — or a task, where it is the answer to the
 * question the task asks.
 *
 * Which of the two a thread belongs to is a {@link CommentSubject}; everything
 * below it is identical, because server-side it is one table and one set of rules.
 *
 * Two properties of the backend shape the whole client:
 *
 *   - **Bodies are plain text.** Nothing is parsed, rendered or sanitised
 *     server-side, so whatever displays a body must escape it (React does, as long
 *     as nobody reaches for `dangerouslySetInnerHTML`).
 *   - **Writing is open to every signed-in role, viewers included.** The create
 *     route is guarded by `RequireAuth`, not `RequireWrite` — the one documented
 *     exception to the read-only rule — so this client never gates on `canWrite`.
 *
 * Every call throws {@link ApiError} on a non-OK response so callers can branch on
 * `status`: 403 (someone else's comment), 404 (already deleted), 429 (the per-user
 * rate limit) and 400 (empty or over-long body) all mean different things to a reader.
 */

const API_BASE = '/api/v1'

/**
 * The longest comment body the backend accepts, in characters (runes, not bytes),
 * mirroring `comments.MaxBodyLen`. The composer caps its input at the same number
 * so an over-long body is prevented rather than rejected after the fact.
 */
export const MAX_COMMENT_LENGTH = 2000

/** What a comment thread hangs off: a photograph, or a task. */
export type CommentSubjectKind = 'photo' | 'task'

/** The one thing a thread is about — the kind, and the uid of that row. */
export interface CommentSubject {
  kind: CommentSubjectKind
  uid: string
}

/** Names the thread of a photograph. */
export function photoSubject(uid: string): CommentSubject {
  return { kind: 'photo', uid }
}

/** Names the thread of a task. */
export function taskSubject(uid: string): CommentSubject {
  return { kind: 'task', uid }
}

/**
 * One stored comment as read back from the API (`comments.Comment`).
 *
 * Exactly one of `photo_uid` and `task_uid` is set, matching the row's one subject.
 *
 * `author_uid` and `author_name` are **empty strings** for a comment whose author's
 * account has since been deleted: the row survives authorless, and nobody may edit
 * it any more. Renderers must therefore not assume a name is present.
 */
export interface Comment {
  uid: string
  /** The photograph this comment is on; absent on a task thread. */
  photo_uid?: string
  /** The task this comment is on; absent on a photo thread. */
  task_uid?: string
  author_uid: string
  /** The author's display name (falling back to the username), resolved server-side. */
  author_name: string
  /**
   * The cover photo of the person the author's account says it is. It predates
   * profile pictures and the thread no longer draws it: `PersonAvatar` asks
   * `GET /users/{uid}/avatar` with `author_uid`, which resolves the whole chain
   * (an uploaded picture, a picked photo, the linked person's face) rather than
   * this one link of it.
   */
  author_photo_uid?: string
  body: string
  created_at: string
  /** Set once the author has rewritten the body; absent on a never-edited comment. */
  edited_at?: string
}

/** Response body of the thread endpoint of either subject. */
interface CommentListResponse {
  comments: Comment[]
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

/** Issues a request against the comments API, throwing ApiError on a non-OK status. */
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

/**
 * Builds the thread path for a subject, escaping the uid. The two collections are
 * the only difference between a photo thread and a task thread.
 */
function threadPath(subject: CommentSubject): string {
  const collection = subject.kind === 'task' ? 'tasks' : 'photos'
  return `/${collection}/${encodeURIComponent(subject.uid)}/comments`
}

/**
 * Lists a subject's comments, **oldest first** — a conversation reads forwards.
 *
 * A subject with no comments (and, deliberately, one that does not exist) yields
 * an empty array rather than a 404, so an empty thread is a normal result.
 */
export async function fetchComments(
  subject: CommentSubject,
  signal?: AbortSignal,
): Promise<Comment[]> {
  const body = await send<CommentListResponse>('GET', threadPath(subject), undefined, signal)
  return body.comments
}

/**
 * Appends a comment to a subject's thread and returns the created record.
 *
 * @throws ApiError 400 (blank or over-long body), 404 (no such photo) or 429 (the
 *   per-user rate limit — the caller should say "slow down", not "it failed").
 */
export async function createComment(
  subject: CommentSubject,
  body: string,
  signal?: AbortSignal,
): Promise<Comment> {
  const created = await send<Comment>('POST', threadPath(subject), { body }, signal)
  announceTaskWrite(subject)
  return created
}

/**
 * Tells the task-change bus a task's thread was written to, so the counts
 * behind the navigation badge ("whose move is it") refresh at once. A photo's
 * thread is not the queue's business and stays silent.
 */
function announceTaskWrite(subject: CommentSubject): void {
  if (subject.kind === 'task') {
    notifyTaskChanged()
  }
}

/**
 * Rewrites the body of the caller's own comment and returns the edited record,
 * now carrying `edited_at`.
 *
 * @throws ApiError 403 (someone else's comment — admins included: an admin may
 *   remove a comment but never rewrite what someone is recorded as having said) or
 *   404 (already deleted, or addressed through the wrong subject).
 */
export async function updateComment(
  subject: CommentSubject,
  commentUid: string,
  body: string,
  signal?: AbortSignal,
): Promise<Comment> {
  const edited = await send<Comment>(
    'PATCH',
    `${threadPath(subject)}/${encodeURIComponent(commentUid)}`,
    { body },
    signal,
  )
  announceTaskWrite(subject)
  return edited
}

/**
 * Deletes a comment — the author's own, or (for an admin) anyone's. The delete is
 * soft server-side, so the comment simply drops out of every read.
 *
 * @throws ApiError 403 (not the author and not an admin) or 404 (already deleted).
 */
export async function deleteComment(
  subject: CommentSubject,
  commentUid: string,
  signal?: AbortSignal,
): Promise<void> {
  await send<undefined>(
    'DELETE',
    `${threadPath(subject)}/${encodeURIComponent(commentUid)}`,
    undefined,
    signal,
  )
  announceTaskWrite(subject)
}
