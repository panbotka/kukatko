import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { subjectTileImage } from '../../lib/subjectTile'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { type SubjectCount } from '../../services/people'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'

import { FaceCrop } from './FaceCrop'

/** Props for {@link SubjectTile}. */
export interface SubjectTileProps {
  /** The subject with its photo (marker) count. */
  subject: SubjectCount
}

/**
 * A single subject card in the people grid: a square picture of the person, their
 * name, and the count of photos they appear in. Links to the subject's page.
 *
 * The picture is the point. A page about people that shows a grid of grey
 * "no preview" boxes is a page about nothing, so a subject with no chosen cover
 * falls back to a crop of their own face taken from a photo they appear on — see
 * {@link subjectTileImage} for which image wins. Only a subject with no usable
 * face at all keeps the placeholder.
 */
export function SubjectTile({ subject }: SubjectTileProps) {
  const { t } = useTranslation()
  const image = subjectTileImage(subject)

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
        {image.kind === 'cover' && (
          <FadeInImage
            src={thumbUrl(image.photoUid, GRID_THUMB_SIZE)}
            alt={subject.name}
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
          />
        )}
        {image.kind === 'face' && (
          <FaceCrop
            photoUid={image.photoUid}
            crop={image.crop}
            frame={image.frame}
            label={subject.name}
            className="w-100 h-100"
          />
        )}
        {image.kind === 'none' && (
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
