import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

import { type Photo } from '../../services/people'

/** Props for {@link SubjectPhotoTile}. */
export interface SubjectPhotoTileProps {
  /** The photo to render. */
  photo: Photo
  /** Whether this photo is the subject's current cover. */
  isCover: boolean
  /** Whether the viewer may change the cover. */
  canSetCover: boolean
  /** True while a cover change is in flight (disables the action). */
  busy: boolean
  /** Sets this photo as the subject's cover. */
  onSetCover: (photoUid: string) => void
  /**
   * When true the tile offers selection: a circular checkmark control appears in
   * the top-start corner on hover (and always once anything in the gallery is
   * selected), and clicking it toggles this tile's selection (via
   * {@link SubjectPhotoTileProps.onToggleSelect}) without opening the photo.
   * Mirrors {@link import('../library/PhotoTile').PhotoTileProps.selectable}.
   */
  selectable?: boolean
  /**
   * When true the whole tile is a selection target — clicking anywhere on it
   * toggles selection instead of navigating — and the set-cover action steps
   * aside so the tile stays a clean pick. The gallery sets this once a selection
   * exists, exactly as the library grid does.
   */
  selectFirst?: boolean
  /** Whether this tile is currently selected (only meaningful when selectable). */
  selected?: boolean
  /**
   * Whether any tile in the gallery is selected. Keeps every tile's checkmark
   * shown (not just on hover) so the selection is visible at a glance.
   */
  anySelected?: boolean
  /** Toggles this tile's selection (only meaningful when selectable). */
  onToggleSelect?: (uid: string) => void
  /**
   * Query string (without the leading `?`) appended to the detail link so the
   * viewer inherits this subject's scope and order for prev/next and Back — the
   * subject gallery scopes it to `person=<subjectUid>`. Mirrors
   * {@link import('../library/PhotoTile').PhotoTileProps.detailQuery}.
   * Empty/undefined links to the bare detail route.
   */
  detailQuery?: string
}

/**
 * A square photo tile for a subject's gallery: links to the photo detail and,
 * for editors, overlays a quiet "set as cover" action — a small icon-only disc
 * in the bottom-start corner, revealed on hover/focus of the tile rather than a
 * loud labelled button on every one. The current cover keeps its (filled) disc
 * shown as a marker, and on touch (no hover) every disc stays visible so the
 * action is still reachable. An editor's tile also carries the library's corner
 * selection checkmark from the outset, so a batch of the person's photos can be
 * picked and bulk-edited without first entering a selection mode.
 */
export function SubjectPhotoTile({
  photo,
  isCover,
  canSetCover,
  busy,
  onSetCover,
  selectable = false,
  selectFirst = false,
  selected = false,
  anySelected = false,
  onToggleSelect,
  detailQuery,
}: SubjectPhotoTileProps) {
  const { t } = useTranslation()
  const label = photo.title !== '' ? photo.title : photo.file_name

  // The tile root is ALWAYS a <Link>, as in the library grid, so its element
  // TYPE never changes when the gallery flips into selection-first mode and the
  // thumbnails are not remounted (which would re-run their load-in fade). Only
  // the click behaviour and ARIA role change: when selection-first the box is
  // exposed as a toggle button and navigation is suppressed (preventDefault,
  // which react-router honours). A native <a> activates on Enter but not on
  // Space, so Space is handled explicitly to keep it operable as a button.
  const media = (
    <Link
      to={
        detailQuery !== undefined && detailQuery !== ''
          ? `/photos/${photo.uid}?${detailQuery}`
          : `/photos/${photo.uid}`
      }
      className="d-block bg-secondary-subtle overflow-hidden rounded"
      style={{
        aspectRatio: '1 / 1',
        outline: selected ? '3px solid var(--bs-primary)' : undefined,
      }}
      aria-label={label}
      title={label}
      role={selectFirst ? 'button' : undefined}
      aria-pressed={selectFirst ? selected : undefined}
      onClick={
        selectFirst
          ? (event) => {
              event.preventDefault()
              onToggleSelect?.(photo.uid)
            }
          : undefined
      }
      onKeyDown={
        selectFirst
          ? (event) => {
              if (event.key === ' ') {
                event.preventDefault()
                onToggleSelect?.(photo.uid)
              }
            }
          : undefined
      }
    >
      <FadeInImage
        src={photo.thumb_url}
        alt={label}
        className="w-100 h-100"
        style={{ objectFit: 'cover' }}
      />
    </Link>
  )

  const coverLabel = isCover ? t('subject.cover.current') : t('subject.cover.set')

  return (
    <div className={`kk-subject-tile position-relative${anySelected ? ' kk-tile--checks' : ''}`}>
      {media}
      {/* The checkmark is a sibling of the link (interactive content cannot
          nest), so picking a tile never navigates. Same control and styling as
          the library grid's, hidden at rest and revealed on hover/focus. */}
      {selectable && (
        <button
          type="button"
          className={`kk-tile__check${selected ? ' kk-tile__check--on' : ''}`}
          aria-pressed={selected}
          aria-label={t('selection.toggle', { name: label })}
          title={t('selection.toggle', { name: label })}
          onClick={() => {
            onToggleSelect?.(photo.uid)
          }}
        >
          {selected && <Icon name="check-lg" />}
        </button>
      )}
      {/* The cover action is a sibling of the link (interactive content cannot
          nest), and steps aside once the tile is a selection target so it stays
          a clean pick — as the library tile hides its heart there. It is
          a quiet icon-only disc revealed on hover/focus of the tile (kept always
          visible on touch, and for the current cover as its marker), rather than
          a loud labelled button on every tile. Same handler and PATCH as before —
          only the prominence and the reveal change. */}
      {canSetCover && !selectFirst && (
        <button
          type="button"
          className={`kk-cover-btn${isCover ? ' kk-cover-btn--on' : ''}`}
          aria-pressed={isCover}
          aria-label={coverLabel}
          title={coverLabel}
          disabled={busy || isCover}
          onClick={() => {
            onSetCover(photo.uid)
          }}
        >
          <Icon name={isCover ? 'image-fill' : 'image'} />
        </button>
      )}
    </div>
  )
}
