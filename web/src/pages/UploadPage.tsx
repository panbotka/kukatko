import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { DropZone } from '../components/upload/DropZone'
import { UploadList } from '../components/upload/UploadList'
import { UploadOrganize } from '../components/upload/UploadOrganize'
import { UploadProgressHeader } from '../components/upload/UploadProgressHeader'
import { useUploadOrganize } from '../hooks/useUploadOrganize'
import { useUploadQueue } from '../hooks/useUploadQueue'

/**
 * Multiupload page: drag or pick many files (gallery/camera on mobile), review
 * the queue, upload with a prominent sticky overall-progress header and a
 * virtualized per-file list, retry failures (whole-batch or per file, and an
 * errors-only filter to find them in a big batch), and jump to the freshly added
 * photos in the library. Before uploading, the user may pick albums and labels
 * for the whole batch; once every file settles, every resolved photo — new *or*
 * duplicate — is added to them in one bulk call, with an "assigning…" state and
 * a retryable error if that step alone fails. Every state and label is
 * translated (cs/en) and the controls are sized for touch.
 */
export function UploadPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const {
    items,
    summary,
    progress,
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

  // Errors-only filter, so a handful of failures in a large batch are easy to
  // find. It only makes sense while failures exist, so reset it once they are
  // all retried away (or the queue is cleared).
  const [showErrorsOnly, setShowErrorsOnly] = useState(false)
  useEffect(() => {
    if (!hasFailed) {
      setShowErrorsOnly(false)
    }
  }, [hasFailed])

  const visibleItems = useMemo(
    () => (showErrorsOnly ? items.filter((item) => item.status === 'error') : items),
    [items, showErrorsOnly],
  )

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
          <UploadProgressHeader
            summary={summary}
            progress={progress}
            isComplete={isComplete}
            hasCreated={createdUids.length > 0}
            onRetryFailed={retryFailed}
          />

          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2">
            <h2 className="kk-section-title mb-0">{t('upload.queue.heading')}</h2>
            <div className="d-flex flex-wrap gap-2">
              {hasQueued && (
                <Button type="button" variant="primary" onClick={start}>
                  {t('upload.actions.start', { count: summary.queued })}
                </Button>
              )}
              {hasFailed && (
                <Button
                  type="button"
                  variant={showErrorsOnly ? 'danger' : 'outline-danger'}
                  aria-pressed={showErrorsOnly}
                  onClick={() => {
                    setShowErrorsOnly((value) => !value)
                  }}
                >
                  {showErrorsOnly
                    ? t('upload.filter.showAll')
                    : t('upload.filter.showErrors', { count: summary.error })}
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

          <UploadList items={visibleItems} onRemove={removeItem} onRetry={retry} />
        </>
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
