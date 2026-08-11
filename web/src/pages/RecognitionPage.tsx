import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import ProgressBar from 'react-bootstrap/ProgressBar'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { PersonSweepCard } from '../components/recognition/PersonSweepCard'
import { CandidateDecisions } from '../components/review/CandidateDecisions'
import { candidateStage } from '../components/review/candidateStage'
import { ReviewLightbox } from '../components/review/ReviewLightbox'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useLightbox } from '../hooks/useLightbox'
import { useSweepReview } from '../hooks/useSweepReview'
import { distanceToPercent } from '../lib/faceThreshold'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import {
  clampConfidencePercent,
  focusSequence,
  nextFocusKey,
  personActionableCount,
  SWEEP_DEFAULT_LIMIT,
  SWEEP_DEFAULT_PERCENT,
  SWEEP_MAX_PERCENT,
  SWEEP_MIN_PERCENT,
  SWEEP_STEP_PERCENT,
} from '../lib/recognitionSweep'

/**
 * RecognitionPage is the recognition sweep: scan every named person for confident
 * matches among unnamed faces at once, and work through the results grouped by person,
 * a list that visibly shrinks as it is cleared. It is editor-only and reuses the
 * /faces review card and keyboard flow. See docs/FRONTEND.md.
 */
export function RecognitionPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('recognition.title'))
  const [searchParams, setSearchParams] = useSearchParams()

  const [confidence, setConfidence] = useState(() =>
    clampConfidencePercent(Number(searchParams.get('confidence') ?? SWEEP_DEFAULT_PERCENT)),
  )
  const [limit, setLimit] = useState(() =>
    Math.max(0, Math.trunc(Number(searchParams.get('limit') ?? SWEEP_DEFAULT_LIMIT)) || 0),
  )
  const [focusedKey, setFocusedKey] = useState<string | null>(null)

  const review = useSweepReview()
  const scanning = review.phase === 'scanning'

  const startScan = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('confidence', String(confidence))
        next.set('limit', String(limit))
        return next
      },
      { replace: true },
    )
    setFocusedKey(null)
    review.scan({ confidence, limit })
  }, [confidence, limit, review, setSearchParams])

  const sequence = useMemo(() => focusSequence(review.people), [review.people])
  const liveActionable = useMemo(
    () => review.people.reduce((sum, person) => sum + personActionableCount(person), 0),
    [review.people],
  )

  const { density } = useGridDensity(REVIEW_GRID_SCOPE)
  // The overlay belongs to **one person's block**, not to the whole sweep: ←/→
  // stepping past Alice's last candidate into Bob's would silently change the
  // question the buttons answer. A person cleared away empties this list, which
  // is exactly when the overlay should close — `useLightbox` does that itself.
  const [enlargedSubject, setEnlargedSubject] = useState<string | null>(null)
  const enlargedItems = useMemo(
    () => review.people.find((person) => person.subject.uid === enlargedSubject)?.items ?? [],
    [review.people, enlargedSubject],
  )
  const lightbox = useLightbox(enlargedItems)
  const enlarged = lightbox.item

  // Keep the focused card in view as the keyboard moves the selection.
  useEffect(() => {
    if (focusedKey === null) {
      return
    }
    document.querySelector('[data-focused="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [focusedKey, review.people])

  const move = useCallback(
    (delta: number) => {
      if (sequence.length === 0) {
        return
      }
      const index = sequence.findIndex((entry) => entry.key === focusedKey)
      const base = index < 0 ? 0 : Math.min(Math.max(index + delta, 0), sequence.length - 1)
      setFocusedKey(sequence[base].key)
    },
    [sequence, focusedKey],
  )

  const confirmFocused = useCallback(() => {
    const entry = sequence.find((item) => item.key === focusedKey)
    if (entry === undefined) {
      return
    }
    setFocusedKey(nextFocusKey(sequence, focusedKey))
    review.confirm(entry.subjectUid, entry.candidate)
  }, [sequence, focusedKey, review])

  const rejectFocused = useCallback(() => {
    const entry = sequence.find((item) => item.key === focusedKey)
    if (entry === undefined) {
      return
    }
    setFocusedKey(nextFocusKey(sequence, focusedKey))
    review.reject(entry.subjectUid, entry.candidate)
  }, [sequence, focusedKey, review])

  useKeyboardShortcuts(
    {
      ArrowRight: () => {
        move(1)
      },
      l: () => {
        move(1)
      },
      ArrowDown: () => {
        move(1)
      },
      j: () => {
        move(1)
      },
      ArrowLeft: () => {
        move(-1)
      },
      h: () => {
        move(-1)
      },
      ArrowUp: () => {
        move(-1)
      },
      k: () => {
        move(-1)
      },
      y: confirmFocused,
      Enter: confirmFocused,
      n: rejectFocused,
    },
    {
      // The overlay owns the keyboard while it is up: its arrows step photos, and
      // the page's would move the highlight out from under it.
      enabled: review.people.length > 0 && review.confirmAll === null && !lightbox.isOpen,
    },
  )

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('recognition.title')}</h1>
      <p className="text-secondary">{t('recognition.subtitle')}</p>

      <Form
        className="mb-4"
        onSubmit={(event) => {
          event.preventDefault()
          if (!scanning) {
            startScan()
          }
        }}
      >
        <Row className="g-3 align-items-end">
          <Col md={6} lg={5}>
            <Form.Label htmlFor="sweep-confidence" className="d-flex justify-content-between mb-1">
              <span>{t('recognition.form.confidenceLabel')}</span>
              <span className="fw-semibold">
                {t('recognition.form.confidenceValue', { percent: confidence })}
              </span>
            </Form.Label>
            <Form.Range
              id="sweep-confidence"
              min={SWEEP_MIN_PERCENT}
              max={SWEEP_MAX_PERCENT}
              step={SWEEP_STEP_PERCENT}
              value={confidence}
              disabled={scanning}
              onChange={(event) => {
                setConfidence(clampConfidencePercent(Number(event.target.value)))
              }}
            />
            <div className="d-flex justify-content-between small text-secondary">
              <span>{t('recognition.form.confidenceMore')}</span>
              <span>{t('recognition.form.confidenceBetter')}</span>
            </div>
          </Col>

          <Col xs={6} md={3} lg={2}>
            <Form.Label htmlFor="sweep-limit">{t('recognition.form.limitLabel')}</Form.Label>
            <Form.Control
              id="sweep-limit"
              type="number"
              min={0}
              value={limit}
              disabled={scanning}
              onChange={(event) => {
                setLimit(Math.max(0, Math.trunc(Number(event.target.value)) || 0))
              }}
            />
            <Form.Text>{t('recognition.form.limitHint')}</Form.Text>
          </Col>

          <Col xs={6} md={3} lg={2}>
            {scanning ? (
              <Button
                type="button"
                variant="outline-secondary"
                onClick={review.cancel}
                className="d-flex align-items-center gap-2"
              >
                <Icon name="x-lg" />
                {t('recognition.form.cancel')}
              </Button>
            ) : (
              <Button type="submit" variant="primary" className="d-flex align-items-center gap-2">
                <Icon name="search" />
                {t('recognition.form.scan')}
              </Button>
            )}
          </Col>
        </Row>
      </Form>

      {scanning && review.progress !== null && (
        <div className="mb-4" data-testid="sweep-progress">
          <div className="d-flex justify-content-between small mb-1">
            <span>{t('recognition.progress.scanning', { name: review.progress.name })}</span>
            <span className="text-secondary">
              {t('recognition.progress.count', {
                current: review.progress.scanned,
                total: review.progress.total,
              })}
            </span>
          </div>
          <ProgressBar
            now={review.progress.scanned}
            max={Math.max(1, review.progress.total)}
            animated
          />
        </div>
      )}

      {review.summary !== null && (
        <div
          className="d-flex flex-wrap align-items-center gap-3 mb-4 text-secondary"
          data-testid="sweep-stats"
        >
          <span>{t('recognition.stats.actionable', { count: liveActionable })}</span>
          <span>{t('recognition.stats.people', { count: review.people.length })}</span>
          <span>
            {t('recognition.stats.alreadyDone', { count: review.summary.total_already_done })}
          </span>
          {review.summary.capped && (
            <span className="text-warning">
              {t('recognition.stats.capped', {
                shown: review.summary.people_scanned,
                total: review.summary.subjects_total,
              })}
            </span>
          )}
          {/* The library's stepper on the review tools' shared count — every
              person's grid below moves together, which is the point. */}
          {review.people.length > 0 && (
            <span className="ms-auto">
              <GridDensityControl scope={REVIEW_GRID_SCOPE} />
            </span>
          )}
        </div>
      )}

      {review.actionError !== null && (
        <Alert variant="danger" dismissible onClose={review.dismissError}>
          {review.actionError.kind === 'scan' && t('recognition.error.scan')}
          {review.actionError.kind === 'confirm' && t('recognition.error.confirm')}
          {review.actionError.kind === 'reject' && t('recognition.error.reject')}
          {review.actionError.kind === 'confirmAll' &&
            t('recognition.error.confirmAll', { count: review.actionError.count })}
        </Alert>
      )}

      {review.phase === 'idle' && (
        <EmptyState
          icon={<Icon name="person-check" />}
          title={t('recognition.idle.title')}
          hint={t('recognition.idle.hint')}
        />
      )}

      {review.phase === 'done' && review.people.length === 0 && review.actionError === null && (
        <EmptyState
          icon={<Icon name="check-lg" />}
          title={t('recognition.empty.title')}
          hint={t('recognition.empty.hint')}
        />
      )}

      {review.people.map((person) => (
        <PersonSweepCard
          key={person.subject.uid}
          person={person}
          focusedKey={focusedKey}
          running={
            review.confirmAll?.subjectUid === person.subject.uid
              ? { current: review.confirmAll.current, total: review.confirmAll.total }
              : null
          }
          onConfirm={(candidate) => {
            review.confirm(person.subject.uid, candidate)
          }}
          onReject={(candidate) => {
            review.reject(person.subject.uid, candidate)
          }}
          onConfirmAll={() => {
            review.confirmAllForPerson(person.subject.uid)
          }}
          onCancelConfirmAll={review.cancelConfirmAll}
          density={density}
          onEnlarge={(index) => {
            setEnlargedSubject(person.subject.uid)
            lightbox.open(index)
          }}
        />
      ))}

      {enlarged !== null && enlargedSubject !== null && (
        <ReviewLightbox
          stage={candidateStage(enlarged.candidate, t('faceSearch.card.photoAlt'))}
          title={t('faceSearch.card.match', {
            percent: distanceToPercent(enlarged.candidate.distance),
          })}
          onClose={lightbox.close}
          onPrev={lightbox.prev}
          onNext={lightbox.next}
          hasPrev={lightbox.hasPrev}
          hasNext={lightbox.hasNext}
        >
          <CandidateDecisions
            item={enlarged}
            onConfirm={() => {
              review.confirm(enlargedSubject, enlarged.candidate)
            }}
            onReject={() => {
              review.reject(enlargedSubject, enlarged.candidate)
            }}
          />
        </ReviewLightbox>
      )}
    </>
  )
}
