import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type ReviewBreather } from '../../services/review'
import { Icon } from '../Icon'

import { ReviewPhoto } from './ReviewPhoto'

/**
 * The card a round carries that asks nothing.
 *
 * A game made only of questions is a belt, and a belt is what players quit. So
 * roughly once a round the queue hands over a card with no verdict on it: a photo
 * worth looking at. It counts toward neither the round's progress, the combo nor
 * the session totals: a pause that scored points would not be a pause.
 *
 * It renders into the game's own three-part body (prompt / stage / actions), so
 * the fullscreen layout rules hold unchanged: the text and the button keep their
 * room and the photo takes what is left.
 */

/** Props of the breather card. */
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
  linkState,
  onDismiss,
}: DismissProps & {
  breather: ReviewBreather
  /** The photo's own page, built by the page so every route agrees. */
  href: string
  /** The game's way back, handed to the photo's page; see `ReviewStage`. */
  linkState?: object
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
        <ReviewPhoto
          photo={breather.photo}
          href={href}
          linkState={linkState}
          alt={breather.title}
        />
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
