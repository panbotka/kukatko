import { useCallback, useEffect, useRef, useState } from 'react'

import { rejectFace, rejectLabel, unrejectFace, unrejectLabel } from '../services/feedback'
import { attachLabel, detachLabel } from '../services/organize'
import { assignFace, fetchFaces } from '../services/people'
import {
  answerReview,
  fetchReviewQueue,
  type ReviewAnswer,
  type ReviewQuestion,
} from '../services/review'

/**
 * When the local queue shrinks to this many cards a background refill starts,
 * so the next batch is in memory before the player reaches the boundary.
 */
const REFILL_AT = 3

/** One answer given this session; the undo target. */
export interface AnsweredQuestion {
  question: ReviewQuestion
  answer: ReviewAnswer
}

/** An optimistic answer whose request failed; held for an explicit retry. */
export type FailedAnswer = AnsweredQuestion

/** Everything {@link useReviewGame} exposes to the page. */
export interface ReviewGame {
  /** The question on screen; `undefined` while loading or when the queue is dry. */
  current: ReviewQuestion | undefined
  /** The local queue (current first) — the page prefetches these images. */
  pending: readonly ReviewQuestion[]
  /** Session counter of yes/no answers (skips don't count, undo subtracts). */
  answered: number
  /** Rough server estimate of candidates still queued. */
  remaining: number
  /** True while a batch fetch is in flight. */
  fetching: boolean
  /** True when the last queue fetch failed (offline, server error). */
  loadError: boolean
  /** True when the server has nothing new to ask. */
  exhausted: boolean
  /** Why the queue is empty: `no_people_no_labels` / `no_candidates`. */
  reason: string | undefined
  /** Answers whose requests failed, awaiting retry or dismissal. */
  failed: readonly FailedAnswer[]
  /** The most recent answer, while it is still undoable. */
  lastAnswer: AnsweredQuestion | null
  /** True while an undo is talking to the server. */
  undoing: boolean
  /** True when the last undo attempt failed. */
  undoError: boolean
  /** Answers the current question and advances immediately (optimistic). */
  answer: (verdict: ReviewAnswer) => void
  /** Reverts the last answer through the matching inverse endpoint. */
  undo: () => void
  /** Clears the load-error/exhausted latches so the queue is fetched again. */
  retryLoad: () => void
  /** Re-sends every failed answer. */
  retryFailed: () => void
  /** Drops the failed answers without re-sending them. */
  dismissFailed: () => void
}

/**
 * The review-game engine: a local question queue refilled in the background,
 * optimistic answers, and a one-step undo.
 *
 * Answering advances the UI immediately and settles the request behind the
 * player's back; a failure is held in `failed` for an explicit retry rather
 * than blocking the flow or silently losing the verdict. The queue refills
 * itself when it runs low, deduplicating against every question already seen
 * this session, so the batch boundary is invisible.
 *
 * Undo goes through the *inverse* write paths (`unassign_person`, the
 * feedback-rejection DELETEs, label detach) because `POST /review/answer` is
 * idempotent per question. For the same reason a re-answer of an undone
 * question cannot go through the review endpoint (it would no-op as
 * `already_answered`), so undone questions are marked and their next yes/no is
 * sent through the direct write paths the backend itself reuses. A yes answered
 * as `create_marker` is undone by first looking the new marker up via
 * `GET /photos/{uid}/faces` — unassigning keeps the marker, so any later
 * re-yes is an `assign_person` to that same marker, never a duplicate.
 */
export function useReviewGame(): ReviewGame {
  const [queue, setQueue] = useState<ReviewQuestion[]>([])
  const [answered, setAnswered] = useState(0)
  const [remaining, setRemaining] = useState(0)
  const [fetching, setFetching] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [exhausted, setExhausted] = useState(false)
  const [reason, setReason] = useState<string | undefined>(undefined)
  const [failed, setFailed] = useState<FailedAnswer[]>([])
  const [lastAnswer, setLastAnswer] = useState<AnsweredQuestion | null>(null)
  const [undoing, setUndoing] = useState(false)
  const [undoError, setUndoError] = useState(false)

  // The queue's source of truth. State only mirrors it for rendering: two
  // answers can land within one render (arrow keys at speed), and reading the
  // head from state would answer the same card twice.
  const queueRef = useRef<ReviewQuestion[]>([])
  /** Every question id ever enqueued this session — refill deduplication. */
  const seenRef = useRef<Set<string>>(new Set())
  /** Ids of undone questions whose next yes/no must use the direct paths. */
  const directRef = useRef<Set<string>>(new Set())
  /** Marker uids learned during undo, keyed by question id. */
  const markerRef = useRef<Map<string, string>>(new Map())
  /** In-flight answer requests; undo awaits them before reverting. */
  const inflightRef = useRef<Map<string, Promise<boolean>>>(new Map())
  const fetchingRef = useRef(false)
  const startedRef = useRef(false)
  const undoingRef = useRef(false)
  const failedRef = useRef<FailedAnswer[]>([])
  failedRef.current = failed
  const lastAnswerRef = useRef<AnsweredQuestion | null>(null)
  lastAnswerRef.current = lastAnswer

  /** Replaces the queue in both the ref (truth) and state (render mirror). */
  const commitQueue = useCallback((next: ReviewQuestion[]) => {
    queueRef.current = next
    setQueue(next)
  }, [])

  const load = useCallback(async () => {
    if (fetchingRef.current) {
      return
    }
    fetchingRef.current = true
    setFetching(true)
    const initial = !startedRef.current
    startedRef.current = true
    try {
      const res = await fetchReviewQueue()
      const fresh = res.questions.filter((q) => !seenRef.current.has(q.id))
      for (const q of fresh) {
        seenRef.current.add(q.id)
      }
      if (fresh.length > 0) {
        commitQueue([...queueRef.current, ...fresh])
      } else {
        setExhausted(true)
      }
      setReason(res.reason)
      setRemaining(res.remaining)
      if (initial) {
        setAnswered(res.answered)
      }
      setLoadError(false)
    } catch {
      setLoadError(true)
    } finally {
      fetchingRef.current = false
      setFetching(false)
    }
  }, [commitQueue])

  // Initial load and background refills fall out of the same rule: whenever the
  // local queue runs low and nothing says stop, fetch. The error/exhausted
  // latches keep a failing or dry server from being hammered in a loop.
  useEffect(() => {
    if (queue.length <= REFILL_AT && !exhausted && !loadError) {
      void load()
    }
  }, [queue.length, exhausted, loadError, load])

  /**
   * Finds the marker uid a face-yes must act on: the one the question carried,
   * one learned earlier, or — after a `create_marker` yes — the marker the
   * backend just created, looked up by face index.
   */
  const resolveMarkerUid = useCallback(async (q: ReviewQuestion): Promise<string> => {
    const known = markerRef.current.get(q.id) ?? q.marker_uid
    if (known !== undefined && known !== '') {
      markerRef.current.set(q.id, known)
      return known
    }
    const res = await fetchFaces(q.photo.uid)
    const face = res.faces.find((f) => f.face_index === q.face_index)
    const marker = face?.marker_uid
    if (marker === undefined || marker === '') {
      throw new Error('marker not found for the answered face')
    }
    markerRef.current.set(q.id, marker)
    return marker
  }, [])

  /** Applies the inverse of one applied answer through the direct endpoints. */
  const revertOnServer = useCallback(
    async (last: AnsweredQuestion) => {
      const q = last.question
      if (q.kind === 'face') {
        if (last.answer === 'no') {
          await unrejectFace({
            photo_uid: q.photo.uid,
            face_index: q.face_index ?? 0,
            subject_uid: q.subject?.uid ?? '',
          })
          return
        }
        const markerUid = await resolveMarkerUid(q)
        await assignFace(q.photo.uid, { action: 'unassign_person', marker_uid: markerUid })
        return
      }
      if (last.answer === 'no') {
        await unrejectLabel({ photo_uid: q.photo.uid, label_uid: q.label?.uid ?? '' })
        return
      }
      await detachLabel(q.label?.uid ?? '', q.photo.uid)
    },
    [resolveMarkerUid],
  )

  /** Applies a yes/no through the direct write paths (re-answer after undo). */
  const sendDirect = useCallback(async (q: ReviewQuestion, verdict: ReviewAnswer) => {
    if (q.kind === 'face') {
      if (verdict === 'no') {
        await rejectFace({
          photo_uid: q.photo.uid,
          face_index: q.face_index ?? 0,
          subject_uid: q.subject?.uid ?? '',
        })
        return
      }
      const markerUid = markerRef.current.get(q.id) ?? q.marker_uid
      if (markerUid !== undefined && markerUid !== '') {
        await assignFace(q.photo.uid, {
          action: 'assign_person',
          marker_uid: markerUid,
          subject_uid: q.subject?.uid,
        })
        return
      }
      await assignFace(q.photo.uid, {
        action: 'create_marker',
        face_index: q.face_index,
        subject_uid: q.subject?.uid,
        bbox: q.bbox?.relative,
      })
      return
    }
    if (verdict === 'no') {
      await rejectLabel({ photo_uid: q.photo.uid, label_uid: q.label?.uid ?? '' })
      return
    }
    await attachLabel(q.label?.uid ?? '', q.photo.uid)
  }, [])

  /** Settles one answer in the background; a failure lands in `failed`. */
  const sendAnswer = useCallback(
    async (q: ReviewQuestion, verdict: ReviewAnswer): Promise<boolean> => {
      try {
        // A skip writes nothing server-side, so it may always take the review
        // endpoint; an undone yes/no must not — it would no-op as
        // `already_answered` and silently drop the verdict.
        if (directRef.current.has(q.id) && verdict !== 'skip') {
          await sendDirect(q, verdict)
        } else {
          await answerReview(q.id, verdict)
        }
        return true
      } catch {
        setFailed((prev) => [...prev, { question: q, answer: verdict }])
        return false
      }
    },
    [sendDirect],
  )

  const answer = useCallback(
    (verdict: ReviewAnswer) => {
      const q = queueRef.current.at(0)
      if (q === undefined || undoingRef.current) {
        return
      }
      commitQueue(queueRef.current.slice(1))
      setLastAnswer({ question: q, answer: verdict })
      setUndoError(false)
      if (verdict !== 'skip') {
        setAnswered((n) => n + 1)
      }
      setRemaining((n) => (n > 0 ? n - 1 : 0))
      inflightRef.current.set(q.id, sendAnswer(q, verdict))
    },
    [commitQueue, sendAnswer],
  )

  const doUndo = useCallback(async () => {
    const last = lastAnswerRef.current
    if (last === null || undoingRef.current) {
      return
    }
    undoingRef.current = true
    setUndoing(true)
    setUndoError(false)
    try {
      // Wait for the optimistic request to settle first, so the inverse never
      // races ahead of the answer it reverts.
      const applied = (await inflightRef.current.get(last.question.id)) ?? true
      if (!applied) {
        // The answer never reached the server: nothing to revert, just take it
        // off the retry pile so it is not re-sent later.
        setFailed((prev) => prev.filter((f) => f.question.id !== last.question.id))
      } else if (last.answer !== 'skip') {
        await revertOnServer(last)
        directRef.current.add(last.question.id)
      }
      commitQueue([last.question, ...queueRef.current])
      if (last.answer !== 'skip') {
        setAnswered((n) => (n > 0 ? n - 1 : 0))
      }
      setRemaining((n) => n + 1)
      setLastAnswer(null)
    } catch {
      setUndoError(true)
    } finally {
      undoingRef.current = false
      setUndoing(false)
    }
  }, [commitQueue, revertOnServer])

  const undo = useCallback(() => {
    void doUndo()
  }, [doUndo])

  const retryLoad = useCallback(() => {
    setLoadError(false)
    setExhausted(false)
  }, [])

  const retryFailed = useCallback(() => {
    const toRetry = failedRef.current
    setFailed([])
    for (const f of toRetry) {
      inflightRef.current.set(f.question.id, sendAnswer(f.question, f.answer))
    }
  }, [sendAnswer])

  const dismissFailed = useCallback(() => {
    setFailed([])
  }, [])

  return {
    current: queue[0],
    pending: queue,
    answered,
    remaining,
    fetching,
    loadError,
    exhausted,
    reason,
    failed,
    lastAnswer,
    undoing,
    undoError,
    answer,
    undo,
    retryLoad,
    retryFailed,
    dismissFailed,
  }
}
