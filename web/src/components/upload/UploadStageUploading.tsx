import { useTranslation } from 'react-i18next'

import { type UploadQueueItem, type UploadSummary } from '../../hooks/useUploadQueue'

import { PickFilesButton } from './PickFilesButton'
import { UploadActionBar } from './UploadActionBar'
import { UploadOrganize, type UploadOrganizeProps } from './UploadOrganize'
import { UploadQueuePanel } from './UploadQueuePanel'

/** Props for {@link UploadStageUploading}. */
export interface UploadStageUploadingProps {
  /** Aggregate counts across the batch. */
  summary: UploadSummary
  /** Overall completion as a fraction in `[0, 1]`. */
  progress: number
  /** Every file in the batch, for the demoted per-file list. */
  items: UploadQueueItem[]
  /** The batch-wide album/label picker, wired by the page. */
  organize: UploadOrganizeProps
  /** Appends more files to the running batch. */
  onFiles: (files: File[]) => void
  /** Removes a file from the queue. */
  onRemove: (id: string) => void
  /** Re-queues a single failed file. */
  onRetry: (id: string) => void
}

/**
 * Stage two: the batch is uploading, so the page is the wait.
 *
 * The progress lives in the action bar at the bottom edge — the bar's top rule
 * fills as the batch does — and never anywhere else, which is the whole point:
 * the old page had the numbers in a sticky header and the controls in a
 * different part of the document, so on a phone you always ended up looking at
 * whichever one the queue had not yet pushed away.
 *
 * What the body holds is the one useful thing to do meanwhile: choose the albums
 * and labels for the whole batch. That works because the assignment runs when
 * the batch settles, not when it starts (see `useUploadOrganize`) — picking an
 * album while the photos are still going up is exactly as good as picking it
 * first, and a pick made even later still lands. The per-file list is below,
 * closed, in {@link UploadQueuePanel}.
 *
 * Adding files here appends to the running batch: the queue drains whatever it
 * holds, so nothing in flight is restarted or dropped.
 */
export function UploadStageUploading({
  summary,
  progress,
  items,
  organize,
  onFiles,
  onRemove,
  onRetry,
}: UploadStageUploadingProps) {
  const { t } = useTranslation()

  const done = summary.created + summary.duplicate + summary.error
  const remaining = summary.queued + summary.uploading

  return (
    <section className="kk-upload-stage" aria-labelledby="upload-stage-title">
      <div>
        <h2 id="upload-stage-title" className="kk-section-title mb-1">
          {t('upload.running.title')}
        </h2>
        <p className="text-secondary mb-0">{t('upload.running.lead')}</p>
      </div>

      <div>
        <h3 className="kk-text-eyebrow text-secondary mb-1">{t('upload.organize.heading')}</h3>
        <p className="kk-text-caption text-secondary mb-2">{t('upload.organize.hint')}</p>
        <UploadOrganize {...organize} />
      </div>

      <UploadQueuePanel items={items} summary={summary} onRemove={onRemove} onRetry={onRetry} />

      <UploadActionBar
        progress={{
          percent: Math.round(progress * 100),
          count: t('upload.progress.count', { done, total: summary.total }),
          remaining: t('upload.progress.remaining', { count: remaining }),
        }}
      >
        <PickFilesButton
          onFiles={onFiles}
          label={t('upload.pick.more')}
          inputLabel={t('upload.pick.ariaInput')}
          variant="outline-secondary"
        />
      </UploadActionBar>
    </section>
  )
}
