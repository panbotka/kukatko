import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

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
}

/**
 * A square photo tile for a subject's gallery: links to the photo detail and,
 * for editors, overlays a "set as cover" action (marked when this photo is
 * already the cover). Touch-friendly — the action is an always-visible button on
 * small screens rather than a hover-only affordance.
 */
export function SubjectPhotoTile({
  photo,
  isCover,
  canSetCover,
  busy,
  onSetCover,
}: SubjectPhotoTileProps) {
  const { t } = useTranslation()
  const label = photo.title !== '' ? photo.title : photo.file_name

  return (
    <div className="position-relative">
      <Link
        to={`/photos/${photo.uid}`}
        className="d-block bg-secondary-subtle overflow-hidden rounded"
        style={{ aspectRatio: '1 / 1' }}
        aria-label={label}
        title={label}
      >
        <img
          src={photo.thumb_url}
          alt={label}
          loading="lazy"
          decoding="async"
          className="w-100 h-100"
          style={{ objectFit: 'cover' }}
        />
      </Link>
      {canSetCover && (
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
