import { ApiError } from './auth'

/**
 * Photo-comments client, mirroring the backend JSON shapes of `internal/comments`
 * and the comment routes of `internal/photoapi`. A comment is one short plain-text
 * note by one user on one photo — the family conversation around a picture.
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

/**
 * One stored comment as read back from the API (`comments.Comment`).
 *
 * `author_uid` and `author_name` are **empty strings** for a comment whose author's
 * account has since been deleted: the row survives authorless, and nobody may edit
 * it any more. Renderers must therefore not assume a name is present.
 */
export interface PhotoComment {
  uid: string
  photo_uid: string
  author_uid: string
  /** The author's display name (falling back to the username), resolved server-side. */
  author_name: string
  body: string
  created_at: string
  /** Set once the author has rewritten the body; absent on a never-edited comment. */
  edited_at?: string
}

/** Response body of `GET /api/v1/photos/{uid}/comments`. */
interface CommentListResponse {
  comments: PhotoComment[]
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

/** Builds the thread path for a photo, escaping the uid. */
function threadPath(photoUid: string): string {
  return `/photos/${encodeURIComponent(photoUid)}/comments`
}

/**
 * Lists a photo's comments, **oldest first** — a conversation reads forwards.
 *
 * A photo with no comments (and, deliberately, a photo that does not exist) yields
 * an empty array rather than a 404, so an empty thread is a normal result.
 */
export async function fetchComments(
  photoUid: string,
  signal?: AbortSignal,
): Promise<PhotoComment[]> {
  const body = await send<CommentListResponse>('GET', threadPath(photoUid), undefined, signal)
  return body.comments
}

/**
 * Appends a comment to a photo's thread and returns the created record.
 *
 * @throws ApiError 400 (blank or over-long body), 404 (no such photo) or 429 (the
 *   per-user rate limit — the caller should say "slow down", not "it failed").
 */
export async function createComment(
  photoUid: string,
  body: string,
  signal?: AbortSignal,
): Promise<PhotoComment> {
  return send<PhotoComment>('POST', threadPath(photoUid), { body }, signal)
}

/**
 * Rewrites the body of the caller's own comment and returns the edited record,
 * now carrying `edited_at`.
 *
 * @throws ApiError 403 (someone else's comment — admins included: an admin may
 *   remove a comment but never rewrite what someone is recorded as having said) or
 *   404 (already deleted, or addressed through the wrong photo).
 */
export async function updateComment(
  photoUid: string,
  commentUid: string,
  body: string,
  signal?: AbortSignal,
): Promise<PhotoComment> {
  return send<PhotoComment>(
    'PATCH',
    `${threadPath(photoUid)}/${encodeURIComponent(commentUid)}`,
    { body },
    signal,
  )
}

/**
 * Deletes a comment — the author's own, or (for an admin) anyone's. The delete is
 * soft server-side, so the comment simply drops out of every read.
 *
 * @throws ApiError 403 (not the author and not an admin) or 404 (already deleted).
 */
export async function deleteComment(
  photoUid: string,
  commentUid: string,
  signal?: AbortSignal,
): Promise<void> {
  await send<undefined>(
    'DELETE',
    `${threadPath(photoUid)}/${encodeURIComponent(commentUid)}`,
    undefined,
    signal,
  )
}
