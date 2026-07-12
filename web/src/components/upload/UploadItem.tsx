import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ProgressBar from 'react-bootstrap/ProgressBar'
import { useTranslation } from 'react-i18next'

import { type QueueItemStatus, type UploadQueueItem } from '../../hooks/useUploadQueue'
import { type UploadWarning } from '../../services/upload'

/** Props for {@link UploadItem}. */
export interface UploadItemProps {
  item: UploadQueueItem
  onRemove: (id: string) => void
  onRetry: (id: string) => void
}

/** Bootstrap badge variant per status (Superhero palette). */
const BADGE_VARIANT: Record<QueueItemStatus, string> = {
  queued: 'secondary',
  uploading: 'info',
  created: 'success',
  duplicate: 'warning',
  error: 'danger',
}

/** Literal i18n keys for status labels, so the typed `t()` accepts them. */
type StatusLabelKey =
  | 'upload.status.queued'
  | 'upload.status.uploading'
  | 'upload.status.created'
  | 'upload.status.duplicate'
  | 'upload.status.error'

/** Translation key for each status label; kept literal for typed `t()`. */
function statusLabel(status: QueueItemStatus): StatusLabelKey {
  switch (status) {
    case 'queued':
      return 'upload.status.queued'
    case 'uploading':
      return 'upload.status.uploading'
    case 'created':
      return 'upload.status.created'
    case 'duplicate':
      return 'upload.status.duplicate'
    case 'error':
      return 'upload.status.error'
  }
}

/** Human-readable file size with a sensible unit. */
function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${String(bytes)} B`
  }
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`
}

/** Renders one warning line, translating known codes and falling back to text. */
function WarningLine({ warning }: { warning: UploadWarning }) {
  const { t } = useTranslation()
  const text =
    warning.code === 'near_duplicate' ? t('upload.warning.near_duplicate') : warning.message
  return <div className="small text-warning">{text}</div>
}

/**
 * A single queued file: name and size, a live progress bar while uploading, a
 * status badge, any non-fatal warnings, and contextual actions (remove a file
 * that has not started or finished; retry a failed one). Touch targets are
 * full-size buttons for mobile use.
 *
 * Rendered as a self-contained raised card rather than a `ListGroup.Item` so it
 * sits correctly inside the virtualized list (react-virtuoso positions each row
 * independently); a failed row gains a danger border so a handful of failures in
 * a large batch stand out even without the errors-only filter.
 */
export function UploadItem({ item, onRemove, onRetry }: UploadItemProps) {
  const { t } = useTranslation()
  const percent = Math.round(item.progress * 100)
  const errored = item.status === 'error'

  return (
    <div
      className={`kk-surface p-3 d-flex flex-column gap-2${errored ? ' border border-danger' : ''}`}
    >
      <div className="d-flex align-items-center justify-content-between gap-2">
        <div className="text-truncate">
          <span className="fw-semibold text-truncate d-inline-block align-bottom">
            {item.file.name}
          </span>
          <span className="text-secondary small ms-2">{formatBytes(item.file.size)}</span>
        </div>
        <Badge bg={BADGE_VARIANT[item.status]} className="flex-shrink-0">
          {t(statusLabel(item.status))}
        </Badge>
      </div>

      {item.status === 'uploading' && (
        <ProgressBar
          now={percent}
          label={`${String(percent)}%`}
          animated
          aria-label={t('upload.status.uploading')}
        />
      )}

      {item.status === 'error' && item.error !== undefined && item.error !== '' && (
        <div className="small text-danger">{item.error}</div>
      )}

      {item.warnings?.map((warning, index) => (
        <WarningLine key={`${warning.code}-${String(index)}`} warning={warning} />
      ))}

      <div className="d-flex gap-2">
        {item.status === 'error' && (
          <Button
            type="button"
            size="sm"
            variant="outline-primary"
            onClick={() => {
              onRetry(item.id)
            }}
          >
            {t('upload.actions.retry')}
          </Button>
        )}
        {item.status !== 'uploading' && (
          <Button
            type="button"
            size="sm"
            variant="outline-secondary"
            onClick={() => {
              onRemove(item.id)
            }}
          >
            {t('upload.actions.remove')}
          </Button>
        )}
      </div>
    </div>
  )
}
