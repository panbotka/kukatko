import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../EmptyState'

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
      className="kk-tile d-block text-decoration-none text-body"
      aria-label={subject.name}
      title={subject.name}
    >
      <div
        className="kk-tile__media mb-1 d-flex align-items-center justify-content-center"
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
          <EmptyState size="sm" title={t('people.noCover')} className="kk-tile__placeholder" />
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
      <div className="kk-text-caption text-secondary">
        {t('people.photoCount', { count: subject.marker_count })}
      </div>
    </Link>
  )
}
