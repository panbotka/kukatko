import { useEffect, useState } from 'react'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { fetchSimilar, type SimilarPhoto } from '../../services/photos'

/** Props for {@link SimilarPhotos}. */
export interface SimilarPhotosProps {
  /** The source photo whose visual neighbours are shown. */
  uid: string
  /** Optional cap on how many neighbours to request (backend default 24). */
  limit?: number
}

/** Fetch lifecycle of the similar strip. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; photos: SimilarPhoto[] }

/**
 * A horizontally scrollable strip of the photos most visually similar to `uid`,
 * each thumbnail linking to its detail route. Self-contained and reusable (the
 * detail page mounts it): it fetches `/photos/{uid}/similar` on mount and
 * whenever `uid` changes, aborting any in-flight request. When the source photo
 * has no embedding yet the endpoint returns an empty list, so the component
 * simply renders nothing — it never blocks the surrounding page.
 */
export function SimilarPhotos({ uid, limit }: SimilarPhotosProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSimilar(uid, limit, controller.signal)
      .then((photos) => {
        setState({ status: 'ready', photos })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid, limit])

  if (state.status === 'loading') {
    return (
      <section aria-label={t('similar.title')}>
        <h2 className="kk-section-title mb-2">{t('similar.title')}</h2>
        <div className="d-flex justify-content-center py-3">
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('similar.loading')}</span>
          </Spinner>
        </div>
      </section>
    )
  }

  if (state.status === 'error') {
    return (
      <section aria-label={t('similar.title')}>
        <h2 className="kk-section-title mb-2">{t('similar.title')}</h2>
        <p className="text-secondary small mb-0">{t('similar.error')}</p>
      </section>
    )
  }

  // Empty result (e.g. the source photo is not embedded yet): render nothing so
  // the strip never takes up space with no content.
  if (state.photos.length === 0) {
    return null
  }

  return (
    <section aria-label={t('similar.title')}>
      <h2 className="kk-section-title mb-2">{t('similar.title')}</h2>
      <div className="d-flex gap-2 overflow-auto pb-2" style={{ scrollSnapType: 'x proximity' }}>
        {state.photos.map((photo) => {
          const label = photo.title !== '' ? photo.title : photo.file_name
          return (
            <Link
              key={photo.uid}
              to={`/photos/${photo.uid}`}
              className="d-block position-relative bg-secondary-subtle overflow-hidden rounded flex-shrink-0"
              style={{ width: '120px', height: '120px', scrollSnapAlign: 'start' }}
              aria-label={label}
              title={label}
            >
              <img
                src={photo.thumb_url}
                alt={label}
                loading="lazy"
                decoding="async"
                className="w-100 h-100"
                style={{ objectFit: 'cover' }}
              />
            </Link>
          )
        })}
      </div>
    </section>
  )
}
