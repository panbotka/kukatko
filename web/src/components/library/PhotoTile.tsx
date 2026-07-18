import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useThumbSrc } from '../../hooks/useThumbSrc'
import { formatDate, formatDuration } from '../../lib/format'
import { type Photo } from '../../services/photos'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

import { FavoriteButton } from './FavoriteButton'

/** Whether the photo is a playable video or a live photo (has a motion clip). */
function isPlayable(photo: Photo): boolean {
  return photo.media_type === 'video' || photo.media_type === 'live'
}

/** Props for {@link PhotoTile}. */
export interface PhotoTileProps {
  photo: Photo
  /**
   * When true the tile offers selection: a circular checkmark control appears in
   * a corner on hover (and always once anything in the grid is selected), and
   * clicking it toggles this tile's selection (via
   * {@link PhotoTileProps.onToggleSelect}) without opening the photo.
   */
  selectable?: boolean
  /**
   * When true the whole tile is a selection target — clicking anywhere on it
   * toggles selection instead of navigating. The grid sets this once a selection
   * exists (or in an explicit selection mode) so a run of tiles can be picked
   * quickly, mirroring modern photo apps. When false the tile still navigates and
   * only the corner checkmark toggles.
   */
  selectFirst?: boolean
  /** Whether this tile is currently selected (only meaningful when selectable). */
  selected?: boolean
  /**
   * Whether any tile in the grid is selected. Keeps every tile's checkmark shown
   * (not just on hover) so the selection is visible and reversible at a glance.
   */
  anySelected?: boolean
  /**
   * Toggles this tile's selection (only meaningful when selectable). The click's
   * Shift state rides along so the grid can turn Shift+click into a contiguous
   * range selection.
   */
  onToggleSelect?: (uid: string, shiftKey?: boolean) => void
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
  /**
   * Page-supplied overlays stamped onto the tile (badges, per-tile actions such
   * as the /expand page's similarity percentage and reject button). Rendered as
   * siblings of the link/button inside the tile's relative wrapper — never
   * nested inside it, since interactive content cannot nest — so an interactive
   * extra never navigates or toggles selection.
   */
  extras?: React.ReactNode
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
  selectFirst = false,
  selected = false,
  anySelected = false,
  onToggleSelect,
  favoritable = false,
  detailQuery,
  focused = false,
  extras,
}: PhotoTileProps) {
  const { t, i18n } = useTranslation()
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
        // The load-in fade + settle and the hover zoom (its target scale lives in
        // the `.kukatko-photo-grid` CSS) both ride the `.kk-media-img` transition.
        <FadeInImage
          src={thumb.src}
          alt={alt}
          onError={thumb.onError}
          className="w-100 h-100"
          style={{ objectFit: 'cover' }}
        />
      )}
      {taken !== '' && (
        <span className="kk-tile__caption" aria-hidden="true">
          {taken}
        </span>
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
      {isPlayable(photo) && (
        <span
          // Top-end, not bottom-start: the hover date owns the bottom reading
          // corner now, and a video is never part of a RAW+JPEG stack, so this
          // never collides with the stack badge sharing the corner.
          className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75 d-inline-flex align-items-center gap-1"
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
      {photo.stack_count !== undefined && photo.stack_count > 1 && (
        <span
          className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75 d-inline-flex align-items-center gap-1"
          role="img"
          aria-label={t('library.tile.stackCount', { count: photo.stack_count })}
        >
          <Icon name="images" />
          <span>{photo.stack_count}</span>
        </span>
      )}
    </>
  )

  // When the tile is selection-first the whole media box toggles selection; when
  // it is not (a plain grid, or a selectable grid with nothing yet picked) it
  // stays a link to the detail page and only the corner checkmark selects.
  const base = selectFirst ? (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={label}
      title={label}
      onClick={(event) => {
        onToggleSelect?.(photo.uid, event.shiftKey)
      }}
      className="kk-tile__media btn p-0 border-0 d-block w-100"
      style={{ aspectRatio: '1 / 1' }}
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

  // The checkmark control and the favorite heart both sit in a relative wrapper
  // as siblings of the link/button (never nested inside it — interactive content
  // cannot nest), so toggling selection or a favorite never navigates. The
  // checkmark is shown while the tile is selectable; the heart is hidden once the
  // tile is a selection target so it stays a clean pick. Star rating and
  // pick/reject flagging are deliberately absent from the tile — they live on the
  // photo detail page.
  return (
    <div
      className={`kk-tile position-relative${selected ? ' kk-tile--selected' : ''}${
        anySelected ? ' kk-tile--checks' : ''
      }${focused ? ' kukatko-tile-focused' : ''}`}
      data-focused={focused ? 'true' : undefined}
    >
      {base}
      {selectable && (
        <button
          type="button"
          className={`kk-tile__check${selected ? ' kk-tile__check--on' : ''}`}
          aria-pressed={selected}
          aria-label={t('selection.toggle', { name: label })}
          title={t('selection.toggle', { name: label })}
          onClick={(event) => {
            onToggleSelect?.(photo.uid, event.shiftKey)
          }}
        >
          {selected && <Icon name="check-lg" />}
        </button>
      )}
      {extras}
      {favoritable && !selectFirst && (
        <FavoriteButton
          uid={photo.uid}
          favorite={photo.is_favorite ?? false}
          className="position-absolute bottom-0 end-0 m-1"
        />
      )}
    </div>
  )
}
