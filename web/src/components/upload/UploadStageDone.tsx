import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type OrganizeAssignState } from '../../hooks/useUploadOrganize'
import { type UploadQueueItem, type UploadSummary } from '../../hooks/useUploadQueue'
import { LIBRARY_PATH } from '../../lib/libraryView'

import { UploadActionBar } from './UploadActionBar'
import { UploadOrganize, type UploadOrganizeProps } from './UploadOrganize'
import { UploadQueuePanel } from './UploadQueuePanel'

/** Props for {@link UploadStageDone}. */
export interface UploadStageDoneProps {
  /** Aggregate counts of the finished batch. */
  summary: UploadSummary
  /** Every file of the batch, for the per-file list a failure brings back. */
  items: UploadQueueItem[]
  /** The batch-wide album/label picker, still live — a pick now still applies. */
  organize: UploadOrganizeProps
  /** Human names of everything chosen, for the outcome sentence. */
  organizeNames: string[]
  /** Lifecycle of the album/label assignment that runs once the batch settles. */
  assign: OrganizeAssignState
  /** Re-queues every failed file. */
  onRetryFailed: () => void
  /** Drops one file from the batch. */
  onRemove: (id: string) => void
  /** Re-queues one failed file. */
  onRetry: (id: string) => void
  /** Retries the album/label assignment after it alone failed. */
  onRetryAssign: () => void
  /** Empties the queue and returns to stage one, keeping the album/label choice. */
  onUploadMore: () => void
}

/**
 * Stage three: what happened, and the one thing to do about it.
 *
 * The outcome is a sentence, not a table of counts — "20 photos uploaded, added
 * to Pouť 2026" — and it only claims the album once the assignment has actually
 * come back; until then it says what was uploaded and reports the assignment
 * separately. A batch with failures leads with the failures instead, and the
 * primary action becomes retrying them, because a count of successes is not what
 * the reader needs at that moment.
 *
 * The picker stays on the page. Finishing with no album chosen is the case worth
 * catching — those photos are otherwise quietly untagged — so the stage says so
 * and offers the field right there; `useUploadOrganize` re-arms on a change, so
 * choosing now assigns the batch that has already finished.
 *
 * The per-file list comes back only when something failed — that is where the
 * reason, the per-file **Retry** and the errors-only filter live, and a batch
 * that went through has no use for fifty rows saying so.
 *
 * **Upload more** returns to stage one but keeps the albums and labels: the next
 * batch is almost always more of the same event, and re-picking "Pouť 2026" for
 * every camera roll is the kind of tax that stops people uploading at all.
 */
export function UploadStageDone({
  summary,
  items,
  organize,
  organizeNames,
  assign,
  onRetryFailed,
  onRemove,
  onRetry,
  onRetryAssign,
  onUploadMore,
}: UploadStageDoneProps) {
  const { t } = useTranslation()

  const failed = summary.error > 0
  const landed = summary.created + summary.duplicate
  const assigned = assign.status === 'done' && organizeNames.length > 0

  /** The one-sentence outcome, in the order the reader needs it. */
  function outcome(): string {
    if (failed) {
      return t('upload.done.failed', { count: summary.error })
    }
    if (summary.created === 0) {
      return t('upload.done.allDuplicates', { count: summary.duplicate })
    }
    if (assigned) {
      return t('upload.done.uploadedTo', {
        count: summary.created,
        names: organizeNames.join(', '),
      })
    }
    return t('upload.done.uploaded', { count: summary.created })
  }

  const libraryLink = (
    <Link
      to={`${LIBRARY_PATH}?sort=added`}
      className={`btn btn-lg ${failed ? 'btn-outline-secondary' : 'btn-primary'}`}
    >
      {t('upload.done.viewLibrary')}
    </Link>
  )

  return (
    <section className="kk-upload-stage" aria-labelledby="upload-stage-title">
      <div>
        <h2 id="upload-stage-title" className="kk-upload-outcome mb-2" aria-live="polite">
          {outcome()}
        </h2>
        {/* Only where it is true: with nothing at all through, "everything else
            is in your library" names a set that does not exist. */}
        {failed && landed > 0 && (
          <p className="text-secondary mb-0">{t('upload.done.failedHint')}</p>
        )}
        {!failed && summary.created > 0 && summary.duplicate > 0 && (
          <p className="text-secondary mb-0">
            {t('upload.done.duplicates', { count: summary.duplicate })}
          </p>
        )}
      </div>

      {assign.status === 'assigning' && (
        <Alert variant="info" className="d-flex align-items-center gap-2 mb-0" aria-live="polite">
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('upload.organize.assigning')}</span>
          </Spinner>
          <span>{t('upload.organize.assigning')}</span>
        </Alert>
      )}

      {assign.status === 'error' && (
        <Alert variant="danger" className="mb-0" aria-live="polite">
          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
            <span>
              {assign.message === ''
                ? t('upload.organize.assignErrorGeneric')
                : t('upload.organize.assignError', { message: assign.message })}
            </span>
            <Button type="button" variant="outline-light" size="sm" onClick={onRetryAssign}>
              {t('upload.organize.retry')}
            </Button>
          </div>
        </Alert>
      )}

      {/* Only when there are photos to put somewhere. A batch where every file
          failed has nothing to file, and offering an album picker over it reads
          as if the upload had half worked. */}
      {landed > 0 && (
        <div>
          <h3 className="kk-text-eyebrow text-secondary mb-1">{t('upload.organize.heading')}</h3>
          <p className="kk-text-caption text-secondary mb-2">
            {organizeNames.length === 0 ? t('upload.done.noAlbum') : t('upload.organize.hint')}
          </p>
          <UploadOrganize {...organize} />
        </div>
      )}

      {failed && (
        <UploadQueuePanel items={items} summary={summary} onRemove={onRemove} onRetry={onRetry} />
      )}

      <UploadActionBar>
        {failed ? (
          <>
            {landed > 0 && libraryLink}
            <Button type="button" size="lg" variant="primary" onClick={onRetryFailed}>
              {t('upload.actions.retryFailed')}
            </Button>
          </>
        ) : (
          <>
            <Button type="button" size="lg" variant="outline-secondary" onClick={onUploadMore}>
              {t('upload.done.uploadMore')}
            </Button>
            {libraryLink}
          </>
        )}
      </UploadActionBar>
    </section>
  )
}
