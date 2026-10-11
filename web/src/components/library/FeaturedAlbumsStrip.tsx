import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { albumDisplayTitle } from '../../i18n/albumNames'
import { type FeaturedAlbum, fetchFeaturedAlbums } from '../../services/featured'
import { GRID_THUMB_SIZE, thumbUrl } from '../../services/photos'
import { BlurPlaceholder } from '../BlurPlaceholder'
import { EmptyState } from '../EmptyState'
import { FadeInImage } from '../FadeInImage'

/** DOM id of the strip's heading, which names the list for a screen reader. */
const HEADING_ID = 'featured-albums-heading'

/**
 * One card of the strip: the album's cover over its blurred placeholder, its
 * title and its photo count, linking to the album.
 */
function FeaturedCard({ album }: { album: FeaturedAlbum }) {
  const { t, i18n } = useTranslation()
  const title = albumDisplayTitle(album.title, i18n.language)
  // The server mints the cover's address (a signed edge URL when the bucket is
  // published); the app's own thumbnail route is the fallback for a payload
  // that carries a cover but no URL.
  const src =
    album.thumb_url ??
    (album.cover_uid === undefined ? undefined : thumbUrl(album.cover_uid, GRID_THUMB_SIZE))

  return (
    <li className="kk-featured-strip__item">
      <Link
        to={`/albums/${encodeURIComponent(album.uid)}`}
        className="kk-tile kk-featured-card d-flex flex-column text-decoration-none text-body"
        title={title}
      >
        <div className="kk-tile__media kk-featured-card__media mb-1 d-flex align-items-center justify-content-center">
          {src === undefined ? (
            <EmptyState size="sm" title={t('albums.noPhotos')} className="kk-tile__placeholder" />
          ) : (
            <>
              <BlurPlaceholder hash={album.cover_blurhash} />
              <FadeInImage
                src={src}
                alt=""
                className="w-100 h-100"
                style={{ objectFit: 'cover' }}
              />
            </>
          )}
        </div>
        <span className="fw-semibold kk-prose-clamp text-break">{title}</span>
        <span className="kk-text-caption text-secondary">
          {t('albums.photoCount', { count: album.photo_count })}
        </span>
      </Link>
    </li>
  )
}

/**
 * The homepage's "Doporučujeme" strip: the albums a curator featured — a fresh
 * event, an album the family is filling together — as one horizontally
 * scrollable row of cards above the timeline, newest feature first.
 *
 * It renders nothing at all while loading, on a failure and when nothing is
 * featured: it is a promotion, and an empty or broken promotion is noise on the
 * app's front door. Expiry is the server's (a lapsed feature is simply not
 * listed), and so is privacy (a private album is listed only for a curator).
 *
 * On a phone the row scrolls sideways inside its own box — the page itself
 * never gains a horizontal overflow.
 */
export function FeaturedAlbumsStrip() {
  const { t } = useTranslation()
  const [albums, setAlbums] = useState<FeaturedAlbum[]>([])

  useEffect(() => {
    const controller = new AbortController()
    fetchFeaturedAlbums(controller.signal)
      .then((body) => {
        setAlbums(body.albums)
      })
      .catch(() => {
        // Aborted or failed: the strip stays hidden either way.
      })
    return () => {
      controller.abort()
    }
  }, [])

  if (albums.length === 0) {
    return null
  }

  return (
    <section className="kk-featured-strip mb-3" aria-labelledby={HEADING_ID}>
      <h2 id={HEADING_ID} className="kk-section-title mb-2">
        {t('featured.heading')}
      </h2>
      <ul className="kk-featured-strip__list list-unstyled mb-0">
        {albums.map((album) => (
          <FeaturedCard key={album.uid} album={album} />
        ))}
      </ul>
    </section>
  )
}
