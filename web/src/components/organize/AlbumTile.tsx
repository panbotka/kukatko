import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../EmptyState'

import { type AlbumCount } from '../../services/organize'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/** Props for {@link AlbumTile}. */
export interface AlbumTileProps {
  /** The album with its photo count. */
  album: AlbumCount
}

/**
 * A single album card in the albums grid: a square cover thumbnail (cropped from
 * the album's chosen cover photo, or a neutral placeholder), the title, and the
 * count of photos in the album. Links to the album's detail page.
 */
export function AlbumTile({ album }: AlbumTileProps) {
  const { t } = useTranslation()
  const cover = album.cover_photo_uid

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
          <img
            src={thumbUrl(cover, GRID_THUMB_SIZE)}
            alt={album.title}
            loading="lazy"
            decoding="async"
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        ) : (
          <EmptyState size="sm" title={t('albums.noCover')} className="kk-tile__placeholder" />
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
      <div className="kk-text-caption text-secondary">
        {t('albums.photoCount', { count: album.photo_count })}
      </div>
    </Link>
  )
}
