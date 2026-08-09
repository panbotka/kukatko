import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type RoundSummary, type SessionTally } from '../../hooks/useReviewGame'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon, type IconName } from '../Icon'

/**
 * The two cards that close something: a round, and the session.
 *
 * Both exist for the same reason — a game that only ever hands you the next
 * question never tells you that you did anything. The round card is the breath
 * between ten questions and the invitation into the next ten; the session card is
 * what the player leaves with.
 */

/** One tally line: a glyph, a count and what it counted. */
function Tally({
  icon,
  value,
  label,
  name,
}: {
  icon: IconName
  value: number
  label: string
  /** The verdict this line counts; also its test hook. */
  name: keyof SessionTally
}) {
  return (
    <li className="review-summary__tally" data-testid={`review-tally-${name}`}>
      <Icon name={icon} className="review-summary__tally-icon" />
      <strong className="review-summary__tally-value">{value}</strong>
      <span className="review-summary__tally-label">{label}</span>
    </li>
  )
}

/** The yes/no/skip split of a round or a session, as three tally lines. */
function Tallies({ tally }: { tally: SessionTally }) {
  const { t } = useTranslation()
  return (
    <ul className="review-summary__tallies">
      <Tally
        icon="check-lg"
        name="confirmed"
        value={tally.confirmed}
        label={t('review.summary.confirmed')}
      />
      <Tally
        icon="x-lg"
        name="rejected"
        value={tally.rejected}
        label={t('review.summary.rejected')}
      />
      <Tally
        icon="dash-lg"
        name="skipped"
        value={tally.skipped}
        label={t('review.summary.skipped')}
      />
    </ul>
  )
}

/**
 * The between-rounds card: what the round just played came to, and one tap back
 * into the game. It is deliberately a **card, not a modal** — nothing was
 * interrupted, the round simply ended — and its single primary action is what
 * Enter and → press, so a player on the keyboard never has to reach for a mouse
 * to keep going.
 *
 * Finishing the day's first round earns the „Pro dnešek splněno" badge, which
 * closes nothing: the button underneath still says "Ještě kolo?". The badge is a
 * fact about the day, not a locked door.
 */
export function RoundSummaryCard({
  summary,
  exhausted,
  fetching,
  onContinue,
}: {
  summary: RoundSummary
  /** True when the backend has nothing left; the button then ends the session. */
  exhausted: boolean
  /** True while the next round is still being fetched behind this card. */
  fetching: boolean
  onContinue: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="review-game__center">
      <div className="review-summary" data-testid="review-round-summary">
        <p className="review-summary__eyebrow">
          {summary.daily
            ? t('review.round.daily')
            : t('review.round.title', { index: summary.index })}
        </p>
        <h1 className="review-summary__title">{t('review.summary.title')}</h1>
        {summary.completedDaily && (
          <p className="review-summary__daily" data-testid="review-daily-done">
            <Icon name="check-lg" className="me-2" />
            {t('review.summary.dailyDone')}
          </p>
        )}
        <Tallies tally={summary} />
        <Button
          variant="success"
          size="lg"
          onClick={onContinue}
          data-testid="review-next-round"
          className="kukatko-tap-target"
        >
          {exhausted ? t('review.summary.finish') : t('review.summary.again')}
          <kbd className="review-game__kbd">↵</kbd>
        </Button>
        {fetching && !exhausted && (
          <p className="review-summary__hint">{t('review.summary.loadingNext')}</p>
        )}
      </div>
    </div>
  )
}

/** How many of the touched photos the closing mosaic shows. */
const MOSAIC_SIZE = 12

/** The thumbnail size the mosaic asks for: a small square, cropped by design. */
const MOSAIC_THUMB = 'tile_224'

/**
 * The end-of-session card: the totals, and a small mosaic of the photos the
 * player decided about, each one a way into the library. The mosaic is the point
 * — a column of numbers is a report, whereas twelve photographs somebody just
 * sorted are the evening they spent — and it is capped, because the souvenir
 * stops being one when it becomes the library again.
 */
export function SessionSummaryCard({
  tally,
  touched,
  onClose,
}: {
  tally: SessionTally
  touched: readonly Photo[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const mosaic = touched.slice(-MOSAIC_SIZE).reverse()
  return (
    <div className="review-game__center">
      <div className="review-summary" data-testid="review-session-summary">
        <p className="review-summary__eyebrow">{t('review.session.eyebrow')}</p>
        <h1 className="review-summary__title">{t('review.session.title')}</h1>
        <Tallies tally={tally} />
        {mosaic.length > 0 && (
          <>
            <p className="review-summary__hint">{t('review.session.mosaic')}</p>
            <ul className="review-summary__mosaic" data-testid="review-session-mosaic">
              {mosaic.map((photo) => (
                <li key={photo.uid}>
                  <Link to={`/photos/${encodeURIComponent(photo.uid)}`}>
                    <img
                      src={thumbUrl(photo.uid, MOSAIC_THUMB)}
                      alt={photo.title === '' ? photo.file_name : photo.title}
                      loading="lazy"
                      decoding="async"
                    />
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
        <Button
          variant="outline-light"
          size="lg"
          onClick={onClose}
          data-testid="review-session-close"
          className="kukatko-tap-target"
        >
          {t('review.session.close')}
        </Button>
      </div>
    </div>
  )
}

/**
 * The milestone celebration: a badge that appears over the game at 10, 25 and 50
 * answers and fades out on its own. Deliberately small and silent — no sound, no
 * confetti storm, nothing that has to be clicked away — because it interrupts a
 * rhythm the player is in the middle of. It is `aria-live="polite"` so it is
 * announced once and never stolen focus for.
 */
export function MilestoneBurst({ count }: { count: number }) {
  const { t } = useTranslation()
  return (
    <div className="review-milestone" data-testid="review-milestone" aria-live="polite">
      <span className="review-milestone__badge">
        <Icon name="stars" className="me-2" />
        {t('review.milestone', { count })}
      </span>
    </div>
  )
}
