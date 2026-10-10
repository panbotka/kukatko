import { useEffect, useMemo, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type UploadQueueItem, type UploadSummary } from '../../hooks/useUploadQueue'

import { UploadList } from './UploadList'

/** Props for {@link UploadQueuePanel}. */
export interface UploadQueuePanelProps {
  /** Every file in the batch, in queue order. */
  items: UploadQueueItem[]
  /** Aggregate counts, for the badges and the errors-only filter. */
  summary: UploadSummary
  /** Removes a file from the queue; omitted, no row offers a Remove. */
  onRemove?: (id: string) => void
  /** Re-queues a single failed file. */
  onRetry: (id: string) => void
  /** The disclosure's label; the file count ("Soubory (12)") by default. */
  toggleLabel?: string
  /**
   * Opens the panel as it turns true; by default whenever some file failed. The
   * public upload-link page narrows it to failures a person must look at, so a
   * dropped connection that resumes by itself does not unfold a wall of rows.
   */
  autoOpen?: boolean
}

/**
 * The per-file list, demoted to a disclosure.
 *
 * A batch of fifty files is fifty rows nobody reads, and letting them push the
 * things that matter — the progress and the album picker — off a phone screen is
 * what made the old page unusable. So the list is closed by default and the
 * outcome counts stand in for it: badges saying how many landed, how many were
 * already in the library and how many failed — each only once it is above zero,
 * so a batch that is still starting shows none and a clean one never shows red.
 *
 * It opens itself the moment a file fails, because that is the one time the rows
 * are worth reading: a failed row is where the reason and the per-file **Retry**
 * are, and the errors-only filter beside the toggle finds them in a long batch.
 * Removing a queued file and retrying one are unchanged — this panel only moves
 * them out of the way of everything else.
 */
export function UploadQueuePanel({
  items,
  summary,
  onRemove,
  onRetry,
  toggleLabel,
  autoOpen,
}: UploadQueuePanelProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [showErrorsOnly, setShowErrorsOnly] = useState(false)
  const hasFailed = summary.error > 0
  const openItself = autoOpen ?? hasFailed

  // A failure is the reason to look at the rows, so surface them unasked. The
  // reader can still close the panel again; this only fires as failures appear.
  useEffect(() => {
    if (openItself) {
      setOpen(true)
    }
  }, [openItself])

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
    <section
      className="kk-upload-queue"
      aria-label={t('upload.queue.heading', { total: items.length })}
    >
      <div className="d-flex flex-wrap align-items-center gap-2">
        <Button
          type="button"
          variant="outline-secondary"
          aria-expanded={open}
          aria-controls="upload-queue-list"
          onClick={() => {
            setOpen((value) => !value)
          }}
        >
          {toggleLabel ?? t('upload.queue.heading', { total: items.length })}
        </Button>

        {/* Only the outcomes that happened: a red "0 failed" reads as an error
            at a glance, so a zero count has no badge at all. */}
        {(summary.created > 0 || summary.duplicate > 0 || summary.error > 0) && (
          <div className="d-flex flex-wrap gap-2 ms-auto" data-testid="upload-queue-counts">
            {summary.created > 0 && (
              <Badge bg="success">
                {t('upload.progress.uploaded', { count: summary.created })}
              </Badge>
            )}
            {summary.duplicate > 0 && (
              <Badge bg="warning" text="dark">
                {t('upload.progress.duplicate', { count: summary.duplicate })}
              </Badge>
            )}
            {hasFailed && (
              <Badge bg="danger">{t('upload.progress.failed', { count: summary.error })}</Badge>
            )}
          </div>
        )}
      </div>

      {open && (
        <div id="upload-queue-list" className="mt-3">
          {hasFailed && (
            <Button
              type="button"
              size="sm"
              variant={showErrorsOnly ? 'danger' : 'outline-danger'}
              className="mb-2"
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
          <UploadList items={visibleItems} onRemove={onRemove} onRetry={onRetry} />
        </div>
      )}
    </section>
  )
}
