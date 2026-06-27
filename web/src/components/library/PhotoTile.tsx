import { useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { GRID_THUMB_SIZE, type Photo, thumbUrl } from '../../services/photos'

import { FavoriteButton } from './FavoriteButton'

/** Props for {@link PhotoTile}. */
export interface PhotoTileProps {
  photo: Photo
  /**
   * When true the tile is a selection target: clicking toggles selection (via
   * {@link PhotoTileProps.onToggleSelect}) instead of navigating to the detail
   * page, and a checkbox overlay reflects {@link PhotoTileProps.selected}.
   */
  selectable?: boolean
  /** Whether this tile is currently selected (only meaningful when selectable). */
  selected?: boolean
  /** Toggles this tile's selection (only meaningful when selectable). */
  onToggleSelect?: (uid: string) => void
  /**
   * When true a favorite heart overlay is shown (a personal toggle available to
   * every user). It is hidden in selection mode so the tile stays a clean
   * selection target. Defaults false.
   */
  favoritable?: boolean
}

/**
 * A single square thumbnail tile in the library grid. By default the tile links
 * to the photo's detail route (`/photos/{uid}`); in selection mode it instead
 * toggles its selection so a grid of tiles can be batch-added to an album or
 * given a label. The image is lazy-loaded and sits in a fixed square box so the
 * grid never shifts as thumbnails stream in; a neutral placeholder is shown until
 * it loads or if it fails.
 */
export function PhotoTile({
  photo,
  selectable = false,
  selected = false,
  onToggleSelect,
  favoritable = false,
}: PhotoTileProps) {
  const { t } = useTranslation()
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)

  const label = photo.title !== '' ? photo.title : photo.file_name
  const taken = photo.taken_at ? new Date(photo.taken_at).toLocaleDateString() : ''
  const alt = taken !== '' ? `${label} — ${taken}` : label

  const inner = (
    <>
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
      {selectable && (
        <Form.Check
          type="checkbox"
          checked={selected}
          readOnly
          tabIndex={-1}
          aria-hidden="true"
          className="position-absolute top-0 start-0 m-1"
        />
      )}
      {photo.private && (
        <span
          className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75"
          aria-hidden="true"
        >
          {t('library.tile.private')}
        </span>
      )}
    </>
  )

  const base = selectable ? (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={label}
      title={label}
      onClick={() => {
        onToggleSelect?.(photo.uid)
      }}
      className={`btn p-0 border-0 d-block position-relative bg-secondary-subtle overflow-hidden rounded w-100${
        selected ? ' ring ring-primary' : ''
      }`}
      style={{
        aspectRatio: '1 / 1',
        outline: selected ? '3px solid var(--bs-primary)' : undefined,
      }}
    >
      {inner}
    </button>
  ) : (
    <Link
      to={`/photos/${photo.uid}`}
      className="d-block position-relative bg-secondary-subtle overflow-hidden rounded"
      style={{ aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
    >
      {inner}
    </Link>
  )

  // The favorite heart sits in a relative wrapper as a sibling of the link/button
  // (never nested inside it — interactive content cannot nest), so toggling a
  // favorite never navigates or toggles selection. Hidden in selection mode.
  return (
    <div className="position-relative">
      {base}
      {favoritable && !selectable && (
        <FavoriteButton
          uid={photo.uid}
          favorite={photo.is_favorite ?? false}
          className="position-absolute bottom-0 end-0 m-1"
        />
      )}
    </div>
  )
}
