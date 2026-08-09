import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

/**
 * The preview size of one half of the pair. `fit_*` (whole frame), because the
 * question is whether the two *photos* are the same — a centre-cropped `tile_*`
 * would hide exactly the edges where a crop, a rotation or a different shot most
 * often gives itself away. 1280 rather than the single-photo stage's size is not
 * a saving: each half is at most half the screen wide.
 */
export const DUPLICATE_PREVIEW_SIZE = 'fit_1280'

/** Props for {@link ReviewDuplicate}. */
export interface ReviewDuplicateProps {
  /** The pair's first photo — the one the duplicates page would keep. */
  photo: Photo
  /** The pair's second photo. */
  other: Photo
  /** Builds the path of a photo's own page, for the corner anchors. */
  href: (uid: string) => string
}

/** One half of the pair: the frame, its size caption and its way out. */
function DuplicateHalf({ photo, href, label }: { photo: Photo; href: string; label: string }) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)

  // A new photo is a clean slate for the load-failure flag.
  useEffect(() => {
    setFailed(false)
  }, [photo.uid])

  return (
    <figure className="review-duplicate__half" data-testid="review-duplicate-half">
      <div className="review-duplicate__frame">
        {failed ? (
          <div className="d-flex align-items-center justify-content-center w-100 h-100 text-secondary">
            <Icon name="images" />
          </div>
        ) : (
          <img
            src={thumbUrl(photo.uid, DUPLICATE_PREVIEW_SIZE)}
            alt={label}
            decoding="async"
            onError={() => {
              setFailed(true)
            }}
            className="review-duplicate__img"
          />
        )}
        <Link
          to={href}
          target="_blank"
          rel="noopener noreferrer"
          className="review-photo__open"
          aria-label={t('review.openPhoto')}
          title={t('review.openPhoto')}
        >
          <Icon name="box-arrow-up-right" />
        </Link>
      </div>
      {/* The dimensions and the file name are what actually settle a lot of these
          pairs — the same shot exported twice differs in nothing a thumbnail can
          show, and the numbers say which copy is which. */}
      <figcaption className="review-duplicate__meta">
        <span className="review-duplicate__name" title={photo.file_name}>
          {photo.file_name}
        </span>
        <span className="text-secondary">
          {t('review.duplicate.dimensions', {
            width: photo.file_width,
            height: photo.file_height,
          })}
        </span>
      </figcaption>
    </figure>
  )
}

/**
 * The duplicate check's stage: the two photos side by side, each with its file
 * name and pixel size under it.
 *
 * Side by side, not stacked, because the whole question is a comparison and a
 * pair the eye has to scroll between is a pair the eye cannot compare. On a
 * portrait phone there is no width for two frames, so the row becomes a column
 * there — a swap the stage's own height absorbs, since it is the remainder of
 * the screen either way (see review.css).
 *
 * Each half carries the same quiet corner anchor the single-photo stage does, so
 * either copy can be opened and inspected properly without leaving the game.
 */
export function ReviewDuplicate({ photo, other, href }: ReviewDuplicateProps) {
  const { t } = useTranslation()
  return (
    <div className="review-duplicate" data-testid="review-duplicate">
      <DuplicateHalf photo={photo} href={href(photo.uid)} label={t('review.duplicate.altFirst')} />
      <DuplicateHalf photo={other} href={href(other.uid)} label={t('review.duplicate.altSecond')} />
    </div>
  )
}
