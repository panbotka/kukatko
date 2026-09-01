import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatLifeSpan } from '../../lib/lifeYears'
import { subjectTileImage } from '../../lib/subjectTile'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

import { type SubjectCount, subjectAvatarUrl } from '../../services/people'

/** Props for {@link SubjectTile}. */
export interface SubjectTileProps {
  /** The subject with its counts. */
  subject: SubjectCount
}

/**
 * A single subject card in the people grid: a square picture of the person, their
 * name, and the count of photos they appear in. Links to the subject's page.
 *
 * The picture is the point. A page about people that shows a grid of grey
 * "no preview" boxes is a page about nothing, so a subject with no chosen cover
 * falls back to their own face taken from a photo they appear on. Only a subject
 * with no usable face at all keeps the placeholder — {@link subjectTileImage}
 * owns that rule, and is asked here so a tile with nothing to show never fires a
 * request that can only 404.
 *
 * What it loads is the backend's own avatar rendition: a ~320 px square cut
 * server-side, of some 15 kB. The tile used to crop the face in CSS out of a
 * whole-frame preview instead, which for a face spanning a few per cent of its
 * photo meant fetching megapixels to paint a 150 px square — measured on the real
 * library, 125 Mpx of image for one screen of tiles.
 *
 * Under the name the caption carries the photo count and, for whoever has one,
 * their life span ("1923–1998") — the one fact that tells two people of the same
 * name apart at a glance, and the reason a family archive keeps years at all. It
 * stays on the count's own line, so a grid of people with years and one without
 * still lines up.
 *
 * The count is `photo_count`, not `marker_count`: the caption says "photos", and
 * the tile links straight to the subject's gallery, which lists each photo once
 * however many of the person's faces it holds. The face tools next door show
 * `marker_count` instead, which is the right figure for them.
 */
export function SubjectTile({ subject }: SubjectTileProps) {
  const { t } = useTranslation()
  const image = subjectTileImage(subject)
  const lifeSpan = formatLifeSpan(subject.birth_year, subject.death_year)
  // A rendition that will not arrive (the cover moved, the source is gone) must
  // leave an honest placeholder rather than the browser's broken-image glyph.
  const [failed, setFailed] = useState(false)
  const hasPicture = image.kind !== 'none' && !failed

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
        {hasPicture && (
          <FadeInImage
            src={subjectAvatarUrl(subject.uid)}
            alt={subject.name}
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
      <div className="kk-text-caption text-secondary text-truncate">
        {t('people.photoCount', { count: subject.photo_count })}
        {lifeSpan !== null && ` · ${lifeSpan}`}
      </div>
    </Link>
  )
}
