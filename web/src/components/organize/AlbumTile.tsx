import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

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
      className="d-block text-decoration-none text-body"
      aria-label={album.title}
      title={album.title}
    >
      <div
        className="position-relative bg-secondary-subtle overflow-hidden rounded mb-1 d-flex align-items-center justify-content-center"
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
          <span className="text-secondary small p-2 text-center">{t('albums.noCover')}</span>
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
      <div className="small text-secondary">
        {t('albums.photoCount', { count: album.photo_count })}
      </div>
    </Link>
  )
}
