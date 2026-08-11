import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { OutlierCard } from '../components/people/OutlierCard'
import { OutlierControls } from '../components/people/OutlierControls'
import { OutlierStats } from '../components/people/OutlierStats'
import { ReviewLightbox } from '../components/review/ReviewLightbox'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useLightbox } from '../hooks/useLightbox'
import { useReloadKey } from '../hooks/useReloadKey'
import { useOutlierReview } from '../hooks/useOutlierReview'
import { useSelection } from '../hooks/useSelection'
import { useSubjects } from '../hooks/useSubjects'
import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { isTypingElement } from '../lib/ratingHotkeys'
import {
  canUnassign,
  clampOutlierThresholdPercent,
  distancePercent,
  isActionable,
  OUTLIER_LIMIT,
  OUTLIER_THRESHOLD_DEFAULT_PERCENT,
  type OutlierItem,
  outlierKey,
  outlierThresholdDistance,
} from '../lib/outlierReview'
import { fetchOutliers, type OutlierResult } from '../services/people'

/**
 * The preview the overlay asks for: `fit_*` (the whole frame), never a square
 * `tile_*` — the face rectangle is placed in coordinates of the full photo, so a
 * centre-cropped tile would draw it somewhere else entirely.
 */
const OUTLIER_LIGHTBOX_SIZE = 'fit_1280'

/** The page's fetch state for the outlier query. */
type State =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; result: OutlierResult }

/**
 * How long the threshold slider settles before it queries. The dial is live —
 * you watch the list shrink as you drag — but without this a single drag would
 * fire a query per step.
 */
const THRESHOLD_DEBOUNCE_MS = 250

/**
 * nextActionableIndex returns the index of the next card still awaiting a
 * verdict after `fromIndex`, scanning forward and wrapping, skipping the
 * starting card itself. It is how the keyboard flow lands on the next thing to
 * decide. Returns -1 when nothing else can be acted on.
 */
function nextActionableIndex(items: OutlierItem[], fromIndex: number): number {
  const count = items.length
  for (let offset = 1; offset <= count; offset += 1) {
    const index = (((fromIndex + offset) % count) + count) % count
    if (index !== fromIndex && isActionable(items[index])) {
      return index
    }
  }
  return -1
}

/**
 * OutliersPage is the sweep workspace for "which of this person's faces are
 * probably not them": pick someone, and their assigned faces come back ranked by
 * distance from their (trimmed) centroid, most suspicious first.
 *
 * It is the counterpart to the panel on the subject page, which stays — that one
 * is right when you are already looking at a person; this one is right when you
 * want to hunt. Each card is a **context crop** with the face outlined, because a
 * tight crop is unjudgeable, and carries the two opposite verdicts: ✓ unassigns,
 * ✗ vouches for the face so it is never offered again. How many cards fit across
 * is the user's call, via the library's `GridDensityControl` on the count shared
 * by every review tool (`REVIEW_GRID_SCOPE`). A selection with
 * shift-range and Ctrl/Cmd+A drives a bulk unassign, and the whole grid is
 * keyboard-drivable (arrows/`hjkl`, `y`/Enter, `n`, `x`) — photo-sorter's
 * equivalent page had none of this and was its weakest. Editor-only. See
 * docs/FRONTEND.md.
 */
export function OutliersPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('outliersPage.title'))
  const { subjects, loading: subjectsLoading } = useSubjects()
  const [searchParams, setSearchParams] = useSearchParams()

  // Config is seeded from the URL, so a link from a subject page arrives with
  // the person already picked and Back/refresh restore the view.
  const [subjectUid, setSubjectUid] = useState<string | null>(() => searchParams.get('subject'))
  const [thresholdPercent, setThresholdPercent] = useState(() =>
    clampOutlierThresholdPercent(
      Number(searchParams.get('threshold') ?? OUTLIER_THRESHOLD_DEFAULT_PERCENT),
    ),
  )
  // What the query actually uses: the slider value once it stops moving.
  const [committedThreshold, setCommittedThreshold] = useState(thresholdPercent)

  useEffect(() => {
    if (committedThreshold === thresholdPercent) {
      return
    }
    const timer = setTimeout(() => {
      setCommittedThreshold(thresholdPercent)
    }, THRESHOLD_DEBOUNCE_MS)
    return () => {
      clearTimeout(timer)
    }
  }, [thresholdPercent, committedThreshold])

  const [state, setState] = useState<State>({ status: 'idle' })
  const [reloadKey, reloadOutliers] = useReloadKey()

  useEffect(() => {
    if (subjectUid === null || subjectUid === '') {
      setState({ status: 'idle' })
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchOutliers(
      subjectUid,
      { threshold: outlierThresholdDistance(committedThreshold), limit: OUTLIER_LIMIT },
      controller.signal,
    )
      .then((result) => {
        setState({ status: 'ready', result })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [subjectUid, committedThreshold, reloadKey])

  // Mirror the committed config into the URL: view state lives there, so Back
  // always works. Writing the *committed* value keeps a drag out of history.
  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (subjectUid === null || subjectUid === '') {
          next.delete('subject')
        } else {
          next.set('subject', subjectUid)
        }
        next.set('threshold', String(committedThreshold))
        return next
      },
      { replace: true },
    )
  }, [subjectUid, committedThreshold, setSearchParams])

  const result = state.status === 'ready' ? state.result : null
  // The review acts on the subject the *results* belong to, never the picker's
  // current value — changing the person mid-review must not misdirect a write.
  const review = useOutlierReview(
    result?.subject_uid ?? null,
    result === null ? null : result.faces,
  )
  const items = review.items

  const selection = useSelection()
  const orderedKeys = useMemo(() => items.map((item) => outlierKey(item.face)), [items])
  const subjectName = useMemo(
    () => subjects.find((subject) => subject.uid === result?.subject_uid)?.name ?? '',
    [subjects, result],
  )

  const [focusedIndex, setFocusedIndex] = useState(-1)
  const gridRef = useRef<HTMLDivElement>(null)
  // The card shows a context crop; the overlay shows the whole photo it was cut
  // from, which is the thing the „is this really them?" question is settled on.
  const lightbox = useLightbox(items)
  const enlarged = lightbox.item
  // How many cards fit across, on the review grid's own stored count — the
  // library's density is a browsing preference and would fight this one.
  const { density } = useGridDensity(REVIEW_GRID_SCOPE)

  // A fresh query drops the highlight: it would otherwise point at a stale card.
  // Keyed on the *response*, deliberately — not on the working list, which gets a
  // new identity on every verdict. Resetting on that would undo the focus advance
  // after each decision, which is the whole point of the keyboard flow.
  useEffect(() => {
    setFocusedIndex(-1)
  }, [result])

  // Keep the focused card in view as the keyboard moves the highlight.
  useEffect(() => {
    if (focusedIndex < 0) {
      return
    }
    gridRef.current?.querySelector('[data-focused="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [focusedIndex])

  const move = useCallback(
    (delta: number) => {
      setFocusedIndex((current) => {
        if (items.length === 0) {
          return -1
        }
        if (current < 0) {
          return 0
        }
        return Math.min(Math.max(current + delta, 0), items.length - 1)
      })
    },
    [items.length],
  )

  const toggleSelect = useCallback(
    (index: number, shiftKey: boolean) => {
      const item = items.at(index)
      if (item === undefined) {
        return
      }
      if (!selection.active) {
        selection.enable()
      }
      if (shiftKey) {
        selection.toggleRange(outlierKey(item.face), orderedKeys)
        return
      }
      selection.toggle(outlierKey(item.face))
    },
    [items, selection, orderedKeys],
  )

  // Both verdicts advance the focus to the next undecided card: the point of the
  // keyboard flow is never having to reach for the mouse between decisions.
  const decideFocused = useCallback(
    (verdict: 'unassign' | 'confirm') => {
      if (focusedIndex < 0) {
        return
      }
      const item = items.at(focusedIndex)
      if (item === undefined || !isActionable(item)) {
        return
      }
      const next = nextActionableIndex(items, focusedIndex)
      if (verdict === 'unassign') {
        review.unassign(item.face)
      } else {
        review.confirm(item.face)
      }
      if (next >= 0) {
        setFocusedIndex(next)
      }
    },
    [focusedIndex, items, review],
  )

  const unassignFocused = useCallback(() => {
    decideFocused('unassign')
  }, [decideFocused])
  const confirmFocused = useCallback(() => {
    decideFocused('confirm')
  }, [decideFocused])

  // The overlay owns the keyboard while it is up: its arrows step photos, and
  // the grid's would move the highlight (and the selection) out from under it.
  const gridEnabled =
    state.status === 'ready' && items.length > 0 && !review.bulkState.running && !lightbox.isOpen

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
      // A row is exactly the pinned column count: the grid renders `repeat(N,
      // 1fr)`, so ↑/↓ need no measurement of the DOM to land one row away.
      ArrowDown: () => {
        move(density)
      },
      j: () => {
        move(density)
      },
      ArrowUp: () => {
        move(-density)
      },
      k: () => {
        move(-density)
      },
      y: unassignFocused,
      Enter: unassignFocused,
      n: confirmFocused,
      x: () => {
        if (focusedIndex >= 0) {
          toggleSelect(focusedIndex, false)
        }
      },
      Escape: () => {
        if (selection.count > 0) {
          selection.clear()
          return
        }
        setFocusedIndex(-1)
      },
    },
    { enabled: gridEnabled },
  )

  // Ctrl/Cmd+A selects the whole list. The shared shortcut hook ignores modifier
  // chords by design, so this one is bound separately — and only when the grid
  // owns the page, so it never steals the browser's select-all from a text field.
  useEffect(() => {
    if (!gridEnabled) {
      return
    }
    function onKeyDown(event: KeyboardEvent) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey) {
        return
      }
      if (event.key.toLowerCase() !== 'a' || isTypingElement(event.target)) {
        return
      }
      event.preventDefault()
      selection.enable()
      selection.selectMany(orderedKeys)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [gridEnabled, selection, orderedKeys])

  const selectedFaces = useMemo(
    () => items.filter((item) => selection.selected.has(outlierKey(item.face))).map((i) => i.face),
    [items, selection.selected],
  )

  const handleBulkUnassign = useCallback(() => {
    review.unassignMany(selectedFaces)
    selection.disable()
  }, [review, selectedFaces, selection])

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('outliersPage.title')}</h1>
      <p className="text-secondary">{t('outliersPage.subtitle')}</p>

      <OutlierControls
        subjects={subjects}
        subjectsLoading={subjectsLoading}
        subjectUid={subjectUid}
        thresholdPercent={thresholdPercent}
        onSubjectChange={setSubjectUid}
        onThresholdChange={setThresholdPercent}
      />

      {review.actionError !== null && (
        <Alert variant="danger" dismissible onClose={review.dismissError}>
          {review.actionError.kind === 'unassign' && t('outliersPage.error.unassign')}
          {review.actionError.kind === 'confirm' && t('outliersPage.error.confirm')}
          {review.actionError.kind === 'bulk' &&
            t('outliersPage.error.bulk', { count: review.actionError.count })}
        </Alert>
      )}

      {state.status === 'idle' && (
        <EmptyState
          icon={<Icon name="person-bounding-box" />}
          title={t('outliersPage.idle.title')}
          hint={t('outliersPage.idle.hint')}
        />
      )}

      {state.status === 'loading' && (
        <div className="d-flex align-items-center gap-2 py-5 justify-content-center text-secondary">
          <Spinner animation="border" role="status" size="sm" />
          <span>{t('outliersPage.loading')}</span>
        </div>
      )}

      {state.status === 'error' && (
        <ErrorState title={t('outliersPage.error.load')} onRetry={reloadOutliers} />
      )}

      {result !== null && (
        <>
          <div className="d-flex flex-wrap align-items-start justify-content-between gap-3">
            <div className="flex-grow-1">
              <OutlierStats result={result} shown={items.length} />
            </div>
            {/* The library's stepper, driving the count every review tool shares —
                see REVIEW_GRID_SCOPE for why it is not the library's number. */}
            {items.length > 0 && <GridDensityControl scope={REVIEW_GRID_SCOPE} />}
          </div>

          {selection.active && (
            <SelectionBar count={selection.count} onCancel={selection.disable}>
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={items.length === 0}
                onClick={() => {
                  selection.selectMany(orderedKeys)
                }}
              >
                {t('outliersPage.selectAll')}
              </Button>
              <Button
                variant="danger"
                size="sm"
                disabled={selection.count === 0 || review.bulkState.running}
                onClick={handleBulkUnassign}
              >
                {t('outliersPage.bulkUnassign', { count: selection.count })}
              </Button>
            </SelectionBar>
          )}

          {review.bulkState.running && (
            <Alert variant="info" className="py-2 small" data-testid="outlier-bulk-progress">
              <div className="d-flex align-items-center gap-2 flex-wrap">
                <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
                <span>
                  {t('outliersPage.bulkProgress', {
                    current: review.bulkState.current,
                    total: review.bulkState.total,
                  })}
                </span>
                <Button variant="outline-light" size="sm" onClick={review.cancelBulk}>
                  {t('outliersPage.bulkCancel')}
                </Button>
              </div>
            </Alert>
          )}

          {items.length === 0 ? (
            <EmptyState
              icon={<Icon name="person-check" />}
              title={t('outliersPage.empty.title')}
              hint={t('outliersPage.empty.hint')}
            />
          ) : (
            <div
              ref={gridRef}
              className="d-grid"
              data-density={density}
              /* The user picks the column count, exactly as in the library — the
                 grid used to be a fixed `minmax(16rem, 1fr)`, which is now only
                 the width the count is *seeded* from on a device's first visit. */
              style={{
                gridTemplateColumns: gridTemplateColumns(density),
                gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
              }}
            >
              {items.map((item, index) => (
                <OutlierCard
                  key={outlierKey(item.face)}
                  item={item}
                  subjectName={subjectName}
                  focused={index === focusedIndex}
                  selectable={selection.active}
                  selected={selection.selected.has(outlierKey(item.face))}
                  onSelect={(shiftKey) => {
                    toggleSelect(index, shiftKey)
                  }}
                  onUnassign={() => {
                    review.unassign(item.face)
                  }}
                  onConfirm={() => {
                    review.confirm(item.face)
                  }}
                  onEnlarge={() => {
                    lightbox.open(index)
                  }}
                />
              ))}
            </div>
          )}
        </>
      )}

      {enlarged !== null && (
        <ReviewLightbox
          stage={{
            photoUid: enlarged.face.photo_uid,
            fileWidth: enlarged.face.width,
            fileHeight: enlarged.face.height,
            orientation: enlarged.face.orientation,
            // The whole frame, not the card's crop: the crop is what raised the
            // doubt, the surroundings are what settle it.
            size: OUTLIER_LIGHTBOX_SIZE,
            bbox: enlarged.face.bbox,
            href: `/photos/${enlarged.face.photo_uid}`,
            alt: t('outliersPage.card.photoAlt'),
          }}
          title={t('outliersPage.card.distance', {
            percent: distancePercent(enlarged.face.distance),
          })}
          onClose={lightbox.close}
          onPrev={lightbox.prev}
          onNext={lightbox.next}
          hasPrev={lightbox.hasPrev}
          hasNext={lightbox.hasNext}
        >
          {isActionable(enlarged) ? (
            <>
              <Button
                variant="outline-danger"
                className="d-flex align-items-center gap-1"
                disabled={!canUnassign(enlarged.face)}
                title={canUnassign(enlarged.face) ? undefined : t('outliersPage.card.noMarker')}
                onClick={() => {
                  review.unassign(enlarged.face)
                }}
              >
                <Icon name="check-lg" />
                {t('outliersPage.card.unassign')}
              </Button>
              <Button
                variant="outline-light"
                className="d-flex align-items-center gap-1"
                onClick={() => {
                  review.confirm(enlarged.face)
                }}
              >
                <Icon name="x-lg" />
                {t('outliersPage.card.confirm', { name: subjectName })}
              </Button>
            </>
          ) : (
            <span className="text-secondary">
              {enlarged.status === 'removed'
                ? t('outliersPage.card.removed')
                : t('outliersPage.card.confirmed')}
            </span>
          )}
        </ReviewLightbox>
      )}
    </>
  )
}
