import { useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type OrganizeAssignState } from '../../hooks/useUploadOrganize'
import {
  type QueueItemStatus,
  type UploadQueueItem,
  type UploadSummary,
} from '../../hooks/useUploadQueue'
import { LIBRARY_PATH } from '../../lib/libraryView'

import { batchMedia } from './batchMedia'
import { UploadActionBar } from './UploadActionBar'
import { UploadOrganize, type UploadOrganizeProps } from './UploadOrganize'
import { UploadQueuePanel } from './UploadQueuePanel'

/** The picked files behind every item of the batch in one of the given states. */
function filesIn(items: UploadQueueItem[], statuses: readonly QueueItemStatus[]): File[] {
  return items.filter((item) => statuses.includes(item.status)).map((item) => item.file)
}

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
 * The sentence says what actually went up: stills and clips are counted apart
 * ("3 photos and 1 video uploaded"), because congratulating someone on a photo
 * they never sent is simply false. The split is the client's own — `batchMedia`
 * over the picked files, the same classification the queue's play glyph uses —
 * so nothing is asked of the per-file result, which carries no media type. A
 * batch with no clips in it is worded exactly as it always has been. The
 * duplicate and failed counts stay counts of *files*: a duplicate clip and a
 * duplicate photo are the same event and neither is claimed as uploaded.
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

  // What the batch may be called. The *uploaded* files decide the counted
  // sentence; everything that landed — a duplicate included, since it is filed
  // along with the rest — decides the copy around the album picker.
  const uploaded = useMemo(() => batchMedia(filesIn(items, ['created'])), [items])
  const filed = useMemo(() => batchMedia(filesIn(items, ['created', 'duplicate'])), [items])

  /** The two halves of a mixed batch, each already in its own plural form. */
  function mixedParts(): { photos: string; videos: string } {
    return {
      photos: t('upload.done.photosPart', { count: uploaded.photos }),
      videos: t('upload.done.videosPart', { count: uploaded.videos }),
    }
  }

  /** The uploaded count, named after what was in fact uploaded. */
  function uploadedSentence(): string {
    switch (uploaded.kind) {
      case 'videos':
        return t('upload.done.uploadedVideos', { count: uploaded.videos })
      case 'mixed':
        return t('upload.done.uploadedMixed', mixedParts())
      default:
        return t('upload.done.uploaded', { count: summary.created })
    }
  }

  /** The same, with the albums and labels the batch has just been filed into. */
  function uploadedToSentence(names: string): string {
    switch (uploaded.kind) {
      case 'videos':
        return t('upload.done.uploadedToVideos', { count: uploaded.videos, names })
      case 'mixed':
        return t('upload.done.uploadedToMixed', { ...mixedParts(), names })
      default:
        return t('upload.done.uploadedTo', { count: summary.created, names })
    }
  }

  /** The one-sentence outcome, in the order the reader needs it. */
  function outcome(): string {
    if (failed) {
      return t('upload.done.failed', { count: summary.error })
    }
    if (summary.created === 0) {
      return t('upload.done.allDuplicates', { count: summary.duplicate })
    }
    if (assigned) {
      return uploadedToSentence(organizeNames.join(', '))
    }
    return uploadedSentence()
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
          {/* Both lines are about what landed, so a batch holding a clip takes
              the wording that covers both rather than one about photographs. */}
          <p className="kk-text-caption text-secondary mb-2">
            {organizeNames.length === 0
              ? t(filed.kind === 'photos' ? 'upload.done.noAlbum' : 'upload.done.noAlbumWithVideo')
              : t(
                  filed.kind === 'photos'
                    ? 'upload.organize.hint'
                    : 'upload.organize.hintWithVideo',
                )}
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
