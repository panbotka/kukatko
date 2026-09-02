import { useTranslation } from 'react-i18next'

import { useThumbSrc } from '../../hooks/useThumbSrc'
import { formatDuration } from '../../lib/format'
import { type PhotoHandoff } from '../../lib/photoHandoff'
import { photoLabel } from '../../lib/photoTitle'
import { formatTakenLabel } from '../../lib/takenDate'
import { tileRenditionName, tileUsesPreviewURL } from '../../lib/tileRendition'
import { type Photo, thumbUrl } from '../../services/photos'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'
import { MorphLink } from '../morph/MorphLink'

import { FavoriteButton } from './FavoriteButton'

/** Whether the photo is a playable video or a live photo (has a motion clip). */
function isPlayable(photo: Photo): boolean {
  return photo.media_type === 'video' || photo.media_type === 'live'
}

/** A tile's image address, and how to re-read it from a refreshed payload. */
interface TileSource {
  /** What goes into `<img src>`. */
  src: string
  /** Reads the same rendition off a freshly fetched payload; see {@link useThumbSrc}. */
  refresh: (photo: Photo) => string
  /**
   * Whether {@link TileSource.src} keeps the photograph's own proportions. Only
   * such an address may be handed to the viewer (see `lib/photoHandoff`): a
   * square crop painted under a fitted photograph shows the wrong part of it.
   */
  aspect: boolean
}

/**
 * Picks the rendition a tile draws. A square tile takes the square crop
 * (`thumb_url`). A justified one takes the aspect-preserving `preview_url` the
 * payload carries — unless it was laid out wide enough to outrun that rung, in
 * which case it asks the thumb route for a bigger one (see `lib/tileRendition`).
 * A route address never expires, so there is nothing to re-read it from and its
 * picker gives up instead, which the tile shows as its "unavailable" state.
 */
function tileSource(photo: Photo, fill: boolean, tileWidth?: number): TileSource {
  if (!fill) {
    return { src: photo.thumb_url, refresh: (fresh) => fresh.thumb_url, aspect: false }
  }
  const dpr = typeof window === 'undefined' ? 1 : window.devicePixelRatio
  if (tileWidth !== undefined && !tileUsesPreviewURL(tileWidth, dpr)) {
    return {
      src: thumbUrl(photo.uid, tileRenditionName(tileWidth, dpr)),
      refresh: () => '',
      aspect: true,
    }
  }
  return {
    src: photo.preview_url ?? photo.thumb_url,
    refresh: (fresh) => fresh.preview_url ?? fresh.thumb_url,
    // A payload from a backend too old to mint `preview_url` leaves the square
    // crop as the only address there is; the tile draws it (cropped by
    // `object-fit`) but it is not the photograph's shape and is not handed on.
    aspect: photo.preview_url !== undefined,
  }
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
   * Called with this photo's new favorite state whenever the heart flips (the
   * optimistic flip and a rollback alike). A page that also favorites the photo
   * by another route — the library's `f` shortcut — passes it so both share one
   * baseline; a page whose only route is the heart needs none.
   */
  onFavoriteChange?: (uid: string, favorite: boolean) => void
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
   * When true the tile fills the box it is placed in instead of squaring itself
   * — what the justified wall does, where the row has already decided the tile's
   * width and height from the photo's own proportions. Defaults false, the
   * square tile every other grid draws.
   */
  fill?: boolean
  /**
   * The width the tile is laid out at, in CSS pixels, when the caller knows it
   * (the justified wall does). It only picks the thumbnail rendition — a tile
   * spanning half a desktop needs more pixels than the one every payload
   * carries — so leaving it out costs nothing but that choice.
   */
  tileWidth?: number
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
 * A single thumbnail tile in the library grid — square by default, or the shape
 * of its own photograph when the caller has laid the box out for it (`fill`,
 * which is what the justified wall does). By default the tile links to the
 * photo's detail route (`/photos/{uid}`); in selection mode it instead toggles
 * its selection so a grid of tiles can be batch-added to an album or given a
 * label. The image is lazy-loaded and sits in a box whose size is known before
 * it arrives, so the grid never shifts as thumbnails stream in; until it loads
 * the box shows the photograph's own blurred stand-in (its `blurhash`), or the
 * neutral well when there is none — and the neutral well again if it fails.
 */
export function PhotoTile({
  photo,
  selectable = false,
  selectFirst = false,
  selected = false,
  anySelected = false,
  onToggleSelect,
  favoritable = false,
  onFavoriteChange,
  detailQuery,
  focused = false,
  fill = false,
  tileWidth,
  extras,
}: PhotoTileProps) {
  const { t, i18n } = useTranslation()
  // The thumbnail address comes from the payload, not from the UID: only the
  // server can sign it. A signed URL expires, so a failed load gets one retry
  // with a freshly fetched one before the tile gives up.
  const source = tileSource(photo, fill, tileWidth)
  const thumb = useThumbSrc(photo.uid, source.src, source.refresh)

  const label = photoLabel(photo, i18n.language)
  // The tile shows no date of its own; the only one it carries is in the alt text,
  // and an estimated date is marked there too ("cca 1950") so it cannot be read as
  // a known one. A date stated as a whole period reads as that period ("1974"),
  // never as the day it is anchored at. The grid itself goes on sorting by
  // taken_at exactly as before.
  const taken = formatTakenLabel(photo, t, i18n.language)
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
          // The photograph's own blurred stand-in, painted the moment the row
          // arrives so the tile is never an empty grey well; the thumbnail fades
          // in over it. A photo without a hash keeps that neutral well.
          blurhash={photo.blurhash}
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

  // The tile root is ALWAYS a link — a `MorphLink`, which renders exactly one — so
  // its element TYPE never changes when the grid flips into selection-first mode
  // (selection going empty↔non-empty). Were the root swapped between <a> and
  // <button>, React would unmount the whole tile subtree and mount a fresh <img>
  // — re-running the load-in fade on every visible tile at once (the reported
  // whole-grid flicker). Keeping one element means only its click behaviour and
  // ARIA role change, and the <img> stays mounted.
  //
  // When selection-first the whole media box toggles this tile's selection: it is
  // exposed as a toggle button (role="button" + aria-pressed) and navigation is
  // suppressed (preventDefault, which both react-router and the morph honour —
  // each runs our handler first and stands down when the event is
  // defaultPrevented). A native <a> already activates on Enter (→ a click we
  // intercept), but not on Space, so Space is handled explicitly to keep it
  // operable as a button. When not selection-first it is a plain link to the
  // detail page and only the corner checkmark selects.
  const base = (
    <MorphLink
      // The tile is the half of the grid ⇄ viewer morph that the user clicked:
      // it grows into the viewer's figure for the same photograph. A browser
      // without the View Transitions API — or a reader who asked for reduced
      // motion — gets the plain link this degrades to.
      morphId={photo.uid}
      to={
        detailQuery !== undefined && detailQuery !== ''
          ? `/photos/${photo.uid}?${detailQuery}`
          : `/photos/${photo.uid}`
      }
      // A tile drawn at the photograph's own proportions has already downloaded
      // exactly what the viewer wants for its first frame — hand the address over
      // rather than let the viewer mint a second, differently-signed one for the
      // same rendition. A tile showing a square crop hands over nothing: that
      // crop is not the photograph (see `lib/photoHandoff`).
      state={
        source.aspect
          ? ({ uid: photo.uid, previewUrl: thumb.src } satisfies PhotoHandoff)
          : undefined
      }
      className="kk-tile__media d-block"
      // A justified tile is sized by its row (which has already applied the
      // photo's own proportions); every other grid squares it.
      style={fill ? { height: '100%', width: '100%' } : { aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
      role={selectFirst ? 'button' : undefined}
      aria-pressed={selectFirst ? selected : undefined}
      onClick={
        selectFirst
          ? (event) => {
              event.preventDefault()
              onToggleSelect?.(photo.uid, event.shiftKey)
            }
          : undefined
      }
      onKeyDown={
        selectFirst
          ? (event) => {
              if (event.key === ' ') {
                event.preventDefault()
                onToggleSelect?.(photo.uid, event.shiftKey)
              }
            }
          : undefined
      }
    >
      {inner}
    </MorphLink>
  )

  // The checkmark control and the favorite heart both sit in a relative wrapper
  // as siblings of the tile link (never nested inside it — interactive content
  // cannot nest), so toggling selection or a favorite never navigates. The
  // checkmark is shown while the tile is selectable; the heart is hidden once the
  // tile is a selection target so it stays a clean pick. Star rating and
  // pick/reject flagging are deliberately absent from the tile — they live on the
  // photo detail page.
  return (
    <div
      style={fill ? { height: '100%' } : undefined}
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
          onChange={
            onFavoriteChange === undefined
              ? undefined
              : (favorite) => {
                  onFavoriteChange(photo.uid, favorite)
                }
          }
        />
      )}
    </div>
  )
}
