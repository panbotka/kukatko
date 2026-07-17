import { cropImageStyle, type Frame } from '../../lib/faceGeometry'
import { DEFAULT_TARGET_PX, faceSourceSize } from '../../lib/faceSource'

import { type Bbox } from '../../services/people'
import { thumbUrl } from '../../services/photos'

/** Props for {@link FaceCrop}. */
export interface FaceCropProps {
  /** The photo the face is on. */
  photoUid: string
  /** The crop region, normalised against the photo's *display* frame. */
  crop: Bbox
  /** The photo's display frame, i.e. after EXIF orientation. */
  frame: Frame
  /**
   * Accessible label — who or what this is a picture of. Pass an empty string
   * where the crop sits beside a name that already says it: a second announcement
   * of the same name is noise, and the image is then decorative.
   */
  label: string
  /**
   * Fixed width in CSS pixels. Omit it to let the crop fill its container (pair
   * with `w-100 h-100`), which is what the responsive people grid does. It also
   * sets how sharp the crop needs to be: a 24px chip does not need the thumbnail
   * a 150px tile does.
   */
  size?: number
  /** Extra class names for the crop's container. */
  className?: string
}

/**
 * A face cropped out of a photo, filling whatever box its parent gives it.
 *
 * There is no face-thumbnail endpoint, so the crop is done in the browser: the
 * photo's cached full-frame thumbnail is dropped into an `overflow: hidden`
 * container, scaled and offset (in percentages, so it works at any rendered size)
 * until exactly the crop region shows. Nothing is stretched — the container's
 * `aspect-ratio` is set from the crop's true pixel proportions, so a square crop
 * in a square tile is a square, and callers pass a crop that is already square in
 * pixels (see `squareCrop`).
 *
 * Prefer this over `FaceThumb`, which crops a centre-cropped `tile_*` square as
 * though it were the whole frame and scales its axes independently. This one
 * needs the frame's dimensions to be correct; `FaceThumb` remains for the cluster
 * previews, whose API payload does not carry them.
 */
export function FaceCrop({ photoUid, crop, frame, label, size, className }: FaceCropProps) {
  const [, , cropW, cropH] = crop
  // The crop's real proportions: the same normalised width is more pixels on a
  // wide frame than on a tall one, so the frame decides the box's shape.
  const ratio = cropH > 0 && frame.height > 0 ? (cropW * frame.width) / (cropH * frame.height) : 1
  // Doubled for a 2x display: a crop that is exactly its CSS size is soft there.
  const targetPx = size !== undefined ? size * 2 : DEFAULT_TARGET_PX

  return (
    <div
      className={`position-relative overflow-hidden${className !== undefined ? ` ${className}` : ''}`}
      style={{ aspectRatio: `${ratio}`, ...(size !== undefined && { width: `${size}px` }) }}
    >
      <img
        src={thumbUrl(photoUid, faceSourceSize(crop, frame, targetPx))}
        alt={label}
        aria-hidden={label === '' || undefined}
        loading="lazy"
        decoding="async"
        style={cropImageStyle(crop)}
      />
    </div>
  )
}
