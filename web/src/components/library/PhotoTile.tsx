import { type KeyboardEvent, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useRating } from '../../hooks/useRating'
import { useThumbSrc } from '../../hooks/useThumbSrc'
import { formatDate, formatDuration } from '../../lib/format'
import { isTypingElement, ratingHotkey } from '../../lib/ratingHotkeys'
import { type Photo } from '../../services/photos'

import { FavoriteButton } from './FavoriteButton'
import { FlagControl } from './FlagControl'
import { RatingStars } from './RatingStars'

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
   * When true a compact star + pick/reject overlay is shown (a personal
   * annotation available to every user) and number keys `0`–`5` / `p` / `r` on
   * the focused tile set the rating/flag. Reject-flagged tiles are dimmed and get
   * a badge. Hidden in selection mode, like the favorite heart. Defaults false.
   */
  ratable?: boolean
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
  ratable = false,
  detailQuery,
  focused = false,
}: PhotoTileProps) {
  const { t, i18n } = useTranslation()
  const [loaded, setLoaded] = useState(false)
  // The thumbnail address comes from the payload, not from the UID: only the
  // server can sign it. A signed URL expires, so a failed load gets one retry
  // with a freshly fetched one before the tile gives up.
  const thumb = useThumbSrc(photo.uid, photo.thumb_url)
  // The optimistic rating hook is always instantiated (React hook rules); its
  // overlay and hotkeys only render/fire when the tile is ratable.
  const rating = useRating(photo.uid, photo.rating ?? 0, photo.flag ?? 'none')

  const label = photo.title !== '' ? photo.title : photo.file_name
  const taken = photo.taken_at ? formatDate(photo.taken_at, i18n.language) : ''
  const alt = taken !== '' ? `${label} — ${taken}` : label

  const showRating = ratable && !selectable
  const rejected = showRating && rating.flag === 'reject'

  // Number keys 0–5 set the rating, p/r set pick/reject on the focused tile.
  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.ctrlKey || event.metaKey || event.altKey || isTypingElement(event.target)) {
      return
    }
    const action = ratingHotkey(event.key)
    if (action === null) {
      return
    }
    event.preventDefault()
    if (action.kind === 'rating') {
      rating.setRating(action.value)
    } else {
      rating.setFlag(action.value)
    }
  }

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
            transition: 'opacity 0.2s ease-in',
          }}
        />
      )}
      {thumb.failed && (
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
      {rejected && (
        <span className="position-absolute bottom-0 start-0 m-1 badge text-bg-danger opacity-75">
          {t('rating.rejected')}
        </span>
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
      to={
        detailQuery !== undefined && detailQuery !== ''
          ? `/photos/${photo.uid}?${detailQuery}`
          : `/photos/${photo.uid}`
      }
      className="d-block position-relative bg-secondary-subtle overflow-hidden rounded"
      style={{ aspectRatio: '1 / 1', opacity: rejected ? 0.55 : undefined }}
      aria-label={label}
      title={label}
      onKeyDown={showRating ? handleKeyDown : undefined}
    >
      {inner}
    </Link>
  )

  // The favorite heart sits in a relative wrapper as a sibling of the link/button
  // (never nested inside it — interactive content cannot nest), so toggling a
  // favorite never navigates or toggles selection. Hidden in selection mode.
  // The rating overlay and favorite heart sit in the relative wrapper as siblings
  // of the link/button (never nested — interactive content cannot nest), so
  // rating or favoriting never navigates or toggles selection. Both are hidden in
  // selection mode so the tile stays a clean selection target.
  return (
    <div
      className={`position-relative${focused ? ' kukatko-tile-focused' : ''}`}
      data-focused={focused ? 'true' : undefined}
    >
      {base}
      {showRating && (
        <span
          className="position-absolute top-0 start-0 m-1 d-inline-flex align-items-center gap-1 rounded px-1"
          style={{ backgroundColor: 'rgba(0, 0, 0, 0.45)' }}
        >
          <RatingStars
            rating={rating.rating}
            onRate={rating.setRating}
            disabled={rating.pending}
            size={14}
          />
          <FlagControl
            flag={rating.flag}
            onFlag={rating.setFlag}
            disabled={rating.pending}
            size={13}
          />
        </span>
      )}
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
