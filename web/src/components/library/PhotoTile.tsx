import { useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useThumbSrc } from '../../hooks/useThumbSrc'
import { formatDate, formatDuration } from '../../lib/format'
import { type Photo } from '../../services/photos'

import { FavoriteButton } from './FavoriteButton'

/** Whether the photo is a playable video or a live photo (has a motion clip). */
function isPlayable(photo: Photo): boolean {
  return photo.media_type === 'video' || photo.media_type === 'live'
}

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
  /**
   * Query string (without the leading `?`) appended to the detail link so the
   * detail page inherits the originating list's order and scope for prev/next and
   * Back. Empty/undefined links to the bare detail route.
   */
  detailQuery?: string
  /**
   * When true the tile shows the keyboard focus highlight — the target of the
   * grid's arrow/`hjkl` navigation. Purely visual; it does not steal DOM focus.
   */
  focused?: boolean
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
  detailQuery,
  focused = false,
}: PhotoTileProps) {
  const { t, i18n } = useTranslation()
  const [loaded, setLoaded] = useState(false)
  // The thumbnail address comes from the payload, not from the UID: only the
  // server can sign it. A signed URL expires, so a failed load gets one retry
  // with a freshly fetched one before the tile gives up.
  const thumb = useThumbSrc(photo.uid, photo.thumb_url)

  const label = photo.title !== '' ? photo.title : photo.file_name
  // The tile shows no date of its own; the only one it carries is in the alt text,
  // and an estimated date is marked there too ("cca 1950") so it cannot be read as
  // a known one. The grid itself goes on sorting by taken_at exactly as before.
  const takenDate = photo.taken_at ? formatDate(photo.taken_at, i18n.language) : ''
  const taken =
    takenDate !== '' && photo.taken_at_estimated === true
      ? `${t('photo.metadata.estimatedMarker')} ${takenDate}`
      : takenDate
  const alt = taken !== '' ? `${label} — ${taken}` : label

  const inner = (
    <>
      {!thumb.failed && (
        <img
          src={thumb.src}
          alt={alt}
          loading="lazy"
          decoding="async"
          onLoad={() => {
            setLoaded(true)
          }}
          onError={thumb.onError}
          className="w-100 h-100"
          style={{
            objectFit: 'cover',
            opacity: loaded ? 1 : 0,
            transition: 'opacity var(--kk-duration-base) var(--kk-ease-standard)',
          }}
        />
      )}
      {thumb.failed && (
        <span
          className="d-flex w-100 h-100 align-items-center justify-content-center text-secondary kk-text-caption p-2 text-center"
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
      {isPlayable(photo) && (
        <span
          className="position-absolute bottom-0 start-0 m-1 badge text-bg-dark opacity-75 d-inline-flex align-items-center gap-1"
          role="img"
          aria-label={
            photo.media_type === 'live' ? t('library.tile.live') : t('library.tile.video')
          }
        >
          <span aria-hidden="true">▶</span>
          {photo.duration_ms !== undefined && photo.duration_ms > 0 && (
            <span>{formatDuration(photo.duration_ms)}</span>
          )}
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
      className={`kk-tile__media btn p-0 border-0 d-block w-100${
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
      to={
        detailQuery !== undefined && detailQuery !== ''
          ? `/photos/${photo.uid}?${detailQuery}`
          : `/photos/${photo.uid}`
      }
      className="kk-tile__media d-block"
      style={{ aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
    >
      {inner}
    </Link>
  )

  // The favorite heart sits in a relative wrapper as a sibling of the link/button
  // (never nested inside it — interactive content cannot nest), so toggling a
  // favorite never navigates or toggles selection. Hidden in selection mode so the
  // tile stays a clean selection target. Star rating and pick/reject flagging are
  // deliberately absent from the tile; they live on the photo detail page.
  return (
    <div
      className={`kk-tile position-relative${focused ? ' kukatko-tile-focused' : ''}`}
      data-focused={focused ? 'true' : undefined}
    >
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
