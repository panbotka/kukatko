import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ProgressBar from 'react-bootstrap/ProgressBar'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type UploadSummary } from '../../hooks/useUploadQueue'
import { LIBRARY_PATH } from '../../lib/libraryView'

/** The batch-wide album/label choice, echoed in the sticky header. */
export interface UploadOrganizeRecap {
  /** Human names of everything chosen, albums first (may be empty). */
  names: string[]
  /** Brings the picker back on screen and puts the caret in it. */
  onEdit: () => void
}

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
  /** The batch's albums/labels; omitted while the picker has nothing to show. */
  organize?: UploadOrganizeRecap
}

/**
 * The batch's album/label choice, restated inside the sticky bar with one tap
 * back to the picker.
 *
 * This is what keeps step 2 usable once step 3 is long: with fifty files queued
 * the picker is scrolled far above the rows the reader is looking at, and until
 * now nothing on screen said what the batch would be tagged with — or offered a
 * way back short of scrolling to the top by hand.
 *
 * It is text and a button on purpose. The picker itself must **not** move in
 * here: `.kukatko-sticky-toolbar` is a stacking context, so the `MultiSelect`'s
 * fixed suggestion overlay would be trapped in the bar's own layer (1019) and
 * sink back under the mobile tab bar — exactly the bug that was just fixed.
 */
function OrganizeRecap({ names, onEdit }: UploadOrganizeRecap) {
  const { t } = useTranslation()
  return (
    <div className="d-flex flex-wrap align-items-center gap-2 mt-2 pt-2 kk-upload-recap">
      <span className="kk-text-caption text-secondary">{t('upload.organize.recapLabel')}</span>
      {names.length === 0 ? (
        <span className="kk-text-caption text-secondary fst-italic">
          {t('upload.organize.recapEmpty')}
        </span>
      ) : (
        // Keyed by position too: an album and a label may well carry the same
        // name ("Dovolená"), and a bare name key would then collide.
        names.map((name, index) => (
          <Badge
            key={`${name}-${String(index)}`}
            bg="secondary"
            className="text-truncate kk-upload-recap__chip"
          >
            {name}
          </Badge>
        ))
      )}
      <Button
        type="button"
        size="sm"
        variant="outline-secondary"
        className="ms-auto"
        onClick={onEdit}
      >
        {t('upload.organize.recapEdit')}
      </Button>
    </div>
  )
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
 *
 * Being the one thing that stays on screen while the queue scrolls, it also
 * carries the batch's album/label choice ({@link OrganizeRecap}) — the state of
 * step 2, kept reachable from anywhere in step 3.
 */
export function UploadProgressHeader({
  summary,
  progress,
  isComplete,
  hasCreated,
  onRetryFailed,
  organize,
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

      {organize !== undefined && <OrganizeRecap names={organize.names} onEdit={organize.onEdit} />}
    </div>
  )
}
