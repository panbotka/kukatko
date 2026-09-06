import { type CSSProperties, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { boxWithinCrop, cropImageStyle, displayFrame, padBbox } from '../../lib/faceGeometry'
import {
  FACE_SOURCE_REVIEW_BUDGET_PX,
  FACE_SOURCE_REVIEW_MAX,
  faceSourceSize,
  OUTLIER_TARGET_PX,
  smallerFaceSource,
} from '../../lib/faceSource'
import { type Bbox, type Subject, subjectAvatarUrl } from '../../services/people'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'
import { FaceCrop } from '../people/FaceCrop'

import { EnlargeButton } from './EnlargeButton'

import './review.css'

/**
 * How much of the photo around the face the enlarged crop keeps, per side. The
 * same 40 % the outlier check uses, and for the same reason: asked alone, a face
 * is recognised by its hair, its shoulders and a hint of the scene around it.
 */
const ZOOM_CROP_PADDING = 0.4

/** Props for {@link ReviewFacePair}. */
export interface ReviewFacePairProps {
  /** The photo the judged face is on. */
  photo: Photo
  /** The tight face box, normalised `[x, y, w, h]` in the photo's display space. */
  bbox: Bbox
  /** The person the question asks about. */
  subject: Subject
  /** Opens the enlarged crop ({@link ReviewFaceZoom}); the page owns that state. */
  onZoom: () => void
}

/**
 * The face question's two squares: the face being judged, and the person being
 * asked about. It reads left to right as the question itself — *this face …
 * Tomáš Kozák?*
 *
 * It exists because the photograph alone cannot be answered. A group shot of
 * thirty people is 393 px wide on a phone, which leaves the highlighted face
 * about twenty of them: the rectangle says *which* face, and nothing about
 * whose. Desktop only widens the same problem. So the face is also shown cut
 * out, next to the picture the library already has of the person, and the full
 * frame stays below for the context that decides the hard cases.
 *
 * Both squares are **server-cut renditions** — {@link FaceCrop}'s
 * `GET /photos/{uid}/face?box=…` and `subjectAvatarUrl`'s
 * `GET /subjects/{uid}/avatar`, about 15 kB each — not crops of a whole-frame
 * preview: this is a 96–128 px square, and cropping one in the page means
 * fetching megapixels to paint it.
 *
 * It lives in the prompt, above the photo and out of `.review-game__stage`, so
 * the swipe gesture keeps the whole stage to itself — a face crop that ate
 * horizontal drags would take away the phone's primary way to answer.
 *
 * A person with no picture at all keeps a quiet placeholder rather than the
 * browser's broken-image glyph: the question is still answerable from the face
 * and the name, and inventing a face for somebody would be worse than admitting
 * there is none.
 */
export function ReviewFacePair({ photo, bbox, subject, onZoom }: ReviewFacePairProps) {
  const { t } = useTranslation()
  const [avatarFailed, setAvatarFailed] = useState(false)

  return (
    <div className="review-face-pair" data-testid="review-face-pair">
      <figure className="review-face-pair__item">
        <EnlargeButton
          onEnlarge={onZoom}
          label={t('review.facePair.zoom')}
          className="review-face-pair__enlarge"
        >
          <FaceCrop
            photoUid={photo.uid}
            bbox={bbox}
            // Decorative: the caption under it already says what it is, and the
            // question above says whose face it is being compared with.
            label=""
            className="review-face-pair__square"
          />
        </EnlargeButton>
        <figcaption className="review-face-pair__caption">{t('review.facePair.face')}</figcaption>
      </figure>
      <span className="review-face-pair__vs" aria-hidden="true">
        <Icon name="question-circle" />
      </span>
      <figure className="review-face-pair__item">
        <div className="review-face-pair__square d-flex align-items-center justify-content-center">
          {!avatarFailed && (
            <img
              src={subjectAvatarUrl(subject.uid)}
              data-testid="review-face-pair-avatar"
              alt=""
              aria-hidden="true"
              decoding="async"
              className="w-100 h-100"
              style={{ objectFit: 'cover' }}
              onError={() => {
                setAvatarFailed(true)
              }}
            />
          )}
          {avatarFailed && (
            <span className="review-face-pair__missing" data-testid="review-face-pair-no-avatar">
              <Icon name="person-circle" />
            </span>
          )}
        </div>
        <figcaption className="review-face-pair__caption text-truncate">{subject.name}</figcaption>
      </figure>
    </div>
  )
}

/** Props for {@link ReviewFaceZoom}. */
export interface ReviewFaceZoomProps {
  /** The photo the judged face is on. */
  photo: Photo
  /** The tight face box, normalised against the photo's display frame. */
  bbox: Bbox
  /** The person the question asks about; names the overlay. */
  subject: Subject
  /** Closes the overlay. */
  onClose: () => void
}

/**
 * „Let me look properly": the judged face filling an overlay, with the detector's
 * box outlined inside it exactly as it is on the stage below.
 *
 * It is a crop of a `fit_*` preview rather than the 320 px square the small
 * chip loads — blown up to half a screen that square is a smear, and a smear is
 * not an answer. Which rung it is cut from depends on how small the face is
 * (`lib/faceSource` at the review budget: one crop on screen, opened
 * deliberately, is worth its bytes), degrading down the ladder if a rung is
 * missing from the object store. **Never a `tile_*`** — a tile is a
 * centre-cropped square, i.e. a different frame from the one the box was
 * normalised against, so cropping one lands beside the face.
 *
 * It is a layer of the game's own, not a react-bootstrap `Modal`: the game is a
 * fixed `z-index: 1080` sheet (the immersive viewer's layer, since it owns the
 * whole screen too) and Bootstrap's dialog band tops out at 1055, so a modal
 * opened from here renders *underneath* the photograph. Drawn inside the game,
 * the veil is above the answer buttons rather than beneath them, which is also
 * what keeps a tap meant to dismiss from answering the question by accident.
 *
 * The overlay owns no game state and answers nothing: it is a look, and the card
 * underneath is untouched when it closes. The page silences its own keyboard
 * while it is open, so `←`/`→` cannot answer a question the player is still
 * examining, and `Esc` — bound here — closes the overlay instead of leaving the
 * game.
 */
export function ReviewFaceZoom({ photo, bbox, subject, onClose }: ReviewFaceZoomProps) {
  const { t } = useTranslation()
  const crop = padBbox(bbox, ZOOM_CROP_PADDING)
  const frame = displayFrame(photo.file_width, photo.file_height, photo.file_orientation ?? 0)
  const known = frame.width > 0 && frame.height > 0

  // The crop's own proportions, so nothing is stretched — and a photo whose
  // dimensions were never recorded falls back to a square rather than dividing
  // by zero.
  const ratio = known ? (crop[2] * frame.width) / (crop[3] * frame.height) : 1
  const frameStyle: CSSProperties = {
    aspectRatio: String(ratio),
    // The stage's own idiom (see `ReviewStage`): the frame is width-driven, and
    // caps itself against the overlay's real height in container units, so a
    // portrait crop on a landscape phone shrinks instead of being clipped. The
    // subtracted rem is the caption line under it.
    maxWidth: `min(100%, calc((100cqh - 3rem) * ${String(ratio)}))`,
  }

  const preferred = faceSourceSize(crop, frame, OUTLIER_TARGET_PX, {
    maxSize: FACE_SOURCE_REVIEW_MAX,
    budgetPx: FACE_SOURCE_REVIEW_BUDGET_PX,
  })
  const [degraded, setDegraded] = useState<{ from: string; size: string } | null>(null)
  const source = degraded !== null && degraded.from === preferred ? degraded.size : preferred

  // The page's own shortcuts are off while this is up (`enabled: !zoomedFace`),
  // so Escape has to be answered here — otherwise the only key on the screen
  // would leave the game rather than close the look.
  useKeyboardShortcuts({ Escape: onClose })

  return (
    <div
      className="review-face-zoom"
      role="dialog"
      aria-modal="true"
      aria-label={t('review.facePair.zoomTitle')}
      data-testid="review-face-zoom"
    >
      {/* The dimmed surround is a real button, not a click handler on a div: a
          tap anywhere outside the face closes the look, and it is announced as
          what it does rather than being an invisible trap. */}
      <button
        type="button"
        className="review-face-zoom__veil"
        aria-label={t('review.facePair.zoomClose')}
        onClick={onClose}
      />
      <figure className="review-face-zoom__panel">
        <div className="review-face-zoom__frame" style={frameStyle}>
          <img
            src={thumbUrl(photo.uid, source)}
            data-testid="review-face-zoom-img"
            data-thumb-size={source}
            alt={t('review.facePair.zoomAlt')}
            decoding="async"
            style={{ ...cropImageStyle(crop), objectFit: 'cover' }}
            onError={() => {
              const next = smallerFaceSource(source)
              if (next !== null) {
                setDegraded({ from: preferred, size: next })
              }
            }}
          />
          {/* The stage's own rectangle, so the face named here and the face
              highlighted on the photo below are visibly the same one. */}
          <span className="review-photo__box" style={boxWithinCrop(bbox, crop)} />
        </div>
        <figcaption className="review-face-zoom__caption">{subject.name}</figcaption>
      </figure>
      <button
        type="button"
        className="review-face-zoom__close kukatko-tap-target"
        aria-label={t('dialog.close')}
        title={t('dialog.close')}
        // The keyboard lands where the eye already is: the look was opened from a
        // button behind the veil, and Tab must not walk back into the game.
        autoFocus
        onClick={onClose}
      >
        <Icon name="x-lg" />
      </button>
    </div>
  )
}
