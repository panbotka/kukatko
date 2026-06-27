import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { FaceOverlay } from '../components/people/FaceOverlay'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { fetchPhoto, type PhotoDetail } from '../services/photos'

/** Fetch lifecycle of the photo detail. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; photo: PhotoDetail }

/**
 * The photo detail page: the image with the interactive {@link FaceOverlay} for
 * naming faces, plus the similar-photos strip. It is intentionally lightweight —
 * the face overlay carries the people functionality this milestone delivers.
 */
export function PhotoDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchPhoto(uid, controller.signal)
      .then((photo) => {
        setState({ status: 'ready', photo })
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
  }, [uid])

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-5">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('photo.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <Alert variant="danger">
        {t('photo.error')} <Link to="/library">{t('photo.back')}</Link>
      </Alert>
    )
  }

  const { photo } = state
  const title = photo.title !== '' ? photo.title : photo.file_name

  return (
    <>
      <div className="d-flex align-items-center gap-2 mb-3 flex-wrap">
        <Link to="/library" className="text-decoration-none">
          ← {t('photo.back')}
        </Link>
        <h1 className="h4 mb-0">{title}</h1>
      </div>

      <div className="mb-4">
        <FaceOverlay photoUid={photo.uid} />
      </div>

      <SimilarPhotos uid={photo.uid} />
    </>
  )
}
