import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ProgressBar from 'react-bootstrap/ProgressBar'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type UploadSummary } from '../../hooks/useUploadQueue'
import { LIBRARY_PATH } from '../../lib/libraryView'

/** Props for {@link UploadProgressHeader}. */
export interface UploadProgressHeaderProps {
  /** Aggregate status counts across the whole batch. */
  summary: UploadSummary
  /** Overall completion fraction in `[0, 1]`, weighting in-flight partials. */
  progress: number
  /** True once every file has settled (nothing queued or uploading). */
  isComplete: boolean
  /** Whether any newly created photo exists, enabling the library link. */
  hasCreated: boolean
  /** Re-queues every failed file in the batch. */
  onRetryFailed: () => void
}

/**
 * The coloured live breakdown of outcomes: uploaded / duplicates / errors, plus
 * remaining while the batch is still running. Shown in both the running and the
 * completed header so the numbers read the same throughout.
 */
function CountsBreakdown({
  summary,
  remaining,
  showRemaining,
}: {
  summary: UploadSummary
  remaining: number
  showRemaining: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className="d-flex flex-wrap gap-2">
      <Badge bg="success">{t('upload.progress.uploaded', { count: summary.created })}</Badge>
      <Badge bg="warning" text="dark">
        {t('upload.progress.duplicate', { count: summary.duplicate })}
      </Badge>
      <Badge bg="danger">{t('upload.progress.failed', { count: summary.error })}</Badge>
      {showRemaining && (
        <Badge bg="secondary">{t('upload.progress.remaining', { count: remaining })}</Badge>
      )}
    </div>
  )
}

/**
 * The prominent, sticky overall-progress header for a bulk upload. While the
 * batch runs it shows how many files are done out of the total, a single bar
 * reflecting the whole batch (in-flight files contribute their partial fraction,
 * so it advances smoothly), and a live count breakdown — the one thing worth
 * watching on a phone as the per-file list scrolls beneath it. Once every file
 * settles it flips to a clear completed summary with the library link and a
 * one-tap retry for any failures.
 */
export function UploadProgressHeader({
  summary,
  progress,
  isComplete,
  hasCreated,
  onRetryFailed,
}: UploadProgressHeaderProps) {
  const { t } = useTranslation()

  const done = summary.created + summary.duplicate + summary.error
  const remaining = summary.queued + summary.uploading
  const percent = Math.round(progress * 100)
  const title = summary.uploading > 0 ? t('upload.progress.uploading') : t('upload.progress.ready')

  return (
    <div
      className="kk-surface kukatko-sticky-toolbar shadow-sm p-3 mb-3"
      role="status"
      aria-live="polite"
      data-testid="upload-progress-header"
    >
      {isComplete ? (
        <div className="d-flex flex-wrap align-items-center justify-content-between gap-3">
          <div>
            <div className="kk-section-title mb-1">{t('upload.done.title')}</div>
            <div className="text-secondary mb-2">
              {t('upload.progress.summary', {
                created: summary.created,
                duplicate: summary.duplicate,
                error: summary.error,
              })}
            </div>
            <CountsBreakdown summary={summary} remaining={remaining} showRemaining={false} />
          </div>
          <div className="d-flex flex-wrap gap-2">
            {summary.error > 0 && (
              <Button type="button" variant="primary" onClick={onRetryFailed}>
                {t('upload.actions.retryFailed')}
              </Button>
            )}
            {hasCreated && (
              <Link to={`${LIBRARY_PATH}?sort=added`} className="btn btn-outline-primary">
                {t('upload.done.viewLibrary')}
              </Link>
            )}
          </div>
        </div>
      ) : (
        <>
          <div className="d-flex flex-wrap align-items-baseline justify-content-between gap-2 mb-2">
            <span className="kk-section-title mb-0">{title}</span>
            <span className="fs-5 fw-semibold">
              {t('upload.progress.count', { done, total: summary.total })}
            </span>
          </div>
          <ProgressBar
            now={percent}
            animated={summary.uploading > 0}
            aria-label={t('upload.progress.barLabel')}
            className="mb-2"
          />
          <CountsBreakdown summary={summary} remaining={remaining} showRemaining />
        </>
      )}
    </div>
  )
}
