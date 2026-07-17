import { type ReactNode, useCallback, useEffect } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { KeyboardShortcutsHelp } from '../components/KeyboardShortcutsHelp'
import { REVIEW_PREVIEW_SIZE, ReviewPhoto } from '../components/review/ReviewPhoto'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useReviewGame } from '../hooks/useReviewGame'
import { isTypingElement } from '../lib/ratingHotkeys'
import { thumbUrl } from '../services/photos'
import { REASON_NO_SOURCES, type ReviewQuestion } from '../services/review'

import '../components/review/review.css'

/** How many upcoming photos are decoded ahead of the player. */
const PRELOAD_AHEAD = 4

/** The confidence context under the question: a quiet percentage plus bar. */
function ConfidenceHint({ confidence }: { confidence: number }) {
  const { t } = useTranslation()
  const percent = Math.round(confidence * 100)
  return (
    <p className="review-game__confidence">
      <span>{t('review.confidence', { percent })}</span>
      <span className="review-game__confidence-bar" aria-hidden="true">
        <span style={{ width: `${String(percent)}%` }} />
      </span>
    </p>
  )
}

/** The question sentence with the person/label name as the emphasised part. */
function QuestionText({ question }: { question: ReviewQuestion }) {
  const name =
    question.kind === 'face' ? (question.subject?.name ?? '') : (question.label?.name ?? '')
  return (
    <h1 className="review-game__question" data-testid="review-question" aria-live="polite">
      <Trans
        i18nKey={question.kind === 'face' ? 'review.question.face' : 'review.question.label'}
        values={{ name }}
        components={{ strong: <strong className="review-game__name" /> }}
      />
    </h1>
  )
}

/**
 * The review game (`/review`, editors only): a fullscreen one-question-at-a-time
 * card flow — one plain-language question, the photo under it as large as the
 * room left over allows, and Ano / Ne / Nevím. The question and the buttons
 * always fit; the photo is what shrinks. The keyboard is the primary interface
 * (← no, → yes, Space/↓ skip, y/n, z undo, Esc leave); the buttons are the
 * fallback and the touch interface. Answers are optimistic and the next
 * question is always already in memory, so the rhythm is never broken by a
 * spinner (see {@link useReviewGame}). Rendered outside the layout shell so
 * nothing competes with the photo.
 */
export function ReviewPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const game = useReviewGame()
  const { prime } = useImagePreloader()

  // Decode the next few photos ahead of the player: the card after an answer
  // must paint instantly, and a batch refill must be invisible.
  useEffect(() => {
    prime(
      game.pending
        .slice(0, PRELOAD_AHEAD)
        .map((question) => thumbUrl(question.photo.uid, REVIEW_PREVIEW_SIZE)),
    )
  }, [prime, game.pending])

  const exit = useCallback(() => {
    if (window.history.length > 1) {
      void navigate(-1)
      return
    }
    void navigate('/')
  }, [navigate])

  useKeyboardShortcuts({
    ArrowLeft: () => {
      game.answer('no')
    },
    n: () => {
      game.answer('no')
    },
    ArrowRight: () => {
      game.answer('yes')
    },
    y: () => {
      game.answer('yes')
    },
    ' ': () => {
      game.answer('skip')
    },
    ArrowDown: () => {
      game.answer('skip')
    },
    z: game.undo,
    Escape: () => {
      // Leave Escape to a react-bootstrap modal (the shortcuts help) when one
      // is open — it closes itself.
      if (document.querySelector('.modal.show') === null) {
        exit()
      }
    },
  })

  // Ctrl/Cmd+Z as the familiar undo chord. The shared shortcut hook ignores
  // modifier chords by design, so this one is bound separately.
  const { undo } = game
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey) {
        return
      }
      if (event.key.toLowerCase() !== 'z' || isTypingElement(event.target)) {
        return
      }
      event.preventDefault()
      undo()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [undo])

  const question = game.current
  const total = game.answered + game.remaining
  const progressPct = total > 0 ? Math.min(100, Math.round((game.answered / total) * 100)) : 0

  let body: ReactNode
  if (question !== undefined) {
    body = (
      <>
        <section className="review-game__prompt">
          <QuestionText question={question} />
          {question.kind === 'face' && question.bbox !== undefined && (
            <p className="review-game__face-hint">{t('review.faceHint')}</p>
          )}
          <ConfidenceHint confidence={question.confidence} />
        </section>
        <main className="review-game__stage">
          <ReviewPhoto
            photo={question.photo}
            bbox={question.kind === 'face' ? question.bbox?.relative : undefined}
            alt={t('review.photoAlt')}
          />
        </main>
        <footer className="review-game__actions">
          <Button
            variant="outline-danger"
            size="lg"
            onClick={() => {
              game.answer('no')
            }}
          >
            <Icon name="x-lg" className="me-2" />
            {t('review.actions.no')}
            <kbd className="review-game__kbd">←</kbd>
          </Button>
          <Button
            variant="outline-secondary"
            size="lg"
            onClick={() => {
              game.answer('skip')
            }}
          >
            {t('review.actions.skip')}
            <kbd className="review-game__kbd">{t('review.keys.space')}</kbd>
          </Button>
          <Button
            variant="success"
            size="lg"
            onClick={() => {
              game.answer('yes')
            }}
          >
            <Icon name="check-lg" className="me-2" />
            {t('review.actions.yes')}
            <kbd className="review-game__kbd">→</kbd>
          </Button>
        </footer>
      </>
    )
  } else if (game.fetching || (!game.exhausted && !game.loadError)) {
    // The only unavoidable wait: the first batch (or a slow refill the player
    // outran). Everything else is prefetched.
    body = (
      <div className="review-game__center">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('review.loading')}</span>
        </Spinner>
      </div>
    )
  } else if (game.loadError) {
    body = (
      <div className="review-game__center" data-testid="review-load-error">
        <Alert variant="danger" className="d-flex align-items-center gap-3 mb-0">
          <span>{t('review.errors.load')}</span>
          <Button variant="outline-light" size="sm" onClick={game.retryLoad}>
            {t('review.errors.retry')}
          </Button>
        </Alert>
      </div>
    )
  } else if (game.reason === REASON_NO_SOURCES) {
    // The backend reports an empty *library* (no named people, no labels)
    // separately from an empty queue — a generic "no results" here would send
    // the user hunting a bug that is not there.
    body = (
      <div className="review-game__center" data-testid="review-empty-library">
        <EmptyState
          title={t('review.empty.libraryTitle')}
          hint={t('review.empty.libraryHint')}
          action={
            <div className="d-flex gap-2 justify-content-center">
              <Link to="/people" className="btn btn-sm btn-outline-light">
                {t('review.empty.people')}
              </Link>
              <Link to="/labels" className="btn btn-sm btn-outline-light">
                {t('review.empty.labels')}
              </Link>
            </div>
          }
        />
      </div>
    )
  } else {
    body = (
      <div className="review-game__center" data-testid="review-empty-queue">
        <EmptyState
          title={t('review.empty.queueTitle')}
          hint={t('review.empty.queueHint')}
          action={
            <Button variant="outline-light" size="sm" onClick={game.retryLoad}>
              {t('review.empty.checkAgain')}
            </Button>
          }
        />
      </div>
    )
  }

  return (
    <div className="review-game">
      <header className="review-game__top">
        <div className="d-flex align-items-center gap-2">
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={exit}
            aria-label={t('review.close')}
            title={t('review.close')}
            className="d-inline-flex align-items-center justify-content-center kukatko-tap-target"
          >
            <Icon name="x-lg" />
          </Button>
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={game.undo}
            disabled={game.lastAnswer === null || game.undoing}
            className="d-inline-flex align-items-center gap-2 kukatko-tap-target"
            data-testid="review-undo"
          >
            <Icon name="arrow-counterclockwise" />
            <span className="d-none d-sm-inline">{t('review.actions.undo')}</span>
            <kbd className="review-game__kbd">z</kbd>
          </Button>
        </div>
        <div className="review-game__progress-text" data-testid="review-progress">
          {t('review.progress.answered', { count: game.answered })}
          {game.remaining > 0 && (
            <span className="text-secondary">
              {' · '}
              {t('review.progress.remaining', { count: game.remaining })}
            </span>
          )}
        </div>
        <KeyboardShortcutsHelp />
      </header>
      <div className="review-game__progressbar" aria-hidden="true">
        <div style={{ width: `${String(progressPct)}%` }} />
      </div>
      {game.failed.length > 0 && (
        <Alert variant="danger" className="review-game__alert" data-testid="review-answer-errors">
          <div className="d-flex align-items-center flex-wrap gap-2">
            <span>{t('review.errors.answer', { count: game.failed.length })}</span>
            <Button variant="outline-light" size="sm" onClick={game.retryFailed}>
              {t('review.errors.retryAnswers')}
            </Button>
            <Button variant="outline-light" size="sm" onClick={game.dismissFailed}>
              {t('review.errors.dismiss')}
            </Button>
          </div>
        </Alert>
      )}
      {game.undoError && (
        <Alert variant="danger" className="review-game__alert" data-testid="review-undo-error">
          <div className="d-flex align-items-center flex-wrap gap-2">
            <span>{t('review.errors.undo')}</span>
            <Button variant="outline-light" size="sm" onClick={game.undo}>
              {t('review.errors.retry')}
            </Button>
          </div>
        </Alert>
      )}
      {body}
    </div>
  )
}
