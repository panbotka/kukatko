import { useCallback, useEffect, useRef, useState } from 'react'

import {
  createComment,
  deleteComment,
  fetchComments,
  type PhotoComment,
  updateComment,
} from '../services/comments'

/** Fetch lifecycle of one photo's thread. */
export type CommentsStatus = 'loading' | 'ready' | 'error'

/** Why a write failed, in the terms a reader cares about. */
export type CommentFailure =
  /** The per-user rate limit (429) — "you are going too fast", not "it broke". */
  | 'throttled'
  /** The comment is gone (404) or not the caller's to touch (403). */
  | 'forbidden'
  /** Anything else: network, 5xx, a rejected body. */
  | 'failed'

/** What {@link useComments} exposes to a thread view. */
export interface UseCommentsResult {
  status: CommentsStatus
  /** The live thread, oldest first. Empty while loading and after a failure. */
  comments: PhotoComment[]
  /** The thread's length — the number the count badge shows. */
  count: number
  /** True while a create/edit/delete is in flight, so the controls can stand down. */
  busy: boolean
  /** The last write failure, or null. Cleared when the next write starts. */
  failure: CommentFailure | null
  /** Posts a new comment; resolves true when it landed. */
  post: (body: string) => Promise<boolean>
  /** Rewrites one of the caller's own comments; resolves true when it landed. */
  edit: (uid: string, body: string) => Promise<boolean>
  /** Removes a comment (own, or anyone's for an admin); resolves true when it landed. */
  remove: (uid: string) => Promise<boolean>
}

/** Options for {@link useComments}. */
export interface UseCommentsOptions {
  /**
   * Called with the thread's length whenever it is known or changes — on load and
   * after every successful write. It is what keeps the count badge in the viewer
   * chrome truthful without refetching the whole photo detail.
   */
  onCountChange?: (count: number) => void
}

/** Maps a thrown API error onto the failure the reader is shown. */
function classify(err: unknown): CommentFailure {
  const status = (err as { status?: number }).status
  if (status === 429) {
    return 'throttled'
  }
  if (status === 403 || status === 404) {
    return 'forbidden'
  }
  return 'failed'
}

/**
 * The photo's comment thread as state: loads it, and applies posts, edits and
 * deletes to the local list so the conversation updates in place.
 *
 * Every write reuses the record the backend returns rather than a guess assembled
 * in the browser — the server owns the uid, the timestamps and the author name, and
 * a locally invented "optimistic" comment would show the wrong ones until the next
 * reload. The trade is one round-trip of latency on a control that is already
 * disabled while it runs.
 *
 * The thread aborts its fetch on unmount / uid change, so paging quickly through
 * photos cannot land an earlier photo's comments in a later photo's panel.
 */
export function useComments(photoUid: string, options: UseCommentsOptions = {}): UseCommentsResult {
  const [status, setStatus] = useState<CommentsStatus>('loading')
  const [comments, setComments] = useState<PhotoComment[]>([])
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<CommentFailure | null>(null)
  // Held in a ref so a caller may pass an inline arrow without re-running the load
  // effect on every render of the page above.
  const onCountChange = useRef(options.onCountChange)
  onCountChange.current = options.onCountChange

  // Reports a new thread length upwards, from the one place the list is replaced.
  const publish = useCallback((next: PhotoComment[]): void => {
    setComments(next)
    onCountChange.current?.(next.length)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setStatus('loading')
    fetchComments(photoUid, controller.signal)
      .then((list) => {
        if (!controller.signal.aborted) {
          publish(list)
          setStatus('ready')
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setComments([])
          setStatus('error')
        }
      })
    return () => {
      controller.abort()
    }
  }, [photoUid, publish])

  // The shared shape of every write: stand the controls down, run it, and either
  // apply the server's answer to the list or report why it did not happen.
  const run = useCallback(
    async (apply: () => Promise<PhotoComment[]>): Promise<boolean> => {
      setBusy(true)
      setFailure(null)
      try {
        publish(await apply())
        return true
      } catch (err) {
        setFailure(classify(err))
        return false
      } finally {
        setBusy(false)
      }
    },
    [publish],
  )

  const post = useCallback(
    (body: string): Promise<boolean> =>
      run(async () => {
        const created = await createComment(photoUid, body)
        // Appended, not prepended: the thread reads oldest first, like a conversation.
        return [...comments, created]
      }),
    [comments, photoUid, run],
  )

  const edit = useCallback(
    (uid: string, body: string): Promise<boolean> =>
      run(async () => {
        const edited = await updateComment(photoUid, uid, body)
        return comments.map((item) => (item.uid === uid ? edited : item))
      }),
    [comments, photoUid, run],
  )

  const remove = useCallback(
    (uid: string): Promise<boolean> =>
      run(async () => {
        await deleteComment(photoUid, uid)
        return comments.filter((item) => item.uid !== uid)
      }),
    [comments, photoUid, run],
  )

  return { status, comments, count: comments.length, busy, failure, post, edit, remove }
}
