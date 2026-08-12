import { useTranslation } from 'react-i18next'

import { EntityChip } from '../EntityChip'

import { albumDisplayTitle } from '../../i18n/albumNames'
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
 * here immediately with no second fetch. Both also render the very same
 * {@link EntityChip}, so the strip and the Organize chips cannot drift apart in
 * colour, glyph or tap target. Renders nothing when the photo has neither albums
 * nor labels.
 */
export function OrganizeBadges({ albums, labels }: OrganizeBadgesProps) {
  const { t, i18n } = useTranslation()

  if (albums.length === 0 && labels.length === 0) {
    return null
  }

  // `flex-wrap` lets the strip run onto more lines instead of scrolling the page
  // sideways — on a phone the chips are finger-sized, so a full row is few.
  return (
    <nav
      aria-label={t('photo.sections.organize')}
      data-testid="photo-badges"
      className="d-flex flex-wrap gap-2 mb-3"
    >
      {albums.map((album) => (
        <EntityChip key={album.uid} kind="album" to={`/albums/${album.uid}`}>
          {albumDisplayTitle(album.title, i18n.language)}
        </EntityChip>
      ))}
      {labels.map((label) => (
        <EntityChip key={label.uid} kind="tag" to={`/labels/${label.uid}`}>
          {label.name}
        </EntityChip>
      ))}
    </nav>
  )
}
