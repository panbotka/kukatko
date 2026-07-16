import { useCallback, useEffect, useRef, useState } from 'react'

import {
  canUnassign,
  type OutlierItem,
  type OutlierStatus,
  outlierKey,
  toOutlierItems,
} from '../lib/outlierReview'
import { confirmFace } from '../services/feedback'
import { assignFace, type OutlierFace } from '../services/people'

/** Which action produced an error the page should surface, and how many failed. */
export interface OutlierError {
  kind: 'unassign' | 'confirm' | 'bulk'
  count: number
}

/** Live progress of a running bulk unassign, read by the selection bar. */
export interface BulkState {
  running: boolean
  current: number
  total: number
  failed: number
}

/** The review controller returned by {@link useOutlierReview}. */
export interface OutlierReview {
  items: OutlierItem[]
  /** ✓ "yes, this is wrong": detaches the person from the face. */
  unassign: (face: OutlierFace) => void
  /** ✗ "no, this really is them": records the confirmation durably. */
  confirm: (face: OutlierFace) => void
  /** Unassigns every face in `faces`, one at a time, reporting progress. */
  unassignMany: (faces: OutlierFace[]) => void
  /** Abandons a running bulk unassign after the current face. */
  cancelBulk: () => void
  bulkState: BulkState
  actionError: OutlierError | null
  dismissError: () => void
}

/** replaceStatus returns items with the addressed card set to a new status. */
function replaceStatus(items: OutlierItem[], key: string, status: OutlierStatus): OutlierItem[] {
  return items.map((item) => (outlierKey(item.face) === key ? { ...item, status } : item))
}

const IDLE_BULK: BulkState = { running: false, current: 0, total: 0, failed: 0 }

/**
 * useOutlierReview owns the mutable state of the /outliers grid: it seeds a
 * working list from a fresh query and applies both verdicts **optimistically and
 * in place** — the card flips where it stands, the grid never reloads and the
 * scroll never jumps out from under a curator halfway down a long list.
 *
 * The two verdicts are opposites and go to opposite endpoints. ✓ ("yes, this is
 * wrong") detaches the person through the ordinary assign state machine; ✗ ("no,
 * this really is them") records a durable confirmation, which the backend then
 * excludes from later outlier queries — an outlier list that re-offers the same
 * false alarms every pass is the exact problem this page exists to fix. A failed
 * write marks its own card `error` and never touches its neighbours.
 *
 * The bulk unassign walks the selection sequentially and **reports partial
 * failure honestly**: already-unassigned faces stay unassigned, the failures are
 * counted and surfaced rather than rolled back or swallowed.
 *
 * A new `faces` array (a fresh subject or threshold) resets everything,
 * abandoning any run in flight.
 */
export function useOutlierReview(
  subjectUid: string | null,
  faces: readonly OutlierFace[] | null,
): OutlierReview {
  const [items, setItems] = useState<OutlierItem[]>([])
  const [bulkState, setBulkState] = useState<BulkState>(IDLE_BULK)
  const [actionError, setActionError] = useState<OutlierError | null>(null)

  // Refs let the action callbacks stay stable while always reading the latest state.
  const subjectRef = useRef(subjectUid)
  subjectRef.current = subjectUid
  const cancelRef = useRef(false)
  const runningRef = useRef(false)

  // A fresh query replaces the list and abandons any run in progress.
  useEffect(() => {
    cancelRef.current = true
    runningRef.current = false
    setItems(faces === null ? [] : toOutlierItems(faces))
    setBulkState(IDLE_BULK)
    setActionError(null)
  }, [faces])

  // unassignOne flips a card optimistically, then detaches; on failure it marks
  // the card errored and reports it, so callers can tally a partial bulk run.
  const unassignOne = useCallback(async (face: OutlierFace): Promise<boolean> => {
    if (!canUnassign(face)) {
      return false
    }
    const key = outlierKey(face)
    setItems((prev) => replaceStatus(prev, key, 'removed'))
    try {
      await assignFace(face.photo_uid, {
        action: 'unassign_person',
        marker_uid: face.marker_uid,
      })
      return true
    } catch {
      setItems((prev) => replaceStatus(prev, key, 'error'))
      return false
    }
  }, [])

  const unassign = useCallback(
    (face: OutlierFace) => {
      void unassignOne(face).then((ok) => {
        if (!ok) {
          setActionError({ kind: 'unassign', count: 1 })
        }
      })
    },
    [unassignOne],
  )

  const confirm = useCallback((face: OutlierFace) => {
    const subject = subjectRef.current
    if (subject === null || subject === '') {
      return
    }
    const key = outlierKey(face)
    setItems((prev) => replaceStatus(prev, key, 'confirmed'))
    confirmFace({
      photo_uid: face.photo_uid,
      face_index: face.face_index,
      subject_uid: subject,
    }).catch(() => {
      setItems((prev) => replaceStatus(prev, key, 'error'))
      setActionError({ kind: 'confirm', count: 1 })
    })
  }, [])

  const unassignMany = useCallback(
    (faces: OutlierFace[]) => {
      if (runningRef.current) {
        return
      }
      const targets = faces.filter(canUnassign)
      if (targets.length === 0) {
        return
      }
      runningRef.current = true
      cancelRef.current = false
      setActionError(null)
      setBulkState({ running: true, current: 0, total: targets.length, failed: 0 })
      void (async () => {
        let done = 0
        let failed = 0
        for (const face of targets) {
          if (cancelRef.current) {
            break
          }
          done += 1
          const ok = await unassignOne(face)
          if (!ok) {
            failed += 1
          }
          setBulkState((state) => ({ ...state, current: done, failed }))
        }
        runningRef.current = false
        setBulkState((state) => ({ ...state, running: false }))
        if (failed > 0) {
          setActionError({ kind: 'bulk', count: failed })
        }
      })()
    },
    [unassignOne],
  )

  const cancelBulk = useCallback(() => {
    cancelRef.current = true
  }, [])

  const dismissError = useCallback(() => {
    setActionError(null)
  }, [])

  return {
    items,
    unassign,
    confirm,
    unassignMany,
    cancelBulk,
    bulkState,
    actionError,
    dismissError,
  }
}
