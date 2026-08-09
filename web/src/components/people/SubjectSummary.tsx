import { useTranslation } from 'react-i18next'

import { subjectTileImage } from '../../lib/subjectTile'
import { type SubjectCount } from '../../services/people'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { FaceCrop } from './FaceCrop'

/** How wide the picture is, in CSS pixels: a thumbnail beside a name, not a tile. */
const PICTURE_PX = 72

/** Props for {@link SubjectSummary}. */
export interface SubjectSummaryProps {
  /** The person to summarise, with the counts that make the summary worth reading. */
  subject: SubjectCount
  /** A short line under the counts naming this person's role in the action. */
  role?: string
}

/**
 * One person, small: their picture, their name and how much of the library they
 * carry. It exists for the merge confirmation, where the whole question is "are
 * these two the same person, and do I understand what I am about to lose" — a
 * question no pair of names alone can answer, and one that the *photo* counts
 * decide, since they say which record is the substantial one.
 *
 * The picture follows the same rule as the people grid ({@link subjectTileImage}):
 * a chosen cover wins, a face crop is the fallback, and a person with neither
 * keeps an honest placeholder rather than a borrowed face.
 */
export function SubjectSummary({ subject, role }: SubjectSummaryProps) {
  const { t } = useTranslation()
  const image = subjectTileImage(subject)

  return (
    <div className="d-flex align-items-center gap-3">
      <div
        className="kk-tile__media flex-shrink-0 d-flex align-items-center justify-content-center overflow-hidden"
        style={{ width: `${String(PICTURE_PX)}px`, height: `${String(PICTURE_PX)}px` }}
      >
        {image.kind === 'cover' && (
          <FadeInImage
            src={thumbUrl(image.photoUid, GRID_THUMB_SIZE)}
            alt=""
            aria-hidden
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
            skeleton
          />
        )}
        {image.kind === 'face' && (
          <FaceCrop
            photoUid={image.photoUid}
            crop={image.crop}
            frame={image.frame}
            label=""
            className="w-100 h-100"
          />
        )}
        {image.kind === 'none' && (
          <EmptyState size="sm" title={t('people.noCover')} className="kk-tile__placeholder" />
        )}
      </div>
      {/* `kk-min-w-0`: a person's name is user data and can be one long
          unbroken string, which would otherwise stretch the dialog. */}
      <div className="kk-min-w-0">
        <div className="fw-semibold text-truncate">{subject.name}</div>
        <div className="kk-text-caption text-secondary">
          {t('people.photoCount', { count: subject.photo_count })} ·{' '}
          {t('subject.merge.faceCount', { count: subject.marker_count })}
        </div>
        {role !== undefined && <div className="kk-text-caption text-secondary">{role}</div>}
      </div>
    </div>
  )
}
