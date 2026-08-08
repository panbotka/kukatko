import { type ReactNode, useCallback, useEffect } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Spinner from 'react-bootstrap/Spinner'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon, type IconName } from '../components/Icon'
import { KeyboardShortcutsHelp } from '../components/KeyboardShortcutsHelp'
import { REVIEW_PREVIEW_SIZE, ReviewPhoto } from '../components/review/ReviewPhoto'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useReviewGame } from '../hooks/useReviewGame'
import { isTypingElement } from '../lib/ratingHotkeys'
import { thumbUrl } from '../services/photos'
import {
  REASON_NO_LABELS,
  REASON_NO_PEOPLE,
  REASON_NO_SOURCES,
  REVIEW_SOURCES,
  type ReviewQuestion,
  type ReviewSource,
} from '../services/review'

import '../components/review/review.css'

/** How many upcoming photos are decoded ahead of the player. */
const PRELOAD_AHEAD = 4

/**
 * The path of a photo's own detail page. The game's single place for it: the
 * anchor on the stage and the `o` shortcut are handed the same string, so the
 * link the player copies and the tab the key opens can never disagree.
 */
function photoDetailPath(uid: string): string {
  return `/photos/${encodeURIComponent(uid)}`
}

/**
 * The i18n label key per source, an explicit map (rather than a template
 * literal) so a typo is a compile error and the typed `t` accepts it — the same
 * pattern the leaderboard's window toggle uses.
 */
const SOURCE_LABEL_KEYS = {
  both: 'review.source.both',
  people: 'review.source.people',
  labels: 'review.source.labels',
} as const

/** The glyph per source; decorative, the label carries the meaning. */
const SOURCE_ICONS: Record<ReviewSource, IconName> = {
  both: 'ui-checks',
  people: 'people',
  labels: 'tags',
}

/**
 * Narrows an arbitrary query-string value to a supported source, defaulting to
 * both. An unknown value (an old bookmark, a hand-edited URL) degrades to the
 * default instead of erroring — the backend rejects it, the URL should not
 * strand the player on an error screen.
 */
function parseSource(raw: string | null): ReviewSource {
  return REVIEW_SOURCES.find((candidate) => candidate === raw) ?? 'both'
}

/** The three-state source toggle: ask about people, labels, or both. */
function SourceToggle({
  source,
  onSelect,
}: {
  source: ReviewSource
  onSelect: (next: ReviewSource) => void
}) {
  const { t } = useTranslation()
  return (
    <ButtonGroup size="sm" aria-label={t('review.source.label')} className="review-game__source">
      {REVIEW_SOURCES.map((candidate) => {
        const isActive = candidate === source
        const label = t(SOURCE_LABEL_KEYS[candidate])
        return (
          <Button
            key={candidate}
            variant={isActive ? 'secondary' : 'outline-secondary'}
            active={isActive}
            aria-pressed={isActive}
            // The label is the button's only text on a phone (it is hidden
            // below `md` to keep the row on one line), so it has to be the
            // accessible name as well, not just decoration next to the glyph.
            aria-label={label}
            title={label}
            data-testid={`review-source-${candidate}`}
            className="d-inline-flex align-items-center gap-2 kukatko-tap-target"
            onClick={() => {
              onSelect(candidate)
            }}
          >
            <Icon name={SOURCE_ICONS[candidate]} />
            <span className="d-none d-md-inline">{label}</span>
          </Button>
        )
      })}
    </ButtonGroup>
  )
}

/**
 * The nothing-to-ask bodies, told apart by the reason the backend gave. They
 * differ because the way out differs: an empty library needs people or labels
 * created, an empty *chosen* source needs the toggle moved, and an exhausted
 * band needs only patience. A single "no results" would send the player hunting
 * a bug that is not there.
 */
function EmptyQueue({
  reason,
  source,
  onSelect,
  onRetry,
}: {
  reason: string | undefined
  source: ReviewSource
  onSelect: (next: ReviewSource) => void
  onRetry: () => void
}) {
  const { t } = useTranslation()
  if (reason === REASON_NO_SOURCES) {
    return (
      <div className="review-game__center" data-testid="review-empty-library">
        <EmptyState
          title={t('review.empty.libraryTitle')}
          hint={t('review.empty.libraryHint')}
          action={
            <div className="d-flex gap-2 justify-content-center flex-wrap">
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
  }
  if (reason === REASON_NO_PEOPLE || reason === REASON_NO_LABELS) {
    const noPeople = reason === REASON_NO_PEOPLE
    return (
      <div className="review-game__center" data-testid="review-empty-source">
        <EmptyState
          title={t(noPeople ? 'review.empty.noPeopleTitle' : 'review.empty.noLabelsTitle')}
          hint={t(noPeople ? 'review.empty.noPeopleHint' : 'review.empty.noLabelsHint')}
          action={
            <div className="d-flex gap-2 justify-content-center flex-wrap">
              <Link to={noPeople ? '/people' : '/labels'} className="btn btn-sm btn-outline-light">
                {t(noPeople ? 'review.empty.people' : 'review.empty.labels')}
              </Link>
              <Button
                variant="outline-light"
                size="sm"
                onClick={() => {
                  onSelect(noPeople ? 'labels' : 'people')
                }}
              >
                {t(noPeople ? 'review.empty.askLabels' : 'review.empty.askPeople')}
              </Button>
            </div>
          }
        />
      </div>
    )
  }
  return (
    <div className="review-game__center" data-testid="review-empty-queue">
      <EmptyState
        title={t('review.empty.queueTitle')}
        hint={source === 'both' ? t('review.empty.queueHint') : t('review.empty.queueHintScoped')}
        action={
          <div className="d-flex gap-2 justify-content-center flex-wrap">
            {source !== 'both' && (
              <Button
                variant="outline-light"
                size="sm"
                onClick={() => {
                  onSelect('both')
                }}
              >
                {t('review.empty.askBoth')}
              </Button>
            )}
            <Button variant="outline-light" size="sm" onClick={onRetry}>
              {t('review.empty.checkAgain')}
            </Button>
          </div>
        }
      />
    </div>
  )
}

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
 * (← no, → yes, Space/↓ skip, y/n, z undo, o open the photo in a new tab, Esc
 * leave); the buttons are the fallback and the touch interface. The way out to
 * the photo is a real anchor on the stage as well, because the point of it is
 * being able to copy the URL. Answers are optimistic and the next
 * question is always already in memory, so the rhythm is never broken by a
 * spinner (see {@link useReviewGame}). Rendered outside the layout shell so
 * nothing competes with the photo.
 *
 * What the game asks about is the player's choice — people, labels or both —
 * and it lives in the `source` query parameter, not in component state, so it
 * survives a reload and a shared link like every other view state here.
 */
export function ReviewPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('nav.review'))
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const source = parseSource(searchParams.get('source'))
  const game = useReviewGame(source)
  const { prime } = useImagePreloader()

  /**
   * Writes the chosen source into the URL. It replaces the entry rather than
   * pushing one: Esc leaves the game with `navigate(-1)`, so a stacked toggle
   * history would turn "leave" into "switch back" instead.
   */
  const selectSource = useCallback(
    (next: ReviewSource) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev)
          params.set('source', next)
          return params
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

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

  const question = game.current

  /**
   * The keyboard twin of the anchor in {@link ReviewPhoto}: opens the photo
   * under question in a new tab, same path and same `noopener`. It answers
   * nothing and touches the queue not at all — the card the player is looking at
   * is still there when they come back. A new tab, not this one, because the
   * queue lives in memory and leaving would drop the whole run.
   */
  const openPhoto = useCallback(() => {
    if (question === undefined) {
      return
    }
    window.open(photoDetailPath(question.photo.uid), '_blank', 'noopener,noreferrer')
  }, [question])

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
    // `o` for open. It is deliberately not one of the answer keys' neighbours,
    // and it is an addition, not a replacement — the game runs on touch too,
    // where the corner anchor is the only way there.
    o: openPhoto,
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
            href={photoDetailPath(question.photo.uid)}
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
  } else {
    // The backend distinguishes an empty library, an empty *chosen* source and
    // an exhausted band; EmptyQueue turns each into its own way out.
    body = (
      <EmptyQueue
        reason={game.reason}
        source={source}
        onSelect={selectSource}
        onRetry={game.retryLoad}
      />
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
        {/* A direct child of the header, not part of the button pair, so the
            narrow-screen rule can drop it onto its own line. */}
        <SourceToggle source={source} onSelect={selectSource} />
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
