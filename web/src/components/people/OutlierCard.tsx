import { type CSSProperties } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { boxWithinCrop, cropImageStyle, padBbox } from '../../lib/faceGeometry'
import { canUnassign, distancePercent, type OutlierItem, outlierKey } from '../../lib/outlierReview'
import { type OutlierFace } from '../../services/people'
import { thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

/** How much of the surrounding photo is kept around the face box, per side. */
const CONTEXT_PADDING = 0.3

/** The preview cropped from: the whole frame, since the bbox is frame-relative. */
const OUTLIER_PREVIEW_SIZE = 'fit_720'

/** Props for {@link OutlierCard}. */
export interface OutlierCardProps {
  /** The suspicious face and its live review status. */
  item: OutlierItem
  /** The person the face is assigned to, named in the verdict buttons. */
  subjectName: string
  /** True when this card holds the keyboard focus (draws a ring). */
  focused: boolean
  /** True while the page is in selection mode (the checkbox is shown). */
  selectable: boolean
  /** True when this card is part of the current selection. */
  selected: boolean
  /** Toggles the selection; `shiftKey` requests a range from the anchor. */
  onSelect: (shiftKey: boolean) => void
  /** ✓ "yes, this is wrong" — unassigns the person from the face. */
  onUnassign: () => void
  /** ✗ "no, this really is them" — records the confirmation. */
  onConfirm: () => void
}

/**
 * displayDims returns the photo's dimensions in display (EXIF-oriented) space.
 * Orientations 5–8 swap the stored raw width and height, matching how the
 * normalised face box was derived at detection time.
 */
function displayDims(face: OutlierFace): [number, number] {
  const rotated = face.orientation >= 5 && face.orientation <= 8
  return rotated ? [face.height, face.width] : [face.width, face.height]
}

/**
 * OutlierCard is one suspicious face in the /outliers grid.
 *
 * The picture is the point: it shows a **context crop** — the face box grown by
 * 30 % of its own size on every side — with the face itself outlined inside it.
 * That padding is not cosmetic. A tight crop of a face you are asked to judge is
 * unjudgeable; you need the hair, the shoulders and the room to recognise
 * someone. The crop is built from the full frame with `cropImageStyle` and the
 * rectangle placed with `boxWithinCrop` (both from `lib/faceGeometry`), so the
 * geometry needs no pixel measurement — the container's `aspect-ratio` carries it.
 *
 * The card asks a question ("is this a mistake?") so its ✓ and ✗ read as the
 * answer to it: ✓ agrees and unassigns the person, ✗ disagrees and vouches for
 * the face. Both verdicts flip the card **in place** rather than removing it, so
 * the grid never reflows mid-review.
 */
export function OutlierCard({
  item,
  subjectName,
  focused,
  selectable,
  selected,
  onSelect,
  onUnassign,
  onConfirm,
}: OutlierCardProps) {
  const { t } = useTranslation()
  const { face, status } = item
  const percent = distancePercent(face.distance)
  const crop = padBbox(face.bbox, CONTEXT_PADDING)
  const [displayWidth, displayHeight] = displayDims(face)

  const frameStyle: CSSProperties = {
    // The crop's own proportions: its share of the frame's width over its share
    // of the frame's height. Without this the context crop would stretch.
    aspectRatio: `${String(crop[2] * displayWidth)} / ${String(crop[3] * displayHeight)}`,
    background: 'var(--bs-dark)',
  }
  const boxStyle: CSSProperties = {
    ...boxWithinCrop(face.bbox, crop),
    borderStyle: 'solid',
    borderWidth: 3,
    borderColor: 'var(--bs-warning)',
    // A faint dark halo keeps the box visible over a light patch of photo.
    boxShadow: '0 0 0 1px rgba(0, 0, 0, 0.55)',
  }

  const decided = status === 'removed' || status === 'confirmed'
  const unassignable = canUnassign(face)

  return (
    <Card
      className="h-100"
      data-testid="outlier-card"
      data-outlier-key={outlierKey(face)}
      data-status={status}
      data-focused={focused}
      style={{
        outline: focused ? '3px solid var(--bs-primary)' : undefined,
        outlineOffset: '2px',
      }}
    >
      <div className="position-relative overflow-hidden rounded-top" style={frameStyle}>
        <img
          src={thumbUrl(face.photo_uid, OUTLIER_PREVIEW_SIZE)}
          alt={t('outliersPage.card.photoAlt')}
          loading="lazy"
          decoding="async"
          style={{ ...cropImageStyle(crop), objectFit: 'cover', opacity: decided ? 0.5 : 1 }}
        />
        <span className="position-absolute" style={boxStyle} data-testid="outlier-bbox" />
        <Badge
          bg="warning"
          text="dark"
          className="position-absolute top-0 start-0 m-2"
          title={t('outliersPage.card.distanceTitle')}
        >
          {t('outliersPage.card.distance', { percent })}
        </Badge>
        {selectable && (
          <Form.Check
            className="position-absolute top-0 end-0 m-2"
            checked={selected}
            onChange={() => {
              onSelect(false)
            }}
            onClick={(event) => {
              if (event.shiftKey) {
                event.preventDefault()
                onSelect(true)
              }
            }}
            aria-label={t('outliersPage.card.select')}
          />
        )}
      </div>

      <Card.Body className="d-flex flex-column gap-2 p-2">
        <div className="d-flex align-items-center gap-2">
          <Link
            to={`/photos/${face.photo_uid}`}
            className="small text-decoration-none"
            title={t('outliersPage.card.openPhoto')}
          >
            <Icon name="eye" className="me-1" />
            {t('outliersPage.card.openPhoto')}
          </Link>
        </div>

        {status === 'removed' && (
          <span className="text-success d-flex align-items-center gap-1 small">
            <Icon name="check-lg" />
            {t('outliersPage.card.removed')}
          </span>
        )}
        {status === 'confirmed' && (
          <span className="text-secondary d-flex align-items-center gap-1 small">
            <Icon name="person-check" />
            {t('outliersPage.card.confirmed')}
          </span>
        )}
        {!decided && (
          <>
            <span className="small text-secondary">{t('outliersPage.card.question')}</span>
            {status === 'error' && (
              <span className="text-danger small">{t('outliersPage.card.failed')}</span>
            )}
            <div className="d-flex gap-2">
              <Button
                variant="outline-danger"
                size="sm"
                className="flex-fill d-flex align-items-center justify-content-center gap-1"
                disabled={!unassignable}
                title={unassignable ? undefined : t('outliersPage.card.noMarker')}
                onClick={onUnassign}
              >
                <Icon name="check-lg" />
                {t('outliersPage.card.unassign')}
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                className="flex-fill d-flex align-items-center justify-content-center gap-1"
                onClick={onConfirm}
              >
                <Icon name="x-lg" />
                {t('outliersPage.card.confirm', { name: subjectName })}
              </Button>
            </div>
          </>
        )}
      </Card.Body>
    </Card>
  )
}
