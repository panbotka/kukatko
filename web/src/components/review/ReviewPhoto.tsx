import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { faceBoxStyle, padBbox } from '../../lib/faceGeometry'
import { type Bbox } from '../../services/people'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

/**
 * The stage's preview size. `fit_*` (whole frame), never a square `tile_*`,
 * because the face box coordinates are relative to the full photo; 1280 because
 * the photo is the whole screen here.
 */
export const REVIEW_PREVIEW_SIZE = 'fit_1280'

/** Props for {@link ReviewPhoto}. */
export interface ReviewPhotoProps {
  /** The photo under question. */
  photo: Photo
  /**
   * Where the photo's own page is (`/photos/{uid}`). Handed in rather than built
   * here, because the page's `o` shortcut opens the same target — one string for
   * both means the link the player copies and the tab the key opens can never
   * disagree.
   */
  href: string
  /**
   * The tight face box, normalised `[x, y, w, h]` in display space (face
   * questions only). The drawn rectangle is padded ~30 % around it — a person
   * is unrecognisable from a tight face crop.
   */
  bbox?: Bbox
  /** Accessible description of the photo. */
  alt: string
}

/**
 * displayAspect returns the CSS `aspect-ratio` of a photo in display
 * (EXIF-oriented) space; orientations 5–8 swap width and height. Falls back to
 * 3:2 when dimensions are unknown so the stage never collapses to zero height.
 */
function displayAspect(orientation: number, fileWidth: number, fileHeight: number): number {
  const rotated = orientation >= 5 && orientation <= 8
  const width = rotated ? fileHeight : fileWidth
  const height = rotated ? fileWidth : fileHeight
  if (width <= 0 || height <= 0) {
    return 1.5
  }
  return width / height
}

/**
 * The review game's photo stage: the full frame as large as the space left
 * under the question allows, with the face under question marked by a
 * generously padded rectangle and a gentle dim over everything outside it. The
 * frame is width-driven with `aspect-ratio` and capped against the stage's own
 * height in container-query units, so the normalised box lines up with no pixel
 * measurement (the {@link CandidateFaceImage} approach, scaled to a full
 * screen). A quiet corner anchor leads out to the photo's own page, so anything
 * worth keeping or sharing can be taken along.
 */
export function ReviewPhoto({ photo, href, bbox, alt }: ReviewPhotoProps) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photo.uid])

  const ratio = displayAspect(photo.file_orientation ?? 0, photo.file_width, photo.file_height)
  // `100cqh` is the stage's real height (it is a size container), so the frame
  // caps itself against the room that is actually left after the question and
  // the buttons — never against an estimate that drifts when they grow.
  const frameStyle = {
    aspectRatio: String(ratio),
    maxWidth: `min(100%, calc(100cqh * ${String(ratio)}))`,
  }

  return (
    <div className="review-photo" style={frameStyle}>
      {failed ? (
        <div className="d-flex align-items-center justify-content-center w-100 h-100 text-secondary">
          <Icon name="images" />
        </div>
      ) : (
        <img
          src={thumbUrl(photo.uid, REVIEW_PREVIEW_SIZE)}
          alt={alt}
          decoding="async"
          onError={() => {
            setFailed(true)
          }}
          className="review-photo__img"
        />
      )}
      {bbox !== undefined && (
        <div
          className="position-absolute top-0 start-0 w-100 h-100"
          style={{ pointerEvents: 'none' }}
          aria-hidden="true"
        >
          <span
            className="review-photo__box"
            style={faceBoxStyle(padBbox(bbox))}
            data-testid="review-bbox"
          />
        </div>
      )}
      {/* The way out to the photo itself. A real anchor, not a click handler:
          the point is to *get the URL* — right-click → copy link address,
          Ctrl/Cmd+click, middle-click — and only an `href` can give that. It
          opens in a new tab because the queue lives in memory (`useReviewGame`):
          navigating away would throw the whole run away, and the player wants to
          set a photo aside, not leave the game. It sits in the frame's corner
          rather than being the whole preview: the preview carries the face
          rectangle, and a click into it must never be ambiguous. Answering stays
          the easiest thing on the screen — this is one small, quiet target. */}
      <Link
        to={href}
        target="_blank"
        rel="noopener noreferrer"
        className="review-photo__open"
        aria-label={t('review.openPhoto')}
        title={t('review.openPhoto')}
        data-testid="review-open-photo"
      >
        <Icon name="box-arrow-up-right" />
        <kbd className="review-game__kbd">o</kbd>
      </Link>
    </div>
  )
}
