import { type CSSProperties, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { cropImageStyle, displayFrame, faceMarkerStyle, padBbox } from '../../lib/faceGeometry'
import {
  FACE_SOURCE_REVIEW_BUDGET_PX,
  FACE_SOURCE_REVIEW_MAX,
  faceSourceSize,
  OUTLIER_TARGET_PX,
  smallerFaceSource,
} from '../../lib/faceSource'
import { canUnassign, distancePercent, type OutlierItem, outlierKey } from '../../lib/outlierReview'
import { thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

import './outliers.css'

/** How much of the surrounding photo is kept around the face box, per side. */
const CONTEXT_PADDING = 0.3

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
 * OutlierCard is one suspicious face in the /outliers grid.
 *
 * The picture is the point: it shows a **context crop** — the face box grown by
 * 30 % of its own size on every side — with the face itself outlined inside it.
 * That padding is not cosmetic. A tight crop of a face you are asked to judge is
 * unjudgeable; you need the hair, the shoulders and the room to recognise
 * someone. The crop is built from the full frame with `cropImageStyle` and the
 * marker placed with `faceMarkerStyle` (both from `lib/faceGeometry`), so the
 * geometry needs no pixel measurement — the container's `aspect-ratio` carries it.
 *
 * **Which thumbnail the crop is cut from is chosen per face**, by
 * `lib/faceSource`. The card used to hard-code `fit_720`, which is why a small
 * face came out as mush: most faces span a few percent of the frame, so 720 px
 * left them 20–40 px wide before a ~7× upscale into the tile. Asking for the
 * smallest `fit_*` that still puts `OUTLIER_TARGET_PX` pixels across the crop
 * fixes that without making a grid of dozens of cards fetch `fit_3840` for faces
 * that were already fine. A size missing from the object store degrades down the
 * ladder on `onError` instead of showing a broken image.
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
  const frame = displayFrame(face.width, face.height, face.orientation)
  const known = frame.width > 0 && frame.height > 0

  const frameStyle: CSSProperties = {
    // The crop's own proportions: its share of the frame's width over its share
    // of the frame's height. Without this the context crop would stretch — and
    // without the guard, a photo whose dimensions were never recorded would ask
    // for the invalid `0 / 0` and collapse the tile.
    aspectRatio: known
      ? `${String(crop[2] * frame.width)} / ${String(crop[3] * frame.height)}`
      : '1 / 1',
    background: 'var(--bs-dark)',
  }

  // The preferred source is a pure function of the face, so a card that has
  // already fallen back keeps that fallback — and a card whose face changed
  // identity (never in the grid, which keys by face, but cheap to be right about)
  // starts again from the preferred size.
  const preferred = faceSourceSize(crop, frame, OUTLIER_TARGET_PX, {
    maxSize: FACE_SOURCE_REVIEW_MAX,
    budgetPx: FACE_SOURCE_REVIEW_BUDGET_PX,
  })
  const [degraded, setDegraded] = useState<{ from: string; size: string } | null>(null)
  const source = degraded !== null && degraded.from === preferred ? degraded.size : preferred

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
          src={thumbUrl(face.photo_uid, source)}
          data-testid="outlier-photo"
          data-thumb-size={source}
          alt={t('outliersPage.card.photoAlt')}
          loading="lazy"
          decoding="async"
          style={{ ...cropImageStyle(crop), objectFit: 'cover', opacity: decided ? 0.5 : 1 }}
          onError={() => {
            // The size is missing from the object store (a publishing backend
            // redirects rather than generating), so step down the ladder instead
            // of leaving a broken image where a face should be.
            const next = smallerFaceSource(source)
            if (next !== null) {
              setDegraded({ from: preferred, size: next })
            }
          }}
        />
        <span
          className="kk-face-marker"
          style={faceMarkerStyle(face.bbox, crop)}
          data-testid="outlier-bbox"
        />
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
