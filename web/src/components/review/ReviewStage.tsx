import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useImageFrame } from '../../hooks/useImageFrame'
import { faceBoxStyle, padBbox } from '../../lib/faceGeometry'
import { type Bbox } from '../../services/people'
import { thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

// The stage's own classes live here, so they ship with the component rather
// than only with the review game's page — every tool that opens the lightbox
// needs them, and none of them should have to remember to import a stylesheet.
import './review.css'

/**
 * 3:2 is the stage's shape while nothing better is known — a landscape frame is
 * the commonest shot, and the point of a fallback here is only that the stage
 * never collapses to zero height.
 */
const FALLBACK_RATIO = 1.5

/** Props for {@link ReviewStage}. */
export interface ReviewStageProps {
  /** The photo to show. */
  photoUid: string
  /** The catalogue row's width in pixels, **before** EXIF orientation. */
  fileWidth: number
  /** The catalogue row's height in pixels, **before** EXIF orientation. */
  fileHeight: number
  /** The raw EXIF orientation tag (1–8, or 0 when absent). */
  orientation: number
  /**
   * The preview size to request. Always a `fit_*` (whole frame), never a square
   * `tile_*`: the box coordinates are relative to the full photo, so a
   * centre-cropped tile would put the rectangle somewhere else entirely.
   */
  size: string
  /**
   * The tight face box, normalised `[x, y, w, h]` in display space. The drawn
   * rectangle is padded ~30 % around it — a person is unrecognisable from a tight
   * face crop, and the surrounding frame is most of what you judge from.
   */
  bbox?: Bbox
  /**
   * The photo's own page. Renders the corner anchor when given; without it the
   * stage offers no way out.
   */
  href?: string
  /** Accessible description of the photo. */
  alt: string
}

/**
 * The photo stage shared by the review game and the review tools' lightbox: the
 * full frame as large as the space allows, the face under question marked by a
 * generously padded rectangle, and a gentle dim over everything outside it.
 *
 * The frame is width-driven with `aspect-ratio` and capped against the stage's
 * own height in container-query units, so the normalised box lines up with no
 * pixel measurement. **The parent must therefore be a size container**
 * (`container-type: size`) — `.review-game__stage` and `.review-lightbox__stage`
 * both are.
 *
 * The shape comes from {@link useImageFrame}: the loaded preview's own natural
 * size, with the catalogue row only as the estimate that keeps the stage from
 * resizing under the question. The rectangle waits for the measurement — against
 * a row with a transposed dimension pair it would mark the wrong part of the
 * photo, and being asked „is this Alice?" about the wrong face is worse than
 * being asked a moment later.
 *
 * **The photo is never a link.** Leaving for the photo's own page is the corner
 * anchor's job alone, so that a click into the preview — which carries the face
 * rectangle, and which in a tool's lightbox is the thing you came to look at —
 * is never ambiguous.
 */
export function ReviewStage({
  photoUid,
  fileWidth,
  fileHeight,
  orientation,
  size,
  bbox,
  href,
  alt,
}: ReviewStageProps) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  const stage = useImageFrame({
    source: photoUid,
    width: fileWidth,
    height: fileHeight,
    orientation,
  })

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photoUid])

  const ratio = stage.ratio ?? FALLBACK_RATIO
  // `100cqh` is the stage's real height, so the frame caps itself against the
  // room that is actually left after the chrome around it — never against an
  // estimate that drifts when that chrome grows.
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
          {...stage.imgProps}
          src={thumbUrl(photoUid, size)}
          alt={alt}
          decoding="async"
          onError={() => {
            setFailed(true)
          }}
          className="review-photo__img"
        />
      )}
      {bbox !== undefined && stage.measured && (
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
          opens in a new tab because what is around it lives in memory (the
          game's queue, a tool's search results): navigating away would throw the
          whole run away, and the user wants to set a photo aside, not leave. It
          sits in the frame's corner rather than being the whole preview, so a
          click into the photo stays unambiguous. */}
      {href !== undefined && (
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
      )}
    </div>
  )
}
