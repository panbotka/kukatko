import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import Tab from 'react-bootstrap/Tab'
import Tabs from 'react-bootstrap/Tabs'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { FavoriteButton } from '../components/library/FavoriteButton'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { EditPanel } from '../components/photo/EditPanel'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PhotoLocation } from '../components/photo/PhotoLocation'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from '../lib/detailView'
import { editPreviewStyle, isIdentityEdit } from '../lib/photoEdit'
import { readUrlState } from '../lib/urlState'
import {
  downloadUrl,
  fetchEdit,
  fetchPhoto,
  type PhotoDetail,
  type PhotoEdit,
  thumbUrl,
} from '../services/photos'

/** Fetch lifecycle of the photo detail (the photo and its stored edit). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; photo: PhotoDetail; edit: PhotoEdit }

/**
 * The rich photo detail page: a large preview that reflects the saved
 * non-destructive edit and supports prev/next navigation respecting the
 * originating list order, plus panels for metadata (view/edit), location (GPS
 * mini-map + reverse geocode), albums & labels, the edit tools, the face overlay
 * and a similar-photos strip. Every mutation is role-gated; viewers see a
 * read-only page. The whole view is deep-linkable and Back returns to the prior
 * list view (the order/scope is carried in the URL query).
 */
export function PhotoDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken } = useAuth()
  const [searchParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: 'loading' })

  const view = useMemo(() => readUrlState(searchParams, DETAIL_DEFAULTS), [searchParams])
  const neighborParams = useMemo(() => detailToParams(view), [view])
  const detailQuery = detailQueryString(view)
  const neighbors = usePhotoNeighbors(uid, neighborParams)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    Promise.all([fetchPhoto(uid, controller.signal), fetchEdit(uid, controller.signal)])
      .then(([photo, edit]) => {
        setState({ status: 'ready', photo, edit })
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
        {t('photo.error')} <Link to={backHref(view)}>{t('photo.back')}</Link>
      </Alert>
    )
  }

  const { photo, edit } = state
  const title = photo.title !== '' ? photo.title : photo.file_name

  const setPhoto = (updated: PhotoDetail) => {
    setState({ status: 'ready', photo: updated, edit })
  }
  const setEdit = (updated: PhotoEdit) => {
    setState({ status: 'ready', photo, edit: updated })
  }

  const neighborTo = (neighbor: string) =>
    detailQuery === '' ? `/photos/${neighbor}` : `/photos/${neighbor}?${detailQuery}`

  const poster = thumbUrl(photo.uid, 'fit_1920', downloadToken)

  // Render the main media by kind: a range-streaming player for videos, a
  // hover/hold motion preview for live photos, and the edit-reflecting still for
  // images. Non-destructive edits apply to images only (the backend never
  // re-renders video edits), so the video/live branches do not carry edit CSS.
  const renderMedia = () => {
    if (photo.media_type === 'video') {
      return (
        <VideoPlayer
          uid={photo.uid}
          title={title}
          poster={poster}
          downloadHref={downloadUrl(photo.uid, { original: true, token: downloadToken })}
          token={downloadToken}
        />
      )
    }
    if (photo.media_type === 'live') {
      return <LivePhoto uid={photo.uid} title={title} poster={poster} token={downloadToken} />
    }
    return (
      <img
        src={poster}
        alt={title}
        className="mw-100"
        style={{ maxHeight: '70vh', objectFit: 'contain', ...editPreviewStyle(edit) }}
      />
    )
  }

  return (
    <>
      <div className="d-flex align-items-center gap-2 mb-3 flex-wrap">
        <Link to={backHref(view)} className="text-decoration-none">
          ← {t('photo.back')}
        </Link>
        <h1 className="h4 mb-0 text-truncate">{title}</h1>
        <FavoriteButton uid={photo.uid} favorite={photo.is_favorite ?? false} className="ms-auto" />
      </div>

      <Row className="g-3">
        <Col lg={7}>
          <div className="position-relative bg-dark rounded overflow-hidden d-flex justify-content-center">
            {renderMedia()}
            {neighbors.prev !== null && (
              <Link
                to={neighborTo(neighbors.prev)}
                replace
                aria-label={t('photo.prev')}
                className="btn btn-dark opacity-75 position-absolute top-50 start-0 translate-middle-y ms-2"
              >
                ‹
              </Link>
            )}
            {neighbors.next !== null && (
              <Link
                to={neighborTo(neighbors.next)}
                replace
                aria-label={t('photo.next')}
                className="btn btn-dark opacity-75 position-absolute top-50 end-0 translate-middle-y me-2"
              >
                ›
              </Link>
            )}
          </div>

          <div className="d-flex gap-2 mt-2 flex-wrap">
            <Button
              as="a"
              href={downloadUrl(photo.uid, { original: true, token: downloadToken })}
              variant="outline-secondary"
              size="sm"
              download
            >
              {t('photo.download')}
            </Button>
            {!isIdentityEdit(edit) && (
              <Button
                as="a"
                href={downloadUrl(photo.uid, { token: downloadToken })}
                variant="outline-secondary"
                size="sm"
                download
              >
                {t('photo.downloadEdited')}
              </Button>
            )}
          </div>

          <section className="mt-3" aria-label={t('faces.title')}>
            <h2 className="h6 mb-2">{t('faces.title')}</h2>
            <FaceOverlay photoUid={photo.uid} readOnly={!canWrite} />
          </section>
        </Col>

        <Col lg={5}>
          <Tabs defaultActiveKey="info" className="mb-3">
            <Tab eventKey="info" title={t('photo.tabs.info')}>
              <MetadataPanel photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
              <hr />
              <OrganizePanel photo={photo} canWrite={canWrite} onChanged={setPhoto} />
            </Tab>
            <Tab eventKey="location" title={t('photo.tabs.location')}>
              <PhotoLocation photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
            </Tab>
            {canWrite && (
              <Tab eventKey="edit" title={t('photo.tabs.edit')}>
                <EditPanel uid={photo.uid} edit={edit} onSaved={setEdit} />
              </Tab>
            )}
          </Tabs>
        </Col>
      </Row>

      <div className="mt-4">
        <SimilarPhotos uid={photo.uid} />
      </div>
    </>
  )
}
