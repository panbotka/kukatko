import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type SubjectCount } from '../../services/people'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

/** Props for {@link SubjectTile}. */
export interface SubjectTileProps {
  /** The subject with its photo (marker) count. */
  subject: SubjectCount
}

/**
 * A single subject card in the people grid: a square cover thumbnail (cropped
 * from the subject's chosen cover photo, or a neutral placeholder), the name, and
 * the count of photos the person appears in. Links to the subject's page.
 */
export function SubjectTile({ subject }: SubjectTileProps) {
  const { t } = useTranslation()
  const cover = subject.cover_photo_uid

  return (
    <Link
      to={`/people/${subject.uid}`}
      className="d-block text-decoration-none text-body"
      aria-label={subject.name}
      title={subject.name}
    >
      <div
        className="position-relative bg-secondary-subtle overflow-hidden rounded mb-1 d-flex align-items-center justify-content-center"
        style={{ aspectRatio: '1 / 1' }}
      >
        {cover !== undefined && cover !== '' ? (
          <img
            src={thumbUrl(cover, GRID_THUMB_SIZE)}
            alt={subject.name}
            loading="lazy"
            decoding="async"
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        ) : (
          <span className="text-secondary small p-2 text-center">{t('people.noCover')}</span>
        )}
        {subject.private && (
          <span
            className="position-absolute top-0 end-0 m-1 badge text-bg-dark opacity-75"
            aria-hidden="true"
          >
            {t('people.private')}
          </span>
        )}
      </div>
      <div className="fw-semibold text-truncate">{subject.name}</div>
      <div className="small text-secondary">
        {t('people.photoCount', { count: subject.marker_count })}
      </div>
    </Link>
  )
}
