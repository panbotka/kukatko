import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useImageFrame } from '../../hooks/useImageFrame'
import { useLightbox } from '../../hooks/useLightbox'
import { groupKey } from '../../lib/duplicateMarkers'
import { faceBoxStyle } from '../../lib/faceGeometry'
import { formatDate } from '../../lib/format'
import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../../lib/gridDensity'
import { type DuplicateMarkerGroup } from '../../services/dupmarkers'
import { thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'
import { EnlargeButton } from '../review/EnlargeButton'
import { ReviewLightbox } from '../review/ReviewLightbox'

import { DuplicateMarkerCrop } from './DuplicateMarkerCrop'

import '../review/review.css'

/**
 * The thumbnail the whole-photo preview is cut from. It is a `fit_*` size, so it
 * carries the entire frame the normalised boxes are measured against — a `tile_*`
 * is a centre-cropped square and every box would land beside its face. 1280 is
 * enough for a preview a few hundred pixels wide on a 2× display without making
 * a page of twenty cards pull megabytes.
 */
const PREVIEW_SIZE = 'fit_1280'

/** Props for {@link DuplicateMarkerGroupCard}. */
export interface DuplicateMarkerGroupCardProps {
  /** The finding: one person, one photo, and every marker of theirs on it. */
  group: DuplicateMarkerGroup
  /** True while any decision on the page is in flight; disables the actions. */
  busy: boolean
  /** Keeps `markerUid` and detaches the person from every other marker here. */
  onKeep: (group: DuplicateMarkerGroup, markerUid: string) => void
  /** Flags `markerUid` as holding no face at all. */
  onInvalid: (group: DuplicateMarkerGroup, markerUid: string) => void
  /** Records "this really is them twice" and hides the finding for good. */
  onDismiss: (group: DuplicateMarkerGroup) => void
  /** How many numbered crops sit side by side — the review tools' shared count. */
  density: number
}

/**
 * One finding in the repeated-marker review: "Marie is tagged three times on this
 * photo".
 *
 * The card is built around the picture, because the decision cannot be made
 * without one. The whole photo is shown with every one of that person's boxes
 * outlined and numbered — that is what reveals the usual cause, three names
 * marched across a row of a group shot — and each numbered box is repeated below
 * as a close-up, because at preview size the faces are too small to tell apart.
 * The numbers are the join between the two: box 2 in the preview is crop 2 below.
 *
 * Every action is explicit and every one is reversible in principle: keeping one
 * marker **detaches** the others rather than deleting them (on a group shot the
 * box almost always belongs to the person standing next to her, and it is worth
 * re-tagging), flagging a box invalid leaves the row in place, and "leave it be"
 * changes no data at all. Nothing is merged, and nothing happens without a click.
 */
export function DuplicateMarkerGroupCard({
  group,
  busy,
  onKeep,
  onInvalid,
  onDismiss,
  density,
}: DuplicateMarkerGroupCardProps) {
  const { t, i18n } = useTranslation()
  // The overlay walks this photo's boxes: one frame, one box at a time, big
  // enough to tell which of three faces is actually her. Same list, same order
  // and same numbers as the crops below the preview.
  const lightbox = useLightbox(group.markers)
  const enlarged = lightbox.item
  // The preview's frame *is* the coordinate system of the boxes over it, so it
  // comes from the loaded preview rather than from the catalogue row (which, on a
  // photo imported with already-oriented dimensions, is transposed and would throw
  // every box off its face). The row remains the estimate that holds the card's
  // height still while the preview loads.
  const preview = useImageFrame({
    source: group.photo_uid,
    width: group.width,
    height: group.height,
    orientation: group.orientation,
  })
  const title = group.photo_title !== '' ? group.photo_title : t('duplicateMarkers.card.untitled')

  return (
    <Card className="mb-3" data-testid="dup-marker-group" data-group-key={groupKey(group)}>
      <Card.Header className="d-flex flex-wrap align-items-center gap-2">
        <Icon name="person-bounding-box" className="text-warning" />
        <Link to={`/people/${group.subject_uid}`} className="fw-semibold text-decoration-none">
          {group.subject_name}
        </Link>
        <Badge bg="warning" text="dark">
          {t('duplicateMarkers.card.count', { count: group.markers.length })}
        </Badge>
        <span className="text-secondary small ms-auto text-truncate">
          {title}
          {group.taken_at !== undefined && ` · ${formatDate(group.taken_at, i18n.language)}`}
        </span>
        <Link
          to={`/photos/${group.photo_uid}`}
          className="small text-decoration-none"
          title={t('duplicateMarkers.card.openPhoto')}
        >
          <Icon name="eye" className="me-1" />
          {t('duplicateMarkers.card.openPhoto')}
        </Link>
      </Card.Header>

      <Card.Body className="d-flex flex-column gap-3">
        <div
          className="position-relative mx-auto w-100 overflow-hidden rounded"
          data-testid="dup-marker-preview"
          style={{
            maxWidth: '32rem',
            // 3:2 while nothing is known yet: the card needs a height, and with no
            // frame there is no box to misplace either.
            aspectRatio: preview.aspectRatio ?? '3 / 2',
            background: 'var(--bs-dark)',
          }}
        >
          <img
            {...preview.imgProps}
            src={thumbUrl(group.photo_uid, PREVIEW_SIZE)}
            alt={t('duplicateMarkers.card.previewAlt', { name: group.subject_name })}
            loading="lazy"
            decoding="async"
            className="w-100 h-100"
            style={{ objectFit: 'contain' }}
          />
          {/* No box until the frame is the measured preview: drawn against the
              estimate a box can sit off its face and then jump. The numbered crops
              below carry the same numbers, so nothing is lost in the meantime. */}
          {preview.measured &&
            group.markers.map((marker, index) => (
              <span
                key={marker.uid}
                data-testid="dup-marker-box"
                className="position-absolute border border-2 border-warning rounded-1"
                style={{ ...faceBoxStyle(marker.bbox), pointerEvents: 'none' }}
              >
                <span
                  className="position-absolute top-0 start-0 badge text-bg-warning"
                  style={{ transform: 'translate(-2px, -100%)' }}
                >
                  {index + 1}
                </span>
              </span>
            ))}
        </div>

        <div
          className="d-grid kk-review-grid"
          data-density={density}
          /* The row of crops is what is worth making bigger on this page: the
             page is a list of findings and the judging happens inside one. The
             responsive 2/3/4 columns are now the user's own count, and
             `kk-review-grid` is what keeps that count honest: a crop sits above
             two decision buttons, so without it the `1fr` tracks would grow to
             their labels and run off the side of a phone. */
          style={{
            gridTemplateColumns: gridTemplateColumns(density),
            gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
          }}
        >
          {group.markers.map((marker, index) => (
            <div key={marker.uid}>
              <div className="d-flex flex-column gap-2 h-100">
                <div className="position-relative">
                  <EnlargeButton
                    onEnlarge={() => {
                      lightbox.open(index)
                    }}
                  >
                    <DuplicateMarkerCrop
                      photoUid={group.photo_uid}
                      marker={marker}
                      width={group.width}
                      height={group.height}
                      orientation={group.orientation}
                    />
                  </EnlargeButton>
                  <Badge
                    bg="warning"
                    text="dark"
                    className="position-absolute top-0 start-0 m-1"
                    aria-hidden
                  >
                    {index + 1}
                  </Badge>
                </div>
                <Button
                  variant="outline-success"
                  size="sm"
                  disabled={busy}
                  className="d-flex align-items-center justify-content-center gap-1"
                  onClick={() => {
                    onKeep(group, marker.uid)
                  }}
                >
                  <Icon name="check-lg" />
                  {t('duplicateMarkers.card.keep', { index: index + 1 })}
                </Button>
                <Button
                  variant="outline-secondary"
                  size="sm"
                  disabled={busy}
                  className="d-flex align-items-center justify-content-center gap-1"
                  title={t('duplicateMarkers.card.invalidTitle')}
                  onClick={() => {
                    onInvalid(group, marker.uid)
                  }}
                >
                  <Icon name="x-lg" />
                  {t('duplicateMarkers.card.invalid')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      </Card.Body>

      {enlarged !== null && (
        <ReviewLightbox
          stage={{
            photoUid: group.photo_uid,
            fileWidth: group.width,
            fileHeight: group.height,
            orientation: group.orientation,
            size: PREVIEW_SIZE,
            bbox: enlarged.bbox,
            href: `/photos/${group.photo_uid}`,
            alt: t('duplicateMarkers.card.cropAlt'),
          }}
          title={t('duplicateMarkers.card.keep', { index: lightbox.index + 1 })}
          onClose={lightbox.close}
          onPrev={lightbox.prev}
          onNext={lightbox.next}
          hasPrev={lightbox.hasPrev}
          hasNext={lightbox.hasNext}
        >
          <Button
            variant="outline-success"
            disabled={busy}
            className="d-flex align-items-center gap-1"
            onClick={() => {
              onKeep(group, enlarged.uid)
              lightbox.close()
            }}
          >
            <Icon name="check-lg" />
            {t('duplicateMarkers.card.keep', { index: lightbox.index + 1 })}
          </Button>
          <Button
            variant="outline-light"
            disabled={busy}
            className="d-flex align-items-center gap-1"
            title={t('duplicateMarkers.card.invalidTitle')}
            onClick={() => {
              onInvalid(group, enlarged.uid)
              lightbox.close()
            }}
          >
            <Icon name="x-lg" />
            {t('duplicateMarkers.card.invalid')}
          </Button>
        </ReviewLightbox>
      )}

      <Card.Footer className="d-flex flex-wrap align-items-center gap-2">
        <span className="small text-secondary">{t('duplicateMarkers.card.hint')}</span>
        <Button
          variant="link"
          size="sm"
          className="ms-auto text-decoration-none"
          disabled={busy}
          title={t('duplicateMarkers.card.dismissTitle')}
          onClick={() => {
            onDismiss(group)
          }}
        >
          <Icon name="hand-thumbs-up" className="me-1" />
          {t('duplicateMarkers.card.dismiss')}
        </Button>
      </Card.Footer>
    </Card>
  )
}
