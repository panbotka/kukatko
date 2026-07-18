import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { formatCaptureRange } from '../../lib/format'
import { type AlbumSummary } from '../../services/organize'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/** Props for {@link AlbumTile}. */
export interface AlbumTileProps {
  /** The album with its photo count, effective cover and capture-time span. */
  album: AlbumSummary
}

/**
 * A single album card in the albums grid: a square cover thumbnail, the title,
 * the years the album spans, and the count of photos in it. Links to the album's
 * detail page.
 *
 * The cover is the album's effective one — the hand-picked cover when there is
 * one, and the album's newest photo otherwise — so a tile only falls back to the
 * empty state when the album genuinely holds nothing to show.
 */
export function AlbumTile({ album }: AlbumTileProps) {
  const { t } = useTranslation()
  const cover = album.cover_uid
  const range = formatCaptureRange(album.taken_from, album.taken_to)

  return (
    <Link
      to={`/albums/${album.uid}`}
      className="kk-tile d-block text-decoration-none text-body"
      aria-label={album.title}
      title={album.title}
    >
      <div
        className="kk-tile__media mb-1 d-flex align-items-center justify-content-center"
        style={{ aspectRatio: '1 / 1' }}
      >
        {cover !== undefined && cover !== '' ? (
          <FadeInImage
            src={thumbUrl(cover, GRID_THUMB_SIZE)}
            alt={album.title}
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        ) : (
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
      <div className="fw-semibold text-truncate">{album.title}</div>
      {range !== '' && <div className="kk-text-caption text-secondary text-nowrap">{range}</div>}
      <div className="kk-text-caption text-secondary">
        {t('albums.photoCount', { count: album.photo_count })}
      </div>
    </Link>
  )
}
