import { useTranslation } from 'react-i18next'

import { BlurPlaceholder } from '../BlurPlaceholder'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { type AlbumCover } from '../../lib/albumCovers'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/**
 * Thumbnail size for one cell of a collage cover. A cell is half a tile wide, so
 * the grid size would ship four times the pixels the browser draws — on a page
 * whose whole point is that it loads a lot of covers at once.
 */
export const COLLAGE_THUMB_SIZE = 'tile_224'

/** Props for {@link AlbumCoverMedia}. */
export interface AlbumCoverMediaProps {
  /** What to draw, as `lib/albumCovers` decided it. */
  cover: AlbumCover
  /**
   * Alt text of a single-photo cover. A collage's cells are always decorative,
   * and so is a single photo whose album is already named by the surrounding
   * link — pass `''` then.
   */
  alt: string
  /**
   * Address of the single photo when the server minted one (a signed edge URL
   * for a published bucket). Only for a `single` cover, and only when it is that
   * very photo — the caller vouches for it; otherwise the app's own thumbnail
   * route is used.
   */
  singleSrc?: string
  /** BlurHash painted behind a single photo, under the same proviso as `singleSrc`. */
  singleBlurhash?: string
}

/**
 * The picture inside an album card's media well: one photo filling it, a 2 × 2
 * collage of four, or the empty state for an album with nothing to show. It is
 * the one place that draws an {@link AlbumCover}, shared by the album index's
 * tile and the homepage's featured strip, so an album looks the same in both.
 *
 * It renders only the picture; the caller owns the well (its size, its aspect
 * ratio, any badges laid over it). The well must be positioned and sized — the
 * collage and the images fill it.
 */
export function AlbumCoverMedia({ cover, alt, singleSrc, singleBlurhash }: AlbumCoverMediaProps) {
  const { t } = useTranslation()

  switch (cover.kind) {
    case 'single':
      return (
        <>
          <BlurPlaceholder hash={singleBlurhash} />
          <FadeInImage
            src={singleSrc ?? thumbUrl(cover.photoUid, GRID_THUMB_SIZE)}
            alt={alt}
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        </>
      )
    case 'collage':
      return (
        // The album is named next to the cover, so the cells are decoration:
        // four alt texts reading the same album name would only make the page
        // longer to listen to.
        <div className="kk-tile__collage" aria-hidden="true">
          {cover.photoUids.map((uid) => (
            <FadeInImage
              key={uid}
              src={thumbUrl(uid, COLLAGE_THUMB_SIZE)}
              alt=""
              className="w-100 h-100"
              style={{ objectFit: 'cover' }}
            />
          ))}
        </div>
      )
    case 'none':
      return <EmptyState size="sm" title={t('albums.noPhotos')} className="kk-tile__placeholder" />
  }
}
