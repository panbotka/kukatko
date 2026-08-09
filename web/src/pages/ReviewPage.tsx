import { type CSSProperties, type ReactNode, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Spinner from 'react-bootstrap/Spinner'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon, type IconName } from '../components/Icon'
import { KeyboardShortcutsHelp } from '../components/KeyboardShortcutsHelp'
import { BreatherCard, RevealCard } from '../components/review/ReviewBreather'
import { ReviewDuplicate } from '../components/review/ReviewDuplicate'
import { ReviewOutlier } from '../components/review/ReviewOutlier'
import { REVIEW_PREVIEW_SIZE, ReviewPhoto } from '../components/review/ReviewPhoto'
import {
  MilestoneBurst,
  RoundSummaryCard,
  SessionSummaryCard,
} from '../components/review/ReviewSummary'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useReviewGame } from '../hooks/useReviewGame'
import { useReviewStreak } from '../hooks/useReviewStreak'
import { useReviewSwipe } from '../hooks/useReviewSwipe'
import { type SwipeVerdict } from '../lib/gestures'
import { isTypingElement } from '../lib/ratingHotkeys'
import { cardPhoto, type ReviewCard } from '../lib/reviewRounds'
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

/** How long a milestone celebration stays up before it fades on its own (ms). */
const MILESTONE_MS = 2600

/** A combo is only worth showing once it is one: two in a row, not one. */
const COMBO_FLOOR = 2

/** How far the card is rotated per pixel of horizontal drag (deg/px). */
const DRAG_TILT = 1 / 24

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

/** The answer a swipe is heading for, named on the card before the finger lifts. */
const VERDICT_LABEL_KEYS = {
  yes: 'review.actions.yes',
  no: 'review.actions.no',
  skip: 'review.actions.skip',
} as const

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

/**
 * The i18n key of each kind's question sentence, an explicit map so a typo is a
 * compile error and the typed `t` accepts it.
 */
const QUESTION_KEYS = {
  face: 'review.question.face',
  label: 'review.question.label',
  place: 'review.question.place',
  duplicate: 'review.question.duplicate',
  outlier: 'review.question.outlier',
} as const

/**
 * What each kind of question emphasises: the person, the label, the place. The
 * duplicate check names nothing — the two photos are the question — so its
 * sentence carries no interpolated name.
 */
function questionName(question: ReviewQuestion): string {
  switch (question.kind) {
    case 'face':
    case 'outlier':
      return question.subject?.name ?? ''
    case 'label':
      return question.label?.name ?? ''
    case 'place':
      return question.place?.name ?? ''
    case 'duplicate':
      return ''
  }
}

/** The question sentence with the person/label/place name as the emphasised part. */
function QuestionText({ question }: { question: ReviewQuestion }) {
  return (
    <h1 className="review-game__question" data-testid="review-question" aria-live="polite">
      <Trans
        i18nKey={QUESTION_KEYS[question.kind]}
        values={{ name: questionName(question) }}
        components={{ strong: <strong className="review-game__name" /> }}
      />
    </h1>
  )
}

/**
 * The quiet line under the question that says what to look at. Each kind has its
 * own, because each stage shows something different — a rectangle on a photo, two
 * photos, a face crop — and the place check has none at all: the photo *is* the
 * whole of it.
 */
function QuestionHint({ question }: { question: ReviewQuestion }) {
  const { t } = useTranslation()
  let hint: string | null = null
  if (question.kind === 'face' && question.bbox !== undefined) {
    hint = t('review.faceHint')
  } else if (question.kind === 'duplicate') {
    hint = t('review.duplicate.hint')
  } else if (question.kind === 'outlier') {
    hint = t('review.outlier.hint')
  }
  if (hint === null) {
    return null
  }
  return <p className="review-game__face-hint">{hint}</p>
}

/**
 * The stage under the question, one per kind: two photos for the duplicate
 * check, the face crop plus its context for the outlier check, the full frame
 * (with the face rectangle, where there is one) for everything else.
 */
function QuestionStage({ question, alt }: { question: ReviewQuestion; alt: string }) {
  const { t } = useTranslation()
  if (question.kind === 'duplicate' && question.other !== undefined) {
    return <ReviewDuplicate photo={question.photo} other={question.other} href={photoDetailPath} />
  }
  if (question.kind === 'outlier' && question.bbox !== undefined) {
    return (
      <ReviewOutlier
        photo={question.photo}
        bbox={question.bbox.relative}
        href={photoDetailPath(question.photo.uid)}
        alt={t('review.outlier.faceAlt')}
      />
    )
  }
  return (
    <ReviewPhoto
      photo={question.photo}
      href={photoDetailPath(question.photo.uid)}
      bbox={question.kind === 'face' ? question.bbox?.relative : undefined}
      alt={alt}
    />
  )
}

/**
 * What the screen is showing. The game is no longer "a question or an excuse":
 * a round ends in a card of its own, a session ends in another, and the odd card
 * inside a round asks nothing at all — each with its own keyboard rules, which is
 * exactly why the page names the state instead of inferring it three times.
 */
type Stage =
  | 'question'
  | 'breather'
  | 'round-summary'
  | 'session-summary'
  | 'loading'
  | 'error'
  | 'empty'

/**
 * The keys a non-question card leaves alone. Everything else moves the game on
 * ("any key"), but these four are about something other than the card in front
 * of the player: leaving, undoing the last answer, opening the photo and the
 * shortcut help.
 */
const RESERVED_KEYS = new Set(['Escape', 'z', 'o', '?'])

/** The keys that press the primary button of a summary card. */
const CONFIRM_KEYS = new Set(['Enter', ' ', 'ArrowRight'])

/**
 * The review game (`/review`, editors only): a fullscreen one-card-at-a-time
 * flow played in **rounds**. A round is ~10 questions with a visible position
 * (4/10) and a combo of consecutive answers; roughly once a round a card that
 * asks nothing arrives — a photo "jen pro radost", or the payoff of a face just
 * confirmed — and between rounds a small card says what the last ten came to and
 * offers another. Leaving mid-round costs nothing: every answer was persisted on
 * its own the moment it was given.
 *
 * A question is one plain-language sentence, the photo under it as large as the
 * room left over allows, and Ano / Ne / Nevím. The question and the buttons
 * always fit; the photo is what shrinks. The keyboard is the primary interface
 * (← no, → yes, Space/↓ skip, y/n, z undo, o open the photo in a new tab, Esc
 * leave) and on touch the same three answers are a swipe — right yes, left no,
 * down skip — with the card following the finger and naming the verdict before
 * it fires. The way out to the photo is a real anchor on the stage as well,
 * because the point of it is being able to copy the URL. Answers are optimistic
 * and the next card is always already in memory, so the rhythm is never broken
 * by a spinner (see {@link useReviewGame}). Rendered outside the layout shell so
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
  const streak = useReviewStreak()
  const { prime } = useImagePreloader()
  /** True once the player has asked to leave and the closing card is up. */
  const [leaving, setLeaving] = useState(false)

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
  // must paint instantly, and a round boundary must be invisible.
  useEffect(() => {
    prime(
      game.pending
        .slice(0, PRELOAD_AHEAD)
        .map((card) => cardPhoto(card))
        .filter((photo) => photo !== undefined)
        .map((photo) => thumbUrl(photo.uid, REVIEW_PREVIEW_SIZE)),
    )
  }, [prime, game.pending])

  const leave = useCallback(() => {
    if (window.history.length > 1) {
      void navigate(-1)
      return
    }
    void navigate('/')
  }, [navigate])

  const played = game.session.confirmed + game.session.rejected + game.session.skipped

  /**
   * Leaving is two steps once there is something to show for the session: the
   * closing card first, the actual exit second. A player who sorted for twenty
   * minutes and pressed Esc deserves to be told what they did before the game
   * disappears — and pressing Esc again is still one keystroke away.
   */
  const exit = useCallback(() => {
    if (!leaving && played > 0) {
      setLeaving(true)
      return
    }
    leave()
  }, [leave, leaving, played])

  const card: ReviewCard | undefined = game.current
  const question = card?.type === 'question' ? card.question : undefined

  let stage: Stage
  if (leaving) {
    stage = 'session-summary'
  } else if (game.summary !== null) {
    stage = 'round-summary'
  } else if (card !== undefined) {
    stage = card.type === 'question' ? 'question' : 'breather'
  } else if (game.fetching || (!game.exhausted && !game.loadError)) {
    stage = 'loading'
  } else if (game.loadError) {
    stage = 'error'
  } else {
    stage = 'empty'
  }

  const { answer, advance, nextRound } = game

  /**
   * The primary action of the between-rounds card: another round, or — when the
   * backend has nothing left — the closing card. One handler, because the button
   * and the Enter key must never disagree about what they do.
   */
  const continueRound = useCallback(() => {
    if (game.exhausted) {
      setLeaving(true)
      return
    }
    nextRound()
  }, [game.exhausted, nextRound])

  const swipe = useReviewSwipe({
    onVerdict: useCallback(
      (verdict: SwipeVerdict) => {
        answer(verdict)
      },
      [answer],
    ),
    enabled: stage === 'question',
  })

  /**
   * The keyboard twin of the anchor in {@link ReviewPhoto}: opens the photo on
   * screen in a new tab, same path and same `noopener`. It answers nothing and
   * touches the queue not at all — the card the player is looking at is still
   * there when they come back. A new tab, not this one, because the queue lives
   * in memory and leaving would drop the whole run.
   */
  const openPhoto = useCallback(() => {
    const photo = card === undefined ? undefined : cardPhoto(card)
    if (photo === undefined) {
      return
    }
    window.open(photoDetailPath(photo.uid), '_blank', 'noopener,noreferrer')
  }, [card])

  useKeyboardShortcuts({
    ArrowLeft: () => {
      answer('no')
    },
    n: () => {
      answer('no')
    },
    ArrowRight: () => {
      answer('yes')
    },
    y: () => {
      answer('yes')
    },
    ' ': () => {
      answer('skip')
    },
    ArrowDown: () => {
      answer('skip')
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

  // The keyboard on the cards that ask nothing. A breather moves on with any
  // key, because there is no answer to get wrong; a summary card has one primary
  // action, so only the keys that mean "yes, go on" press it. The answer keys
  // reach `answer()` first through the shortcut map above and no-op there — a
  // breather is not a question — which is what lets → both dismiss a breather and
  // continue into the next round without either meaning being ambiguous.
  useEffect(() => {
    if (stage !== 'breather' && stage !== 'round-summary' && stage !== 'session-summary') {
      return undefined
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.ctrlKey || event.metaKey || event.altKey || isTypingElement(event.target)) {
        return
      }
      if (RESERVED_KEYS.has(event.key) || document.querySelector('.modal.show') !== null) {
        return
      }
      if (stage === 'breather') {
        event.preventDefault()
        advance()
        return
      }
      if (!CONFIRM_KEYS.has(event.key)) {
        return
      }
      event.preventDefault()
      if (stage === 'round-summary') {
        continueRound()
        return
      }
      leave()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [advance, continueRound, leave, stage])

  // The celebration is a moment, not a state: it clears itself so nothing has to
  // be dismissed mid-rhythm.
  const { milestone, clearMilestone } = game
  useEffect(() => {
    if (milestone === null) {
      return undefined
    }
    const timer = window.setTimeout(clearMilestone, MILESTONE_MS)
    return () => {
      window.clearTimeout(timer)
    }
  }, [milestone, clearMilestone])

  // The hairline bar tracks the round, not the session: a session has no end to
  // measure against, and "how far into these ten am I" is the question a player
  // actually asks.
  const roundPct =
    game.round.size > 0 ? Math.min(100, Math.round((game.round.played / game.round.size) * 100)) : 0
  const position = game.round.size > 0 ? Math.min(game.round.played + 1, game.round.size) : 0

  const dragStyle: CSSProperties = swipe.dragging
    ? {
        transform: `translate(${String(swipe.offset.x)}px, ${String(swipe.offset.y)}px) rotate(${String(swipe.offset.x * DRAG_TILT)}deg)`,
      }
    : {}

  let body: ReactNode
  if (stage === 'question' && question !== undefined) {
    body = (
      <>
        <section className="review-game__prompt">
          <QuestionText question={question} />
          <QuestionHint question={question} />
          {/* The place check has no confidence to report: the estimator either
              found neighbours that cluster tightly enough or refused, so there is
              no percentage behind the guess and inventing one would be a lie. */}
          {question.kind !== 'place' && <ConfidenceHint confidence={question.confidence} />}
        </section>
        <main className="review-game__stage" data-testid="review-stage" {...swipe.handlers}>
          <div
            className={`review-game__card${swipe.dragging ? ' review-game__card--dragging' : ''}`}
            style={dragStyle}
          >
            <QuestionStage question={question} alt={t('review.photoAlt')} />
          </div>
          {swipe.hint !== null && (
            <span
              className={`review-game__verdict review-game__verdict--${swipe.hint}`}
              data-testid="review-swipe-hint"
              aria-hidden="true"
            >
              {t(VERDICT_LABEL_KEYS[swipe.hint])}
            </span>
          )}
        </main>
        <footer className="review-game__actions">
          <Button
            variant="outline-danger"
            size="lg"
            onClick={() => {
              answer('no')
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
              answer('skip')
            }}
          >
            {t('review.actions.skip')}
            <kbd className="review-game__kbd">{t('review.keys.space')}</kbd>
          </Button>
          <Button
            variant="success"
            size="lg"
            onClick={() => {
              answer('yes')
            }}
          >
            <Icon name="check-lg" className="me-2" />
            {t('review.actions.yes')}
            <kbd className="review-game__kbd">→</kbd>
          </Button>
        </footer>
      </>
    )
  } else if (stage === 'breather' && card?.type === 'breather') {
    body = (
      <BreatherCard
        breather={card.breather}
        href={photoDetailPath(card.breather.photo.uid)}
        onDismiss={advance}
      />
    )
  } else if (stage === 'breather' && card?.type === 'reveal') {
    body = <RevealCard reveal={card.reveal} onDismiss={advance} />
  } else if (stage === 'round-summary' && game.summary !== null) {
    body = (
      <RoundSummaryCard
        summary={game.summary}
        exhausted={game.exhausted}
        fetching={game.fetching}
        onContinue={continueRound}
      />
    )
  } else if (stage === 'session-summary') {
    body = <SessionSummaryCard tally={game.session} touched={game.touched} onClose={leave} />
  } else if (stage === 'loading') {
    // The only unavoidable wait: the first round (or one the player outran).
    // Everything else is prefetched behind the between-rounds card.
    body = (
      <div className="review-game__center">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('review.loading')}</span>
        </Spinner>
      </div>
    )
  } else if (stage === 'error') {
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
          {position > 0 && (
            <span className="review-game__round" data-testid="review-round-progress">
              {game.round.daily && (
                <span className="review-game__round-name">{t('review.round.daily')}</span>
              )}
              {t('review.round.position', { position, size: game.round.size })}
            </span>
          )}
          {game.combo >= COMBO_FLOOR && (
            <span
              className="review-game__combo"
              data-testid="review-combo"
              title={t('review.comboTitle')}
            >
              <Icon name="lightning-charge-fill" />
              {game.combo}
            </span>
          )}
          {streak > 0 && (
            <span
              className="review-game__streak"
              data-testid="review-streak"
              title={t('review.streakTitle', { count: streak })}
            >
              <Icon name="fire" />
              {streak}
            </span>
          )}
          <span className="d-none d-sm-inline">
            {t('review.progress.answered', { count: game.answered })}
            {game.remaining > 0 && (
              <span className="text-secondary">
                {' · '}
                {t('review.progress.remaining', { count: game.remaining })}
              </span>
            )}
          </span>
        </div>
        <KeyboardShortcutsHelp />
      </header>
      <div className="review-game__progressbar" aria-hidden="true">
        <div style={{ width: `${String(roundPct)}%` }} />
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
      {game.milestone !== null && <MilestoneBurst count={game.milestone} />}
      {body}
    </div>
  )
}
