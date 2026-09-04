import { useState } from 'react'

import { type Bbox, faceCropUrl } from '../../services/people'
import { FadeInImage } from '../FadeInImage'

/** Props for {@link FaceCrop}. */
export interface FaceCropProps {
  /** The photo the face is on. */
  photoUid: string
  /** The face's normalised `[x, y, w, h]` box, in the photo's display space. */
  bbox: Bbox
  /**
   * Accessible label — who or what this is a picture of. Pass an empty string
   * where the crop sits beside a name that already says it: a second announcement
   * of the same name is noise, and the image is then decorative.
   */
  label: string
  /**
   * Fixed width in CSS pixels. Omit it to let the crop fill its container (pair
   * with `w-100 h-100`), which is what a responsive grid does.
   */
  size?: number
  /** Extra class names for the crop's container. */
  className?: string
  /**
   * Called once when the rendition cannot be produced and the crop has given up
   * — there is no picture of this face and there never will be one this render.
   * Callers that *ask about* a face (the outlier section) use it to take the
   * face out of the list rather than offer a grey square as a question; callers
   * that merely show one can leave it out and keep the empty well.
   */
  onUnavailable?: () => void
}

/**
 * A face cropped out of a photo, filling whatever box its parent gives it.
 *
 * The crop is cut **server-side** and fetched as its own small square rendition
 * (`faceCropUrl` → `GET /photos/{uid}/face?box=…`), of some 15 kB. It used to be
 * done in the page: a whole-frame preview was dropped into an `overflow: hidden`
 * box and scaled until only the face showed, which meant downloading a
 * photograph to paint a thumbnail. Measured on one person's page, the outlier
 * section fetched 290 `fit_1280` previews — 1280 × 960 pixels each — so the
 * reader could see 290 windows of 96 px. Now the browser receives the window.
 *
 * The geometry is the renderer's: it pads the box for context, squares it in
 * pixel space and slides it back inside the frame rather than clipping, so the
 * component needs neither the padding nor the photo's dimensions and every face
 * in the app is cropped the same way. That the answer is square is why the box
 * here is one too.
 *
 * The image loads lazily ({@link FadeInImage}'s default), which is what lets a
 * section of hundreds of faces request only the ones the reader has reached. A
 * rendition that cannot be produced — a photograph with no usable preview, a box
 * naming nothing — leaves the caller's empty well rather than the browser's
 * broken-image glyph, and says so through `onUnavailable` for the callers whose
 * tile has no meaning without the picture.
 */
export function FaceCrop({ photoUid, bbox, label, size, className, onUnavailable }: FaceCropProps) {
  const [failed, setFailed] = useState(false)

  return (
    <div
      className={`position-relative overflow-hidden${className !== undefined ? ` ${className}` : ''}`}
      style={{ aspectRatio: '1', ...(size !== undefined && { width: `${size}px` }) }}
    >
      {!failed && (
        <FadeInImage
          src={faceCropUrl(photoUid, bbox)}
          alt={label}
          aria-hidden={label === '' || undefined}
          skeleton
          className="w-100 h-100"
          style={{ objectFit: 'cover' }}
          onError={() => {
            setFailed(true)
            onUnavailable?.()
          }}
        />
      )}
    </div>
  )
}
