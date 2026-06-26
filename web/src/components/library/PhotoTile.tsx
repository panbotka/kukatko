import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { GRID_THUMB_SIZE, type Photo, thumbUrl } from '../../services/photos'

/**
 * A single square thumbnail tile in the library grid. The tile links to the
 * photo's detail route (`/photos/{uid}`). The image is lazy-loaded and sits in a
 * fixed square box so the grid never shifts as thumbnails stream in; a neutral
 * placeholder is shown until it loads or if it fails.
 */
export function PhotoTile({ photo }: { photo: Photo }) {
  const { t } = useTranslation()
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)

  const label = photo.title !== '' ? photo.title : photo.file_name
  const taken = photo.taken_at ? new Date(photo.taken_at).toLocaleDateString() : ''
  const alt = taken !== '' ? `${label} — ${taken}` : label

  return (
    <Link
      to={`/photos/${photo.uid}`}
      className="d-block position-relative bg-secondary-subtle overflow-hidden rounded"
      style={{ aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
    >
      {!failed && (
        <img
          src={thumbUrl(photo.uid, GRID_THUMB_SIZE)}
          alt={alt}
          loading="lazy"
          decoding="async"
          onLoad={() => {
            setLoaded(true)
          }}
          onError={() => {
            setFailed(true)
          }}
          className="w-100 h-100"
          style={{
            objectFit: 'cover',
            opacity: loaded ? 1 : 0,
            transition: 'opacity 0.2s ease-in',
          }}
        />
      )}
      {failed && (
        <span
          className="d-flex w-100 h-100 align-items-center justify-content-center text-secondary small p-2 text-center"
          role="img"
          aria-label={t('library.tile.unavailable')}
        >
          {t('library.tile.unavailable')}
        </span>
      )}
      {photo.private && (
        <span
          className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75"
          aria-hidden="true"
        >
          {t('library.tile.private')}
        </span>
      )}
    </Link>
  )
}
