import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { Outliers } from '../components/people/Outliers'
import { SubjectEditModal } from '../components/people/SubjectEditModal'
import { SubjectPhotoTile } from '../components/people/SubjectPhotoTile'
import { useSubjectPhotos } from '../hooks/useSubjectPhotos'
import { fetchSubject, type Subject, updateSubject } from '../services/people'

/** Fetch lifecycle of the subject record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; subject: Subject }

/**
 * A subject's page: header (name, type, edit), the photo gallery (with a
 * set-cover action for editors), and — for editors — the outlier review section
 * to spot and detach mis-assigned faces. The gallery pages through
 * `GET /subjects/{uid}/photos` with a load-more control.
 */
export function SubjectPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState(false)
  const [coverBusy, setCoverBusy] = useState(false)

  const { photos, status, hasMore, loadingMore, loadMore } = useSubjectPhotos(uid)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSubject(uid, controller.signal)
      .then((subject) => {
        setState({ status: 'ready', subject })
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

  const setCover = useCallback(
    async (photoUid: string) => {
      if (state.status !== 'ready') {
        return
      }
      const subject = state.subject
      setCoverBusy(true)
      try {
        const updated = await updateSubject(subject.uid, {
          name: subject.name,
          type: subject.type,
          favorite: subject.favorite,
          private: subject.private,
          notes: subject.notes,
          cover_photo_uid: photoUid,
        })
        setState({ status: 'ready', subject: updated })
      } catch {
        // A failed cover change leaves the previous cover untouched; the tile
        // re-enables so the user can retry.
      } finally {
        setCoverBusy(false)
      }
    },
    [state],
  )

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-5">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('subject.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <Alert variant="danger">
        {t('subject.error')} <Link to="/people">{t('subject.back')}</Link>
      </Alert>
    )
  }

  const { subject } = state

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2">
          <Link to="/people" className="text-decoration-none">
            ← {t('subject.back')}
          </Link>
          <h1 className="h3 mb-0">{subject.name}</h1>
          <Badge bg="secondary">{t(`subject.type.${subject.type}`)}</Badge>
        </div>
        {canWrite && (
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={() => {
              setEditing(true)
            }}
          >
            {t('subject.editButton')}
          </Button>
        )}
      </div>

      <h2 className="h5">{t('subject.photos')}</h2>
      {status === 'loading' && (
        <div className="d-flex justify-content-center py-4">
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('subject.loadingPhotos')}</span>
          </Spinner>
        </div>
      )}
      {status === 'ready' && photos.length === 0 && (
        <p className="text-secondary">{t('subject.noPhotos')}</p>
      )}
      {photos.length > 0 && (
        <>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))',
              gap: '8px',
            }}
          >
            {photos.map((photo) => (
              <SubjectPhotoTile
                key={photo.uid}
                photo={photo}
                isCover={subject.cover_photo_uid === photo.uid}
                canSetCover={canWrite}
                busy={coverBusy}
                onSetCover={(photoUid) => {
                  void setCover(photoUid)
                }}
              />
            ))}
          </div>
          {hasMore && (
            <div className="text-center mt-3">
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={loadingMore}
                onClick={loadMore}
              >
                {loadingMore ? t('subject.loadingMore') : t('subject.loadMore')}
              </Button>
            </div>
          )}
        </>
      )}

      {canWrite && (
        <section className="mt-4">
          <h2 className="h5">{t('outliers.title')}</h2>
          <p className="text-secondary small">{t('outliers.subtitle')}</p>
          <Outliers subjectUid={subject.uid} />
        </section>
      )}

      {canWrite && (
        <SubjectEditModal
          subject={subject}
          show={editing}
          onHide={() => {
            setEditing(false)
          }}
          onSaved={(updated) => {
            setState({ status: 'ready', subject: updated })
            setEditing(false)
          }}
        />
      )}
    </>
  )
}
