import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { subjectTileImage } from '../../lib/subjectTile'
import { type SubjectCount, subjectAvatarUrl } from '../../services/people'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

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
 * a chosen cover wins, the person's own face is the fallback, and a person with
 * neither keeps an honest placeholder rather than a borrowed face. It is the
 * backend's avatar rendition, the same small square the grid draws, so opening
 * the dialog costs a couple of thumbnails rather than two full previews.
 */
export function SubjectSummary({ subject, role }: SubjectSummaryProps) {
  const { t } = useTranslation()
  const image = subjectTileImage(subject)
  // As in the grid: a rendition that will not arrive leaves the placeholder.
  const [failed, setFailed] = useState(false)
  const hasPicture = image.kind !== 'none' && !failed

  return (
    <div className="d-flex align-items-center gap-3">
      <div
        className="kk-tile__media flex-shrink-0 d-flex align-items-center justify-content-center overflow-hidden"
        style={{ width: `${String(PICTURE_PX)}px`, height: `${String(PICTURE_PX)}px` }}
      >
        {hasPicture && (
          <FadeInImage
            src={subjectAvatarUrl(subject.uid)}
            alt=""
            aria-hidden
            className="w-100 h-100"
            style={{ objectFit: 'cover' }}
            skeleton
            onError={() => {
              setFailed(true)
            }}
          />
        )}
        {!hasPicture && (
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
