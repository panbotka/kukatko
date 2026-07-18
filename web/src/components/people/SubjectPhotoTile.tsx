import Form from 'react-bootstrap/Form'
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
   * When true the tile is a selection target: clicking toggles selection (via
   * {@link SubjectPhotoTileProps.onToggleSelect}) instead of opening the detail
   * page, a checkbox overlay reflects {@link SubjectPhotoTileProps.selected}, and
   * the set-cover action steps aside so the whole tile is one target.
   */
  selectable?: boolean
  /** Whether this tile is currently selected (only meaningful when selectable). */
  selected?: boolean
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
 * action is still reachable. In selection mode the tile becomes a checkbox
 * target instead, so a batch of the person's photos can be bulk-edited.
 */
export function SubjectPhotoTile({
  photo,
  isCover,
  canSetCover,
  busy,
  onSetCover,
  selectable = false,
  selected = false,
  onToggleSelect,
  detailQuery,
}: SubjectPhotoTileProps) {
  const { t } = useTranslation()
  const label = photo.title !== '' ? photo.title : photo.file_name

  const inner = (
    <>
      <FadeInImage
        src={photo.thumb_url}
        alt={label}
        className="w-100 h-100"
        style={{ objectFit: 'cover' }}
      />
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
    </>
  )

  const media = selectable ? (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={label}
      title={label}
      onClick={() => {
        onToggleSelect?.(photo.uid)
      }}
      className="btn p-0 border-0 d-block w-100 bg-secondary-subtle overflow-hidden rounded"
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
      className="d-block bg-secondary-subtle overflow-hidden rounded"
      style={{ aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
    >
      {inner}
    </Link>
  )

  const coverLabel = isCover ? t('subject.cover.current') : t('subject.cover.set')

  return (
    <div className="kk-subject-tile position-relative">
      {media}
      {/* The cover action is a sibling of the link/button (interactive content
          cannot nest), and is hidden in selection mode so the tile stays a clean
          selection target — as the library tile hides its heart and stars. It is
          a quiet icon-only disc revealed on hover/focus of the tile (kept always
          visible on touch, and for the current cover as its marker), rather than
          a loud labelled button on every tile. Same handler and PATCH as before —
          only the prominence and the reveal change. */}
      {canSetCover && !selectable && (
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
