import { type Bbox } from '../../services/people'
import { type Photo } from '../../services/photos'

import { ReviewStage } from './ReviewStage'

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
 * The review game's photo stage: a {@link ReviewStage} at the game's preview
 * size, fed from the catalogue row.
 *
 * The stage itself — the measured frame, the padded rectangle, the corner anchor
 * out to the photo's page — is shared with the review tools' lightbox, so the
 * game and the tools cannot drift apart. What stays here is only the game's own
 * vocabulary: it thinks in {@link Photo} rows, the stage thinks in a photo's uid
 * and dimensions.
 */
export function ReviewPhoto({ photo, href, bbox, alt }: ReviewPhotoProps) {
  return (
    <ReviewStage
      photoUid={photo.uid}
      fileWidth={photo.file_width}
      fileHeight={photo.file_height}
      orientation={photo.file_orientation ?? 0}
      size={REVIEW_PREVIEW_SIZE}
      bbox={bbox}
      href={href}
      alt={alt}
    />
  )
}
