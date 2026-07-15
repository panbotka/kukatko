import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { CandidateResults } from '../components/faces/CandidateResults'
import { CandidateSearchForm } from '../components/faces/CandidateSearchForm'
import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { useCandidateReview } from '../hooks/useCandidateReview'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useSubjects } from '../hooks/useSubjects'
import {
  candidateKey,
  type FilterTab,
  FILTER_TABS,
  isActionable,
  matchesTab,
  type ReviewItem,
} from '../lib/candidateReview'
import {
  clampThresholdPercent,
  percentToDistance,
  THRESHOLD_DEFAULT_PERCENT,
} from '../lib/faceThreshold'
import { type CandidateResult, searchCandidates } from '../services/faces'

/** The page's fetch state for the candidate search. */
type SearchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; result: CandidateResult }

/**
 * nextActionableKey returns the key of the next actionable card after `fromIndex`,
 * scanning forward and wrapping, and skipping the starting card itself. It is how the
 * keyboard flow moves focus to the next thing to decide after a confirm or reject.
 * Returns null when no other card can be acted on.
 */
function nextActionableKey(items: ReviewItem[], fromIndex: number): string | null {
  const count = items.length
  for (let offset = 1; offset <= count; offset += 1) {
    const index = (((fromIndex + offset) % count) + count) % count
    if (index !== fromIndex && isActionable(items[index])) {
      return candidateKey(items[index].candidate)
    }
  }
  return null
}

/**
 * FacesPage is the "find a person among untagged photos" workspace: pick a subject,
 * tune how sure the match must be, and work through the resulting faces fast — with
 * one-tap confirm/reject, a keyboard flow, and a batch "confirm all". It is
 * editor-only. See docs/FRONTEND.md.
 */
export function FacesPage() {
  const { t } = useTranslation()
  const { subjects, loading: subjectsLoading } = useSubjects()
  const [searchParams, setSearchParams] = useSearchParams()

  // Config lives in local state, seeded from the URL so a reload or Back restores the
  // last search; it is written back to the URL only when a search actually runs.
  const [subjectUid, setSubjectUid] = useState<string | null>(() => searchParams.get('subject'))
  const [thresholdPercent, setThresholdPercent] = useState(() =>
    clampThresholdPercent(Number(searchParams.get('threshold') ?? THRESHOLD_DEFAULT_PERCENT)),
  )
  const [limit, setLimit] = useState(() =>
    Math.max(0, Math.trunc(Number(searchParams.get('limit') ?? 0)) || 0),
  )
  const [activeTab, setActiveTab] = useState<FilterTab>(() => {
    const tab = searchParams.get('tab')
    return FILTER_TABS.includes(tab as FilterTab) ? (tab as FilterTab) : 'all'
  })

  const [state, setState] = useState<SearchState>({ status: 'idle' })
  const abortRef = useRef<AbortController | null>(null)
  const [focusedKey, setFocusedKey] = useState<string | null>(null)
  const gridRef = useRef<HTMLDivElement>(null)

  const result = state.status === 'ready' ? state.result : null
  // The review acts on the subject the *results* belong to, not the picker's current
  // value — changing the picker after a search must not misdirect a confirm.
  const review = useCandidateReview(
    result === null ? null : result.subject_uid,
    result === null ? null : result.candidates,
  )

  const runSearch = useCallback((uid: string, percent: number, cap: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setState({ status: 'loading' })
    searchCandidates(uid, { threshold: percentToDistance(percent), limit: cap }, controller.signal)
      .then((res) => {
        setState({ status: 'ready', result: res })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
  }, [])

  // Abort an in-flight search when leaving the page.
  useEffect(() => () => abortRef.current?.abort(), [])

  // Restore a search from the URL on first load, so Back and refresh work.
  const autoRan = useRef(false)
  useEffect(() => {
    if (autoRan.current) {
      return
    }
    autoRan.current = true
    const uid = searchParams.get('subject')
    if (uid !== null && uid !== '') {
      runSearch(uid, thresholdPercent, limit)
    }
  }, [searchParams, thresholdPercent, limit, runSearch])

  const handleSearch = useCallback(() => {
    if (subjectUid === null || subjectUid === '') {
      return
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('subject', subjectUid)
        next.set('threshold', String(thresholdPercent))
        next.set('limit', String(limit))
        return next
      },
      { replace: true },
    )
    setFocusedKey(null)
    runSearch(subjectUid, thresholdPercent, limit)
  }, [subjectUid, thresholdPercent, limit, runSearch, setSearchParams])

  const selectTab = useCallback(
    (tab: FilterTab) => {
      setActiveTab(tab)
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.set('tab', tab)
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const visible = useMemo(
    () => review.items.filter((item) => matchesTab(item, activeTab)),
    [review.items, activeTab],
  )
  const focusedIndex = useMemo(
    () =>
      focusedKey === null
        ? -1
        : visible.findIndex((item) => candidateKey(item.candidate) === focusedKey),
    [visible, focusedKey],
  )
  const actionableCount = useMemo(() => visible.filter(isActionable).length, [visible])

  // Keep the focused card in view as the keyboard moves the selection.
  useEffect(() => {
    if (focusedIndex < 0) {
      return
    }
    gridRef.current?.querySelector('[data-focused="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [focusedIndex, visible])

  const columns = useCallback(() => {
    const el = gridRef.current
    if (el === null) {
      return 1
    }
    const tracks = getComputedStyle(el)
      .gridTemplateColumns.split(' ')
      .filter((track) => track.trim() !== '')
    return Math.max(1, tracks.length)
  }, [])

  const move = useCallback(
    (delta: number) => {
      if (visible.length === 0) {
        return
      }
      const base =
        focusedIndex < 0 ? 0 : Math.min(Math.max(focusedIndex + delta, 0), visible.length - 1)
      setFocusedKey(candidateKey(visible[base].candidate))
    },
    [visible, focusedIndex],
  )

  const confirmFocused = useCallback(() => {
    if (focusedIndex < 0) {
      return
    }
    const item = visible[focusedIndex]
    if (!isActionable(item)) {
      return
    }
    const next = nextActionableKey(visible, focusedIndex)
    review.confirm(item.candidate)
    if (next !== null) {
      setFocusedKey(next)
    }
  }, [visible, focusedIndex, review])

  const rejectFocused = useCallback(() => {
    if (focusedIndex < 0) {
      return
    }
    const item = visible[focusedIndex]
    const next = nextActionableKey(visible, focusedIndex)
    review.reject(item.candidate)
    setFocusedKey(next)
  }, [visible, focusedIndex, review])

  useKeyboardShortcuts(
    {
      ArrowRight: () => {
        move(1)
      },
      l: () => {
        move(1)
      },
      ArrowLeft: () => {
        move(-1)
      },
      h: () => {
        move(-1)
      },
      ArrowDown: () => {
        move(columns())
      },
      j: () => {
        move(columns())
      },
      ArrowUp: () => {
        move(-columns())
      },
      k: () => {
        move(-columns())
      },
      y: confirmFocused,
      Enter: confirmFocused,
      n: rejectFocused,
    },
    { enabled: state.status === 'ready' && !review.confirmAllState.running },
  )

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('faceSearch.title')}</h1>
      <p className="text-secondary">{t('faceSearch.subtitle')}</p>

      <CandidateSearchForm
        subjects={subjects}
        subjectsLoading={subjectsLoading}
        subjectUid={subjectUid}
        thresholdPercent={thresholdPercent}
        limit={limit}
        loading={state.status === 'loading'}
        onSubjectChange={setSubjectUid}
        onThresholdChange={setThresholdPercent}
        onLimitChange={setLimit}
        onSearch={handleSearch}
      />

      {review.actionError !== null && (
        <Alert variant="danger" dismissible onClose={review.dismissError}>
          {review.actionError.kind === 'confirmAll' &&
            t('faceSearch.error.confirmAll', { count: review.actionError.count })}
          {review.actionError.kind === 'confirm' && t('faceSearch.error.confirm')}
          {review.actionError.kind === 'reject' && t('faceSearch.error.reject')}
        </Alert>
      )}

      {state.status === 'idle' && (
        <EmptyState
          icon={<Icon name="search" />}
          title={t('faceSearch.idle.title')}
          hint={t('faceSearch.idle.hint')}
        />
      )}

      {state.status === 'loading' && (
        <div className="d-flex align-items-center gap-2 py-5 justify-content-center text-secondary">
          <Spinner animation="border" role="status" size="sm" />
          <span>{t('faceSearch.loading')}</span>
        </div>
      )}

      {state.status === 'error' && <Alert variant="danger">{t('faceSearch.error.search')}</Alert>}

      {result !== null && (
        <CandidateResults
          result={result}
          review={review}
          activeTab={activeTab}
          onSelectTab={selectTab}
          visible={visible}
          focusedIndex={focusedIndex}
          actionableCount={actionableCount}
          gridRef={gridRef}
        />
      )}
    </>
  )
}
