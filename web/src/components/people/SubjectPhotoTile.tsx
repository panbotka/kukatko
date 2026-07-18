import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { FadeInImage } from '../FadeInImage'

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
}

/**
 * A square photo tile for a subject's gallery: links to the photo detail and,
 * for editors, overlays a "set as cover" action (marked when this photo is
 * already the cover). Touch-friendly — the action is an always-visible button on
 * small screens rather than a hover-only affordance. In selection mode the tile
 * becomes a checkbox target instead, so a batch of the person's photos can be
 * bulk-edited.
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
      to={`/photos/${photo.uid}`}
      className="d-block bg-secondary-subtle overflow-hidden rounded"
      style={{ aspectRatio: '1 / 1' }}
      aria-label={label}
      title={label}
    >
      {inner}
    </Link>
  )

  return (
    <div className="position-relative">
      {media}
      {/* The cover action is a sibling of the link/button (interactive content
          cannot nest), and is hidden in selection mode so the tile stays a clean
          selection target — as the library tile hides its heart and stars. */}
      {canSetCover && !selectable && (
        <Button
          variant={isCover ? 'success' : 'dark'}
          size="sm"
          className="position-absolute bottom-0 start-0 m-1 opacity-75"
          disabled={busy || isCover}
          onClick={() => {
            onSetCover(photo.uid)
          }}
        >
          {isCover ? t('subject.cover.current') : t('subject.cover.set')}
        </Button>
      )}
    </div>
  )
}
