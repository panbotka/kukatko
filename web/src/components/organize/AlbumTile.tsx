import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { albumDisplayTitle } from '../../i18n/albumNames'
import { type AlbumCover, albumCover } from '../../lib/albumCovers'
import { formatCaptureRange } from '../../lib/format'
import { type AlbumSummary } from '../../services/organize'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/**
 * Thumbnail size for one cell of a collage cover. A cell is half a tile wide, so
 * the grid size would ship four times the pixels the browser draws — on a page
 * whose whole point is that it loads a lot of covers at once.
 */
const COLLAGE_THUMB_SIZE = 'tile_224'

/** Props for {@link AlbumTile}. */
export interface AlbumTileProps {
  /** The album with its photo count, effective cover and capture-time span. */
  album: AlbumSummary
  /**
   * What the cover draws, planned across the whole grid so neighbouring tiles do
   * not repeat a photograph (see `lib/albumCovers`). Omitted, the tile plans for
   * itself alone — right for a tile rendered on its own, and the reason a caller
   * rendering many must pass this in.
   */
  cover?: AlbumCover
}

/**
 * A single album card in the albums grid: a square cover, the title, the years
 * the album spans, and the count of photos in it. Links to the album's detail
 * page.
 *
 * The cover is a 2 × 2 collage of the album's newest photos, or one photo for an
 * album too small for a collage and for one whose cover was picked by hand;
 * which photos, and why not simply the newest one, is `lib/albumCovers`. A tile
 * falls back to the empty state only when the album genuinely holds nothing to
 * show.
 *
 * The title is the album's display name: a machine-made English one (`January
 * 2026`, a country) reads in Czech on a Czech UI, everything else verbatim. It
 * is a rendering decision only — the stored title is never touched.
 */
export function AlbumTile({ album, cover }: AlbumTileProps) {
  const { t, i18n } = useTranslation()
  const drawn = cover ?? albumCover(album)
  const range = formatCaptureRange(album.taken_from, album.taken_to)
  const title = albumDisplayTitle(album.title, i18n.language)

  return (
    <Link
      to={`/albums/${album.uid}`}
      className="kk-tile d-block text-decoration-none text-body"
      aria-label={title}
      title={title}
    >
      <div
        className="kk-tile__media mb-1 d-flex align-items-center justify-content-center"
        style={{ aspectRatio: '1 / 1' }}
      >
        {drawn.kind === 'single' && (
          <FadeInImage
            src={thumbUrl(drawn.photoUid, GRID_THUMB_SIZE)}
            alt={title}
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        )}
        {drawn.kind === 'collage' && (
          // The album is named right below and the link carries the title as its
          // accessible name, so the cells are decoration: four alt texts reading
          // the same album name would only make the page longer to listen to.
          <div className="kk-tile__collage" aria-hidden="true">
            {drawn.photoUids.map((uid) => (
              <FadeInImage
                key={uid}
                src={thumbUrl(uid, COLLAGE_THUMB_SIZE)}
                alt=""
                className="w-100 h-100"
                style={{ objectFit: 'cover' }}
              />
            ))}
          </div>
        )}
        {drawn.kind === 'none' && (
          <EmptyState size="sm" title={t('albums.noPhotos')} className="kk-tile__placeholder" />
        )}
        {album.private && (
          <span
            className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75"
            aria-hidden="true"
          >
            {t('albums.private')}
          </span>
        )}
      </div>
      <div className="fw-semibold text-truncate">{title}</div>
      {range !== '' && <div className="kk-text-caption text-secondary text-nowrap">{range}</div>}
      <div className="kk-text-caption text-secondary">
        {t('albums.photoCount', { count: album.photo_count })}
      </div>
    </Link>
  )
}
