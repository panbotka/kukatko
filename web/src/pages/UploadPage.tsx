import { useEffect } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { DropZone } from '../components/upload/DropZone'
import { UploadItem } from '../components/upload/UploadItem'
import { UploadOrganize } from '../components/upload/UploadOrganize'
import { useUploadOrganize } from '../hooks/useUploadOrganize'
import { useUploadQueue } from '../hooks/useUploadQueue'
import { LIBRARY_PATH } from '../lib/libraryView'

/**
 * Multiupload page: drag or pick many files (gallery/camera on mobile), review
 * the queue, upload with per-file progress and a counts summary, retry failures,
 * and jump to the freshly added photos in the library. Before uploading, the
 * user may pick albums and labels for the whole batch; once every file settles,
 * every resolved photo — new *or* duplicate — is added to them in one bulk call,
 * with an "assigning…" state and a retryable error if that step alone fails.
 * Every state and label is translated (cs/en) and the controls are sized for touch.
 */
export function UploadPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const {
    items,
    summary,
    isUploading,
    isComplete,
    createdUids,
    resolvedUids,
    addFiles,
    removeItem,
    start,
    retry,
    retryFailed,
    clear,
  } = useUploadQueue()
  const {
    load: organizeLoad,
    albums,
    labels,
    setAlbums,
    setLabels,
    hasSelection,
    assign,
    runAssign,
    retryAssign,
    resetAssign,
  } = useUploadOrganize()

  // Once every file has settled, assign the whole batch to the chosen albums and
  // labels — but only when something is chosen and at least one photo resolved.
  useEffect(() => {
    if (isComplete && hasSelection && resolvedUids.length > 0 && assign.status === 'idle') {
      runAssign(resolvedUids)
    }
  }, [isComplete, hasSelection, resolvedUids, assign.status, runAssign])

  // A fresh batch (files re-queued, cleared, or a failed upload retried) clears a
  // prior assignment result so the next completion assigns again.
  useEffect(() => {
    if (!isComplete && (assign.status === 'done' || assign.status === 'error')) {
      resetAssign()
    }
  }, [isComplete, assign.status, resetAssign])

  const hasQueued = summary.queued > 0
  const hasFailed = summary.error > 0
  const hasItems = items.length > 0
  const assigning = assign.status === 'assigning'

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('upload.title')}</h1>
      <p className="text-secondary">{t('upload.subtitle')}</p>

      <DropZone onFiles={addFiles} />

      <UploadOrganize
        load={organizeLoad}
        albums={albums}
        labels={labels}
        onAlbums={setAlbums}
        onLabels={setLabels}
        disabled={assigning}
        allowCreate={canWrite}
      />

      {hasItems && (
        <>
          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2">
            <h2 className="kk-section-title mb-0">{t('upload.queue.heading')}</h2>
            <div className="d-flex flex-wrap gap-2">
              <Button type="button" variant="primary" onClick={start} disabled={!hasQueued}>
                {t('upload.actions.start', { count: summary.queued })}
              </Button>
              {hasFailed && (
                <Button type="button" variant="outline-primary" onClick={retryFailed}>
                  {t('upload.actions.retryFailed')}
                </Button>
              )}
              <Button
                type="button"
                variant="outline-secondary"
                onClick={clear}
                disabled={isUploading || assigning}
              >
                {t('upload.actions.clear')}
              </Button>
            </div>
          </div>

          <p className="text-secondary small mb-2" aria-live="polite">
            {t('upload.summary.line', {
              created: summary.created,
              duplicate: summary.duplicate,
              error: summary.error,
              pending: summary.queued + summary.uploading,
            })}
          </p>

          <ListGroup className="mb-3">
            {items.map((item) => (
              <UploadItem key={item.id} item={item} onRemove={removeItem} onRetry={retry} />
            ))}
          </ListGroup>
        </>
      )}

      {isComplete && (
        <Alert variant={hasFailed ? 'warning' : 'success'}>
          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
            <span>{t('upload.done.title')}</span>
            {createdUids.length > 0 && (
              <Link to={`${LIBRARY_PATH}?sort=added`} className="btn btn-outline-light btn-sm">
                {t('upload.done.viewLibrary')}
              </Link>
            )}
          </div>
        </Alert>
      )}

      {assigning && (
        <Alert variant="info" className="d-flex align-items-center gap-2" aria-live="polite">
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('upload.organize.assigning')}</span>
          </Spinner>
          <span>{t('upload.organize.assigning')}</span>
        </Alert>
      )}

      {assign.status === 'done' && (
        <Alert variant="success" aria-live="polite">
          {t('upload.organize.assigned')}
        </Alert>
      )}

      {assign.status === 'error' && (
        <Alert variant="danger" aria-live="polite">
          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
            <span>
              {assign.message === ''
                ? t('upload.organize.assignErrorGeneric')
                : t('upload.organize.assignError', { message: assign.message })}
            </span>
            <Button type="button" variant="outline-light" size="sm" onClick={retryAssign}>
              {t('upload.organize.retry')}
            </Button>
          </div>
        </Alert>
      )}
    </>
  )
}
