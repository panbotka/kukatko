import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'

import {
  bucketOf,
  BUCKET_LABEL_KEY,
  BUCKET_VARIANT,
  candidateKey,
  type ReviewItem,
} from '../../lib/candidateReview'
import { distanceToPercent } from '../../lib/faceThreshold'
import { Icon } from '../Icon'

import { CandidateFaceImage } from './CandidateFaceImage'

/** Props for {@link CandidateCard}. */
export interface CandidateCardProps {
  /** The candidate and its live review status. */
  item: ReviewItem
  /** True when this card holds the keyboard focus (draws a ring). */
  focused: boolean
  /** Confirms the candidate (assigns the person). */
  onConfirm: () => void
  /** Rejects the candidate (persists "not this person" and removes the card). */
  onReject: () => void
}

/**
 * CandidateCard is one result in the /faces grid: a full-frame photo with the
 * candidate face boxed in the bucket colour, a badge naming what confirming does,
 * the match percentage (and how many source photos voted for it), and the ✓ / ✗
 * controls. Confirming flips it to the done state in place; the ✗ removes it.
 *
 * The card carries `data-candidate-key` and `data-focused` so the page can scroll the
 * focused card into view and tests can address it, without threading a ref down.
 */
export function CandidateCard({ item, focused, onConfirm, onReject }: CandidateCardProps) {
  const { t } = useTranslation()
  const { candidate, status } = item
  const bucket = bucketOf(item)
  const variant = BUCKET_VARIANT[bucket]
  const matchPercent = distanceToPercent(candidate.distance)
  const done = status === 'done'
  const errored = status === 'error'

  return (
    <Card
      className="h-100"
      data-testid="candidate-card"
      data-candidate-key={candidateKey(candidate)}
      data-status={status}
      data-focused={focused}
      style={{
        outline: focused ? '3px solid var(--bs-primary)' : undefined,
        outlineOffset: '2px',
      }}
    >
      <div className="position-relative">
        <CandidateFaceImage
          photoUid={candidate.photo.uid}
          orientation={candidate.photo.file_orientation ?? 1}
          fileWidth={candidate.photo.file_width}
          fileHeight={candidate.photo.file_height}
          bbox={candidate.bbox.relative}
          variant={variant}
          done={done}
          alt={t('faceSearch.card.photoAlt')}
        />
        <Badge bg={variant} className="position-absolute top-0 start-0 m-2">
          {t(BUCKET_LABEL_KEY[bucket])}
        </Badge>
      </div>

      <Card.Body className="d-flex align-items-center gap-2 p-2">
        <span className="small text-secondary" title={t('faceSearch.card.matchTitle')}>
          {t('faceSearch.card.match', { percent: matchPercent })}
        </span>
        {candidate.match_count > 1 && (
          <Badge bg="secondary" pill title={t('faceSearch.card.votesTitle')}>
            {t('faceSearch.card.votes', { count: candidate.match_count })}
          </Badge>
        )}

        <span className="ms-auto d-flex align-items-center gap-2">
          {done ? (
            <span className="text-success d-flex align-items-center gap-1 small">
              <Icon name="check-lg" />
              {t('faceSearch.card.doneLabel')}
            </span>
          ) : (
            <>
              {errored && <span className="text-danger small">{t('faceSearch.card.failed')}</span>}
              <Button
                variant="outline-danger"
                size="sm"
                onClick={onReject}
                aria-label={t('faceSearch.card.reject')}
                title={t('faceSearch.card.reject')}
              >
                <Icon name="x-lg" />
              </Button>
              <Button
                variant="success"
                size="sm"
                onClick={onConfirm}
                aria-label={t('faceSearch.card.confirm')}
                title={t('faceSearch.card.confirm')}
              >
                <Icon name="check-lg" />
              </Button>
            </>
          )}
        </span>
      </Card.Body>
    </Card>
  )
}
