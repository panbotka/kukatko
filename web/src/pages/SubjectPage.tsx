import { useCallback, useEffect, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SelectionStart } from '../components/organize/SelectionStart'
import { Outliers } from '../components/people/Outliers'
import { SubjectEditModal } from '../components/people/SubjectEditModal'
import { SubjectPhotoTile } from '../components/people/SubjectPhotoTile'
import { Skeleton } from '../components/Skeleton'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useGridDensity } from '../hooks/useGridDensity'
import { useReloadKey } from '../hooks/useReloadKey'
import { useSubjectPhotos } from '../hooks/useSubjectPhotos'
import { gridTemplateColumns } from '../lib/gridDensity'
import { fetchSubject, type Subject, updateSubject } from '../services/people'

/** The subject gallery breathes a little more than the library grid. */
const GALLERY_GAP_PX = 8

/**
 * Where the back link leads. The people index keeps no view state of its own in
 * the URL, so the bare route restores it exactly; should it ever grow filters,
 * this is the one place that has to carry them.
 */
const PEOPLE_PATH = '/people'

/** Fetch lifecycle of the subject record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; subject: Subject }

/**
 * A subject's page: header (name, type, edit), the photo gallery (with a
 * set-cover action for editors), and — for editors — the outlier review section
 * to spot and detach mis-assigned faces. The gallery pages through
 * `GET /subjects/{uid}/photos` with a load-more control.
 *
 * Editors can also select photos in the gallery and bulk-edit them; the gallery
 * refetches afterwards, since the edit may have taken photos out of it. The
 * set-cover action lives on the tiles and is untouched outside selection mode.
 */
export function SubjectPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const { density } = useGridDensity()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState(false)
  const [coverBusy, setCoverBusy] = useState(false)

  const [reloadKey, reload] = useReloadKey()
  const { photos, status, hasMore, loadingMore, loadMore, retry } = useSubjectPhotos(uid, {
    reloadKey,
  })

  const bulk = useBulkEdit({ onEdited: reload })
  const selection = bulk.selection

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

  // Walking from one person to the next reuses this page, so a selection made in
  // the previous gallery would survive into a gallery that never showed those
  // photos. Each subject is its own list: leave selection mode with the subject.
  const leaveSelection = selection.disable
  useEffect(() => {
    leaveSelection()
  }, [uid, leaveSelection])

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
    // Hold the page's shape while the record loads: a header placeholder over the
    // gallery skeleton, so the layout does not jump when the subject arrives.
    return (
      <>
        <div className="d-flex align-items-center gap-2 mb-3" aria-hidden="true">
          <Skeleton width="10rem" height="1.75rem" radius="var(--kk-radius-sm)" />
          <Skeleton width="4rem" height="1.375rem" radius="var(--kk-radius-pill)" />
        </div>
        <h2 className="kk-section-title">{t('subject.photos')}</h2>
        <GridSkeleton label={t('subject.loadingPhotos')} />
      </>
    )
  }

  if (state.status === 'error') {
    return (
      <ErrorState
        title={t('subject.error')}
        action={<BackLink to={PEOPLE_PATH} label={t('subject.back')} />}
      />
    )
  }

  const { subject } = state

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2">
          <BackLink to={PEOPLE_PATH} label={t('subject.back')} />
          <h1 className="kk-page-title mb-0">{subject.name}</h1>
          <Badge bg="secondary">{t(`subject.type.${subject.type}`)}</Badge>
        </div>
        {!selection.active && (
          <div className="d-flex gap-1 flex-wrap">
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
            {photos.length > 0 && <SelectionStart bulk={bulk} />}
          </div>
        )}
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <BulkEditControl bulk={bulk} />
        </SelectionBar>
      )}

      <h2 className="kk-section-title">{t('subject.photos')}</h2>
      {status === 'loading' && <GridSkeleton label={t('subject.loadingPhotos')} />}
      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}
      {status === 'ready' && photos.length === 0 && <EmptyState title={t('subject.noPhotos')} />}
      {photos.length > 0 && (
        <>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: gridTemplateColumns(density, GALLERY_GAP_PX),
              gap: `${GALLERY_GAP_PX}px`,
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
                selectable={selection.active}
                selected={selection.selected.has(photo.uid)}
                onToggleSelect={selection.toggle}
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
          <h2 className="kk-section-title">{t('outliers.title')}</h2>
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
