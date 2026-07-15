import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  buildAssignRequest,
  buildRejection,
  candidateKey,
  type FilterTab,
  isActionable,
  matchesTab,
  type ReviewItem,
  tabCounts,
  toReviewItems,
} from '../lib/candidateReview'
import { type Candidate } from '../services/faces'
import { rejectFace } from '../services/feedback'
import { assignFace } from '../services/people'

/** Which action produced an error the page should surface, and how many failed. */
export interface ReviewError {
  kind: 'confirm' | 'reject' | 'confirmAll'
  count: number
}

/** Live progress of a running "confirm all", read by the button and the tab strip. */
export interface ConfirmAllState {
  running: boolean
  current: number
  total: number
  failed: number
}

/** The review controller returned by {@link useCandidateReview}. */
export interface CandidateReview {
  items: ReviewItem[]
  counts: Record<FilterTab, number>
  confirm: (candidate: Candidate) => void
  reject: (candidate: Candidate) => void
  confirmAll: (tab: FilterTab) => void
  cancelConfirmAll: () => void
  confirmAllState: ConfirmAllState
  actionError: ReviewError | null
  dismissError: () => void
}

/** replaceStatus returns items with the addressed card set to a new status. */
function replaceStatus(
  items: ReviewItem[],
  key: string,
  status: ReviewItem['status'],
): ReviewItem[] {
  return items.map((item) => (candidateKey(item.candidate) === key ? { ...item, status } : item))
}

const IDLE_CONFIRM_ALL: ConfirmAllState = { running: false, current: 0, total: 0, failed: 0 }

/**
 * useCandidateReview owns the mutable review state for the /faces grid: it seeds a
 * working list from a fresh search, and applies the confirm/reject actions
 * optimistically so the grid never reloads or jumps under the user.
 *
 * Confirming flips the card to "done" in place and calls the assign endpoint; a
 * failure marks it "error" so it can be retried, and never touches its neighbours.
 * Rejecting removes the card and persists the rejection, restoring it only if the
 * call fails. "Confirm all" walks the actionable cards of one tab sequentially,
 * reporting live progress, cancellable, and leaves every already-confirmed card
 * confirmed even when a later one fails.
 *
 * A new `candidates` array (a fresh search) resets everything, cancelling any run in
 * flight.
 */
export function useCandidateReview(
  subjectUid: string | null,
  candidates: Candidate[] | null,
): CandidateReview {
  const [items, setItems] = useState<ReviewItem[]>([])
  const [confirmAllState, setConfirmAllState] = useState<ConfirmAllState>(IDLE_CONFIRM_ALL)
  const [actionError, setActionError] = useState<ReviewError | null>(null)

  // Refs let the action callbacks stay stable while always reading the latest state.
  const itemsRef = useRef(items)
  itemsRef.current = items
  const subjectRef = useRef(subjectUid)
  subjectRef.current = subjectUid
  const cancelRef = useRef(false)
  const runningRef = useRef(false)

  // A fresh search replaces the list and abandons any run in progress.
  useEffect(() => {
    cancelRef.current = true
    runningRef.current = false
    setItems(candidates === null ? [] : toReviewItems(candidates))
    setConfirmAllState(IDLE_CONFIRM_ALL)
    setActionError(null)
  }, [candidates])

  // confirmOne flips a card to done optimistically, then assigns; on failure it
  // marks the card errored and reports success so callers can tally partial runs.
  const confirmOne = useCallback(async (candidate: Candidate): Promise<boolean> => {
    const subject = subjectRef.current
    if (subject === null || subject === '') {
      return false
    }
    const key = candidateKey(candidate)
    setItems((prev) => replaceStatus(prev, key, 'done'))
    try {
      await assignFace(candidate.photo.uid, buildAssignRequest(candidate, subject))
      return true
    } catch {
      setItems((prev) => replaceStatus(prev, key, 'error'))
      return false
    }
  }, [])

  const confirm = useCallback(
    (candidate: Candidate) => {
      void confirmOne(candidate).then((ok) => {
        if (!ok) {
          setActionError({ kind: 'confirm', count: 1 })
        }
      })
    },
    [confirmOne],
  )

  const reject = useCallback((candidate: Candidate) => {
    const subject = subjectRef.current
    if (subject === null || subject === '') {
      return
    }
    const key = candidateKey(candidate)
    const index = itemsRef.current.findIndex((item) => candidateKey(item.candidate) === key)
    if (index === -1) {
      return
    }
    const removed = itemsRef.current[index]
    setItems((prev) => prev.filter((item) => candidateKey(item.candidate) !== key))
    rejectFace(buildRejection(candidate, subject)).catch(() => {
      setActionError({ kind: 'reject', count: 1 })
      // Put the card back where it was; guard against a double-restore.
      setItems((prev) => {
        if (prev.some((item) => candidateKey(item.candidate) === key)) {
          return prev
        }
        const next = prev.slice()
        next.splice(Math.min(index, next.length), 0, removed)
        return next
      })
    })
  }, [])

  const confirmAll = useCallback(
    (tab: FilterTab) => {
      if (runningRef.current) {
        return
      }
      const targets = itemsRef.current
        .filter((item) => matchesTab(item, tab) && isActionable(item))
        .map((item) => item.candidate)
      if (targets.length === 0) {
        return
      }
      runningRef.current = true
      cancelRef.current = false
      setActionError(null)
      setConfirmAllState({ running: true, current: 0, total: targets.length, failed: 0 })
      void (async () => {
        let done = 0
        let failed = 0
        for (const candidate of targets) {
          if (cancelRef.current) {
            break
          }
          done += 1
          const ok = await confirmOne(candidate)
          if (!ok) {
            failed += 1
          }
          setConfirmAllState((state) => ({ ...state, current: done, failed }))
        }
        runningRef.current = false
        setConfirmAllState((state) => ({ ...state, running: false }))
        if (failed > 0) {
          setActionError({ kind: 'confirmAll', count: failed })
        }
      })()
    },
    [confirmOne],
  )

  const cancelConfirmAll = useCallback(() => {
    cancelRef.current = true
  }, [])

  const dismissError = useCallback(() => {
    setActionError(null)
  }, [])

  const counts = useMemo(() => tabCounts(items), [items])

  return {
    items,
    counts,
    confirm,
    reject,
    confirmAll,
    cancelConfirmAll,
    confirmAllState,
    actionError,
    dismissError,
  }
}
