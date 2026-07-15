import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  buildAssignRequest,
  buildRejection,
  candidateKey,
  type ReviewItem,
  toReviewItems,
} from '../lib/candidateReview'
import { hasActionable, type PersonState } from '../lib/recognitionSweep'
import { type Candidate } from '../services/faces'
import { rejectFace } from '../services/feedback'
import { assignFace } from '../services/people'
import {
  type SweepParams,
  type SweepProgress,
  type SweepSummary,
  streamSweep,
} from '../services/recognition'

/** Where the sweep is in its lifecycle. */
export type SweepPhase = 'idle' | 'scanning' | 'done'

/** Which action failed, and how many failed, for a dismissible banner. */
export interface SweepActionError {
  kind: 'confirm' | 'reject' | 'confirmAll' | 'scan'
  count: number
}

/** Live progress of a "confirm all" walking one person's list. */
export interface ConfirmAllProgress {
  subjectUid: string
  current: number
  total: number
  failed: number
}

/** The sweep controller returned by {@link useSweepReview}. */
export interface SweepReview {
  phase: SweepPhase
  progress: SweepProgress | null
  summary: SweepSummary | null
  /** People with at least one actionable candidate, in arrival order. */
  people: PersonState[]
  actionError: SweepActionError | null
  confirmAll: ConfirmAllProgress | null
  scan: (params: SweepParams) => void
  cancel: () => void
  confirm: (subjectUid: string, candidate: Candidate) => void
  reject: (subjectUid: string, candidate: Candidate) => void
  confirmAllForPerson: (subjectUid: string) => void
  cancelConfirmAll: () => void
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

/** mapItems returns people with the named person's items transformed by fn. */
function mapItems(
  people: PersonState[],
  subjectUid: string,
  fn: (items: ReviewItem[]) => ReviewItem[],
): PersonState[] {
  return people.map((person) =>
    person.subject.uid === subjectUid ? { ...person, items: fn(person.items) } : person,
  )
}

/**
 * useSweepReview drives the /recognition page: it streams the sweep, collecting one
 * person card per subject with matches as they arrive, and applies confirm/reject
 * optimistically so cards never reload under the user. A person whose last actionable
 * candidate is cleared drops out of {@link SweepReview.people} — the shrinking list is
 * the reward loop. "Confirm all" walks a single person's list sequentially, cancellable.
 *
 * Confirming reuses the same assign/reject rules as the /faces grid
 * ({@link buildAssignRequest} / {@link buildRejection}); this hook only orchestrates
 * them across many people at once and never auto-decides anything.
 */
export function useSweepReview(): SweepReview {
  const [phase, setPhase] = useState<SweepPhase>('idle')
  const [progress, setProgress] = useState<SweepProgress | null>(null)
  const [summary, setSummary] = useState<SweepSummary | null>(null)
  const [allPeople, setAllPeople] = useState<PersonState[]>([])
  const [actionError, setActionError] = useState<SweepActionError | null>(null)
  const [confirmAll, setConfirmAll] = useState<ConfirmAllProgress | null>(null)

  const peopleRef = useRef(allPeople)
  peopleRef.current = allPeople
  const abortRef = useRef<AbortController | null>(null)
  const cancelAllRef = useRef(false)
  const runningAllRef = useRef(false)

  // Abort an in-flight scan when the page unmounts.
  useEffect(() => () => abortRef.current?.abort(), [])

  const scan = useCallback((params: SweepParams) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    cancelAllRef.current = true
    runningAllRef.current = false
    setPhase('scanning')
    setProgress(null)
    setSummary(null)
    setAllPeople([])
    setActionError(null)
    setConfirmAll(null)

    streamSweep(
      params,
      (message) => {
        switch (message.type) {
          case 'progress':
            setProgress(message.progress)
            break
          case 'person':
            setAllPeople((prev) => [
              ...prev,
              { subject: message.person.subject, items: toReviewItems(message.person.candidates) },
            ])
            break
          case 'summary':
            setSummary(message.summary)
            break
        }
      },
      controller.signal,
    )
      .then(() => {
        setPhase('done')
      })
      .catch((err: unknown) => {
        setPhase('done')
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setActionError({ kind: 'scan', count: 1 })
      })
  }, [])

  const cancel = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  // confirmOne flips a card to done optimistically, then assigns; on failure it marks
  // the card errored (so it stays actionable and can be retried) and reports the miss.
  const confirmOne = useCallback(
    async (subjectUid: string, candidate: Candidate): Promise<boolean> => {
      const key = candidateKey(candidate)
      setAllPeople((prev) =>
        mapItems(prev, subjectUid, (items) => replaceStatus(items, key, 'done')),
      )
      try {
        await assignFace(candidate.photo.uid, buildAssignRequest(candidate, subjectUid))
        return true
      } catch {
        setAllPeople((prev) =>
          mapItems(prev, subjectUid, (items) => replaceStatus(items, key, 'error')),
        )
        return false
      }
    },
    [],
  )

  const confirm = useCallback(
    (subjectUid: string, candidate: Candidate) => {
      void confirmOne(subjectUid, candidate).then((ok) => {
        if (!ok) {
          setActionError({ kind: 'confirm', count: 1 })
        }
      })
    },
    [confirmOne],
  )

  const reject = useCallback((subjectUid: string, candidate: Candidate) => {
    const key = candidateKey(candidate)
    const person = peopleRef.current.find((entry) => entry.subject.uid === subjectUid)
    if (person === undefined) {
      return
    }
    const index = person.items.findIndex((item) => candidateKey(item.candidate) === key)
    if (index === -1) {
      return
    }
    const removed = person.items[index]
    setAllPeople((prev) =>
      mapItems(prev, subjectUid, (items) =>
        items.filter((item) => candidateKey(item.candidate) !== key),
      ),
    )
    rejectFace(buildRejection(candidate, subjectUid)).catch(() => {
      setActionError({ kind: 'reject', count: 1 })
      setAllPeople((prev) =>
        mapItems(prev, subjectUid, (items) => {
          if (items.some((item) => candidateKey(item.candidate) === key)) {
            return items
          }
          const next = items.slice()
          next.splice(Math.min(index, next.length), 0, removed)
          return next
        }),
      )
    })
  }, [])

  const confirmAllForPerson = useCallback(
    (subjectUid: string) => {
      if (runningAllRef.current) {
        return
      }
      const person = peopleRef.current.find((entry) => entry.subject.uid === subjectUid)
      const targets =
        person?.items
          .filter((item) => item.status === 'pending' || item.status === 'error')
          .map((item) => item.candidate) ?? []
      if (targets.length === 0) {
        return
      }
      runningAllRef.current = true
      cancelAllRef.current = false
      setActionError(null)
      setConfirmAll({ subjectUid, current: 0, total: targets.length, failed: 0 })
      void (async () => {
        let done = 0
        let failed = 0
        for (const candidate of targets) {
          if (cancelAllRef.current) {
            break
          }
          done += 1
          if (!(await confirmOne(subjectUid, candidate))) {
            failed += 1
          }
          setConfirmAll((state) => (state === null ? state : { ...state, current: done, failed }))
        }
        runningAllRef.current = false
        setConfirmAll(null)
        if (failed > 0) {
          setActionError({ kind: 'confirmAll', count: failed })
        }
      })()
    },
    [confirmOne],
  )

  const cancelConfirmAll = useCallback(() => {
    cancelAllRef.current = true
  }, [])

  const dismissError = useCallback(() => {
    setActionError(null)
  }, [])

  const people = useMemo(() => allPeople.filter(hasActionable), [allPeople])

  return {
    phase,
    progress,
    summary,
    people,
    actionError,
    confirmAll,
    scan,
    cancel,
    confirm,
    reject,
    confirmAllForPerson,
    cancelConfirmAll,
    dismissError,
  }
}
