import { type TFunction } from 'i18next'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type ReviewBreather, type ReviewReveal } from '../../services/review'
import { Icon } from '../Icon'

import { ReviewPhoto } from './ReviewPhoto'

/**
 * The two cards a round carries that ask nothing.
 *
 * A game made only of questions is a belt, and a belt is what players quit. So
 * roughly once a round the queue hands over a card with no verdict on it: a photo
 * worth looking at, or — better, when the round earned one — what a confirmed
 * face just added up to. Neither counts toward the round's progress, the combo or
 * the session totals: a pause that scored points would not be a pause.
 *
 * Both render into the game's own three-part body (prompt / stage / actions), so
 * the fullscreen layout rules hold unchanged: the text and the button keep their
 * room and the photo takes what is left.
 */

/** Props shared by both breather cards. */
interface DismissProps {
  /** Moves past the card; any key and any tap end up here. */
  onDismiss: () => void
}

/** The i18n key explaining why a photo was picked out for a pause. */
const REASON_KEYS = {
  favorite: 'review.breather.favorite',
  rated: 'review.breather.rated',
} as const

/** Why this photo, in a quiet line — or nothing for a reason we don't know. */
function reasonKey(reason: string): (typeof REASON_KEYS)[keyof typeof REASON_KEYS] | undefined {
  if (reason === 'favorite' || reason === 'rated') {
    return REASON_KEYS[reason]
  }
  return undefined
}

/**
 * The "jen pro radost" card: a photo the household favourited or rated highly,
 * with its title and year, and nothing to decide. The photo keeps its corner
 * anchor out to its own page — a breather is exactly when somebody wants to keep
 * one.
 */
export function BreatherCard({
  breather,
  href,
  onDismiss,
}: DismissProps & {
  breather: ReviewBreather
  /** The photo's own page, built by the page so every route agrees. */
  href: string
}) {
  const { t } = useTranslation()
  const reason = reasonKey(breather.reason)
  return (
    <>
      <section className="review-game__prompt" data-testid="review-breather">
        <p className="review-game__breather-tag">
          <Icon name="stars" className="me-2" />
          {t('review.breather.joy')}
        </p>
        <h1 className="review-game__question review-game__breather-title">
          {breather.title}
          {breather.year !== undefined && (
            <span className="review-game__breather-year"> · {breather.year}</span>
          )}
        </h1>
        {reason !== undefined && <p className="review-game__face-hint">{t(reason)}</p>}
      </section>
      <main className="review-game__stage">
        <ReviewPhoto photo={breather.photo} href={href} alt={breather.title} />
      </main>
      <footer className="review-game__actions">
        <Button
          variant="outline-light"
          size="lg"
          onClick={onDismiss}
          data-testid="review-breather-continue"
          className="kukatko-tap-target"
        >
          {t('review.breather.continue')}
          <kbd className="review-game__kbd">→</kbd>
        </Button>
      </footer>
    </>
  )
}

/**
 * How far a person's dated photos reach, as one line — or nothing at all when
 * none of their photos carries a date. A one-sided span (only the oldest known)
 * says just that rather than pretending the other end is today.
 */
function yearSpan(reveal: ReviewReveal, t: TFunction): string | null {
  if (reveal.oldest_year === undefined) {
    return null
  }
  if (reveal.newest_year === undefined || reveal.newest_year === reveal.oldest_year) {
    return t('review.reveal.oldest', { year: reveal.oldest_year })
  }
  return t('review.reveal.span', { oldest: reveal.oldest_year, newest: reveal.newest_year })
}

/**
 * The payoff card: what the face the player just confirmed added up to — how many
 * photos that person is on now and how far their collection reaches — with the
 * way into their gallery. It is the one moment the game shows the work adding up
 * rather than asking for more of it.
 */
export function RevealCard({ reveal, onDismiss }: DismissProps & { reveal: ReviewReveal }) {
  const { t } = useTranslation()
  const span = yearSpan(reveal, t)
  return (
    <>
      <section className="review-game__prompt" data-testid="review-reveal">
        <p className="review-game__breather-tag">
          <Icon name="person-check" className="me-2" />
          {t('review.reveal.tag')}
        </p>
        <h1 className="review-game__question">
          {t('review.reveal.title', { name: reveal.name, count: reveal.photo_count })}
        </h1>
        {span !== null && <p className="review-game__face-hint">{span}</p>}
      </section>
      <main className="review-game__stage review-game__stage--plain">
        <Link
          to={`/people/${encodeURIComponent(reveal.subject_uid)}`}
          target="_blank"
          rel="noopener noreferrer"
          className="review-game__reveal-link kukatko-tap-target"
          data-testid="review-reveal-link"
        >
          <Icon name="people" className="me-2" />
          {t('review.reveal.open', { name: reveal.name })}
        </Link>
      </main>
      <footer className="review-game__actions">
        <Button
          variant="outline-light"
          size="lg"
          onClick={onDismiss}
          data-testid="review-breather-continue"
          className="kukatko-tap-target"
        >
          {t('review.breather.continue')}
          <kbd className="review-game__kbd">→</kbd>
        </Button>
      </footer>
    </>
  )
}
