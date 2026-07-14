import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'

import { type PhotoAlbumRef, type PhotoLabelRef } from '../../services/photos'

/** Props for {@link OrganizeBadges}. */
export interface OrganizeBadgesProps {
  /** The albums the photo belongs to; rendered first. */
  albums: PhotoAlbumRef[]
  /** The labels the photo carries; rendered after the albums. */
  labels: PhotoLabelRef[]
}

/**
 * The read-only "filed under" strip of the photo detail: the photo's albums and
 * then its labels as pill badges, right under the title and above the photo, so
 * a reader sees what the photo is filed under without scrolling down to the
 * Organize card.
 *
 * Purely informative: the badges carry no remove/add controls — adding and
 * removing memberships stays exclusively in {@link OrganizePanel}. Both read the
 * very same `photo.albums`/`photo.labels` arrays, so an edit down there shows up
 * here immediately with no second fetch. Each badge links to its scoped list and
 * uses the shared `ENTITY_STYLE` colour + glyph, so it looks identical to the
 * Organize chips. Renders nothing when the photo has neither albums nor labels.
 */
export function OrganizeBadges({ albums, labels }: OrganizeBadgesProps) {
  const { t } = useTranslation()

  if (albums.length === 0 && labels.length === 0) {
    return null
  }

  // The pill is the link itself (no nested control), so the whole badge — glyph
  // included — is one tap target; `flex-wrap` lets the strip run onto more lines
  // instead of scrolling the page sideways.
  const pill = (kind: 'album' | 'tag') =>
    `badge rounded-pill ${ENTITY_STYLE[kind].className} d-inline-flex align-items-center gap-1 text-white text-decoration-none`

  return (
    <nav
      aria-label={t('photo.sections.organize')}
      data-testid="photo-badges"
      className="d-flex flex-wrap gap-2 mb-3"
    >
      {albums.map((album) => (
        <Link key={album.uid} to={`/albums/${album.uid}`} className={pill('album')}>
          <Icon name={ENTITY_STYLE.album.icon} />
          {album.title}
        </Link>
      ))}
      {labels.map((label) => (
        <Link key={label.uid} to={`/labels/${label.uid}`} className={pill('tag')}>
          <Icon name={ENTITY_STYLE.tag.icon} />
          {label.name}
        </Link>
      ))}
    </nav>
  )
}
