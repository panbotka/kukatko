import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type ReviewItem } from '../../lib/candidateReview'
import { Icon } from '../Icon'

/** Props for {@link CandidateDecisions}. */
export interface CandidateDecisionsProps {
  /** The candidate on show, with its live review status. */
  item: ReviewItem
  /** Confirms it — the very handler the card's ✓ calls. */
  onConfirm: () => void
  /** Rejects it — the very handler the card's ✗ calls. */
  onReject: () => void
}

/**
 * The two verdicts on a face candidate, for the lightbox's footer: the same pair
 * the card carries, on the same handlers, so a decision costs no trip back to the
 * grid. A candidate already assigned to this subject shows what it shows on the
 * card — the done state — rather than an offer to assign it twice.
 *
 * Labelled with words here, unlike the card's icon-only pair: the overlay has the
 * room, and a full-screen photo gives no surrounding card to read the ✓ against.
 */
export function CandidateDecisions({ item, onConfirm, onReject }: CandidateDecisionsProps) {
  const { t } = useTranslation()

  if (item.status === 'done') {
    return (
      <span className="text-success d-flex align-items-center gap-1">
        <Icon name="check-lg" />
        {t('faceSearch.card.doneLabel')}
      </span>
    )
  }

  return (
    <>
      <Button
        variant="outline-danger"
        onClick={onReject}
        className="d-flex align-items-center gap-1"
      >
        <Icon name="x-lg" />
        {t('faceSearch.card.reject')}
      </Button>
      <Button variant="success" onClick={onConfirm} className="d-flex align-items-center gap-1">
        <Icon name="check-lg" />
        {t('faceSearch.card.confirm')}
      </Button>
    </>
  )
}
