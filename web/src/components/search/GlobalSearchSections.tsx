import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useGlobalSearch } from '../../hooks/useGlobalSearch'
import { thumbUrl } from '../../services/photos'
import {
  DIRECT_KIND_LABEL,
  DIRECT_TARGET_ICON,
  directHitSecondary,
  directHitTitle,
} from '../../lib/directHit'
import { directHitRoute, type GlobalSearchDirect, hasEntityMatches } from '../../services/search'
import { ENTITY_STYLE } from '../entityStyle'
import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'

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
    <FadeInImage
      src={thumbUrl(uid, SECTION_THUMB_SIZE)}
      alt=""
      className={`flex-shrink-0 object-fit-cover ${shape}`}
      style={{ width: 28, height: 28 }}
    />
  )
}

/**
 * The banner for a pasted UID: a prominent "go to this" card when the id
 * resolved, and a plain statement when it did not. It sits above everything
 * else and looks nothing like the fuzzy chips below — an id is an exact
 * reference, and a photo grid that stays empty underneath is the expected
 * outcome, not a failed search.
 */
function DirectHitBanner({ direct }: { direct: GlobalSearchDirect }) {
  const { t } = useTranslation()
  const route = directHitRoute(direct)

  if (route === null || direct.target_kind === undefined) {
    return (
      <div className="alert alert-warning d-flex align-items-center gap-2 mb-4" role="status">
        <Icon name="exclamation-triangle" />
        <span>
          {t('globalSearch.direct.notFound', {
            kind: t(DIRECT_KIND_LABEL[direct.kind]),
            uid: direct.uid,
          })}
        </span>
      </div>
    )
  }

  const cover = direct.target_kind === 'photo' ? direct.target_uid : direct.cover
  return (
    <section aria-label={t('globalSearch.direct.heading')} className="mb-4">
      <h2 className="kk-section-title text-secondary mb-2">{t('globalSearch.direct.heading')}</h2>
      <Link
        to={route}
        className="d-inline-flex align-items-center gap-3 text-decoration-none text-body border rounded p-2"
      >
        <ChipThumb uid={cover} circle={direct.target_kind === 'person'} />
        <span className="d-flex flex-column">
          <span className="fw-semibold">{directHitTitle(direct)}</span>
          <span className="small text-secondary">{directHitSecondary(direct, t)}</span>
        </span>
        <Icon name={DIRECT_TARGET_ICON[direct.target_kind]} />
      </Link>
    </section>
  )
}

/**
 * Compact cross-entity sections for the search page: given the current query,
 * it renders chips linking to matching Albums, Labels and People above the photo
 * results grid, so a text search also surfaces non-photo entities. It runs its
 * own grouped global search (independent of the photo full-text/semantic search
 * below) and renders nothing until that returns at least one album/label/person —
 * so an empty query, a still-loading search or a photos-only match adds no chrome.
 *
 * A query that is a UID is the one exception: it renders the direct hit banner
 * instead, because that is the whole answer to pasting an id.
 */
export function GlobalSearchSections({ query }: { query: string }) {
  const { t } = useTranslation()
  const { status, result } = useGlobalSearch(query)

  if (status !== 'ready' || result === null) {
    return null
  }
  if (result.direct !== undefined) {
    return <DirectHitBanner direct={result.direct} />
  }
  if (!hasEntityMatches(result)) {
    return null
  }

  return (
    <section aria-label={t('globalSearch.sectionsLabel')} className="mb-4">
      {result.albums.length > 0 && (
        <div className="mb-3">
          <h2 className="kk-section-title text-secondary mb-2">
            {t('globalSearch.groups.albums')}
          </h2>
          <div className="d-flex flex-wrap gap-2">
            {result.albums.map((album) => (
              <Link
                key={album.uid}
                to={`/albums/${album.uid}`}
                className={`d-inline-flex align-items-center gap-2 text-decoration-none text-white rounded-pill ${ENTITY_STYLE.album.className} ps-1 pe-3 py-1`}
              >
                <ChipThumb uid={album.cover} />
                <span className="text-truncate" style={{ maxWidth: '12rem' }}>
                  {album.title || t('globalSearch.untitled')}
                </span>
                <Badge bg="light" text="dark" pill>
                  {album.photo_count}
                </Badge>
              </Link>
            ))}
          </div>
        </div>
      )}

      {result.people.length > 0 && (
        <div className="mb-3">
          <h2 className="kk-section-title text-secondary mb-2">
            {t('globalSearch.groups.people')}
          </h2>
          <div className="d-flex flex-wrap gap-2">
            {result.people.map((person) => (
              <Link
                key={person.uid}
                to={`/people/${person.uid}`}
                className={`d-inline-flex align-items-center gap-2 text-decoration-none text-white rounded-pill ${ENTITY_STYLE.person.className} ps-1 pe-3 py-1`}
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
          <h2 className="kk-section-title text-secondary mb-2">
            {t('globalSearch.groups.labels')}
          </h2>
          <div className="d-flex flex-wrap gap-2">
            {result.labels.map((label) => (
              <Link key={label.uid} to={`/labels/${label.uid}`} className="text-decoration-none">
                <span
                  className={`badge rounded-pill ${ENTITY_STYLE.tag.className} fw-normal d-inline-flex align-items-center gap-1`}
                >
                  <Icon name={ENTITY_STYLE.tag.icon} />
                  {label.name}
                  <span className="ms-1 opacity-75">{label.photo_count}</span>
                </span>
              </Link>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}
