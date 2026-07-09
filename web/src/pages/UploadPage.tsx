import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { DropZone } from '../components/upload/DropZone'
import { UploadItem } from '../components/upload/UploadItem'
import { useUploadQueue } from '../hooks/useUploadQueue'

/**
 * Multiupload page: drag or pick many files (gallery/camera on mobile), review
 * the queue, upload with per-file progress and a counts summary, retry failures,
 * and jump to the freshly added photos in the library. Every state and label is
 * translated (cs/en) and the controls are sized for touch.
 */
export function UploadPage() {
  const { t } = useTranslation()
  const {
    items,
    summary,
    isUploading,
    isComplete,
    createdUids,
    addFiles,
    removeItem,
    start,
    retry,
    retryFailed,
    clear,
  } = useUploadQueue()

  const hasQueued = summary.queued > 0
  const hasFailed = summary.error > 0
  const hasItems = items.length > 0

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('upload.title')}</h1>
      <p className="text-secondary">{t('upload.subtitle')}</p>

      <DropZone onFiles={addFiles} />

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
                disabled={isUploading}
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
              <Link to="/library?sort=added" className="btn btn-outline-light btn-sm">
                {t('upload.done.viewLibrary')}
              </Link>
            )}
          </div>
        </Alert>
      )}
    </>
  )
}
