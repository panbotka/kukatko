import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useGlobalSearch } from '../../hooks/useGlobalSearch'
import { thumbUrl } from '../../services/photos'
import { hasEntityMatches } from '../../services/search'

/** Thumbnail size used for the small album/person avatars in the sections. */
const SECTION_THUMB_SIZE = 'tile_100'

/** A small square thumbnail (or a neutral placeholder) for an entity chip. */
function ChipThumb({ uid, circle }: { uid?: string; circle?: boolean }) {
  const shape = circle ? 'rounded-circle' : 'rounded'
  if (uid === undefined || uid === '') {
    return (
      <span
        aria-hidden="true"
        className={`flex-shrink-0 bg-secondary-subtle ${shape}`}
        style={{ width: 28, height: 28 }}
      />
    )
  }
  return (
    <img
      src={thumbUrl(uid, SECTION_THUMB_SIZE)}
      alt=""
      loading="lazy"
      className={`flex-shrink-0 object-fit-cover ${shape}`}
      style={{ width: 28, height: 28 }}
    />
  )
}

/**
 * Compact cross-entity sections for the search page: given the current query,
 * it renders chips linking to matching Albums, Labels and People above the photo
 * results grid, so a text search also surfaces non-photo entities. It runs its
 * own grouped global search (independent of the photo full-text/semantic search
 * below) and renders nothing until that returns at least one album/label/person —
 * so an empty query, a still-loading search or a photos-only match adds no chrome.
 */
export function GlobalSearchSections({ query }: { query: string }) {
  const { t } = useTranslation()
  const { status, result } = useGlobalSearch(query)

  if (status !== 'ready' || result === null || !hasEntityMatches(result)) {
    return null
  }

  return (
    <section aria-label={t('globalSearch.sectionsLabel')} className="mb-4">
      {result.albums.length > 0 && (
        <div className="mb-3">
          <h2 className="h6 text-secondary mb-2">{t('globalSearch.groups.albums')}</h2>
          <div className="d-flex flex-wrap gap-2">
            {result.albums.map((album) => (
              <Link
                key={album.uid}
                to={`/albums/${album.uid}`}
                className="d-inline-flex align-items-center gap-2 text-decoration-none border rounded-pill ps-1 pe-3 py-1"
              >
                <ChipThumb uid={album.cover} />
                <span className="text-truncate" style={{ maxWidth: '12rem' }}>
                  {album.title || t('globalSearch.untitled')}
                </span>
                <Badge bg="secondary" pill>
                  {album.photo_count}
                </Badge>
              </Link>
            ))}
          </div>
        </div>
      )}

      {result.people.length > 0 && (
        <div className="mb-3">
          <h2 className="h6 text-secondary mb-2">{t('globalSearch.groups.people')}</h2>
          <div className="d-flex flex-wrap gap-2">
            {result.people.map((person) => (
              <Link
                key={person.uid}
                to={`/people/${person.uid}`}
                className="d-inline-flex align-items-center gap-2 text-decoration-none border rounded-pill ps-1 pe-3 py-1"
              >
                <ChipThumb uid={person.cover} circle />
                <span className="text-truncate" style={{ maxWidth: '12rem' }}>
                  {person.name}
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}

      {result.labels.length > 0 && (
        <div className="mb-3">
          <h2 className="h6 text-secondary mb-2">{t('globalSearch.groups.labels')}</h2>
          <div className="d-flex flex-wrap gap-2">
            {result.labels.map((label) => (
              <Link key={label.uid} to={`/labels/${label.uid}`} className="text-decoration-none">
                <Badge bg="primary" className="fw-normal">
                  {label.name}
                  <span className="ms-2 opacity-75">{label.photo_count}</span>
                </Badge>
              </Link>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}
