import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { DropZone } from '../components/upload/DropZone'
import { UploadList } from '../components/upload/UploadList'
import {
  organizeSelectionNames,
  UPLOAD_ALBUMS_FIELD_ID,
} from '../components/upload/organizeSelection'
import { UploadOrganize } from '../components/upload/UploadOrganize'
import { UploadProgressHeader } from '../components/upload/UploadProgressHeader'
import { UploadStep } from '../components/upload/UploadStep'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { usePasteFiles } from '../hooks/usePasteFiles'
import { useUploadOrganize } from '../hooks/useUploadOrganize'
import { useUploadQueue } from '../hooks/useUploadQueue'
import { SHARE_PARAM } from '../pwa/shareContract'
import { collectSharedFiles, type SharedFiles } from '../pwa/shareTarget'

/**
 * Multiupload page, laid out as the three steps it actually is — **pick files →
 * organise the batch → start** — each a numbered {@link UploadStep}, in that
 * order, so the flow is on screen instead of inferred from a wall of controls.
 * Steps 1 and 2 stay rendered from the first visit (an empty step 3 says what
 * will appear there), because a stage that only materialises once you have done
 * the previous one cannot be read as a sequence.
 *
 * Step 1: drag, paste or pick many files (gallery/camera on mobile). Step 2: the
 * albums and labels for the **whole batch** — its heading counts the queue
 * ("added to all 57 files"), and the sticky header of step 3 restates the choice
 * with one tap back here, which is what keeps the picker reachable once a long
 * queue has pushed it off screen. Step 3: the queue itself — a prominent sticky
 * overall-progress header and a virtualized per-file list with a local thumbnail
 * per row, retry of failures (whole-batch or per file, and an errors-only filter
 * to find them in a big batch), and a jump to the freshly added photos in the
 * library.
 *
 * Once every file settles, every resolved photo — new *or* duplicate — is added
 * to the chosen albums/labels in one bulk call, with an "assigning…" state and a
 * retryable error if that step alone fails; both are reported inside step 2,
 * beside the picker they belong to, rather than under a hundred queue rows.
 * Picking (or adding) an album or label only after the batch has finished
 * re-runs that assignment with the current selection. Every state and label is
 * translated (cs/en) and the controls are sized for touch.
 *
 * `?share=<id>` is how photos arrive from the phone's share sheet: the files are
 * already staged in the browser's cache by then (see `pwa/shareContract.ts`), so
 * the page collects them into the ordinary queue — same limits, same progress,
 * same albums and labels — and says what it took and what it could not.
 */
export function UploadPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('upload.title'))
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

  // Pasting is the third way in beside the picker and drag-and-drop, and the
  // only quick one on an iPhone (no share target there — see shareContract.ts).
  const addPasted = useCallback(
    (files: File[]) => {
      addFiles(files)
    },
    [addFiles],
  )
  usePasteFiles(addPasted)

  const [searchParams, setSearchParams] = useSearchParams()
  const shareId = searchParams.get(SHARE_PARAM)
  const [shared, setShared] = useState<SharedFiles | null>(null)
  // Ids already handed to `collectSharedFiles`. Collecting consumes the cache
  // entries, so a second pass would find nothing and report an empty share —
  // which StrictMode's double-invoked effect would otherwise do every time.
  const collected = useRef(new Set<string>())

  useEffect(() => {
    if (shareId === null || shareId === '' || collected.current.has(shareId)) {
      return
    }
    collected.current.add(shareId)
    void collectSharedFiles(shareId).then((result) => {
      if (result.accepted.length > 0) {
        addFiles(result.accepted)
      }
      setShared(result)
      // Drop the parameter once the share is in hand: a reload would otherwise
      // look like a second share that has already been consumed. `replace` so
      // Back does not walk into the spent URL either.
      setSearchParams(
        (previous) => {
          const next = new URLSearchParams(previous)
          next.delete(SHARE_PARAM)
          return next
        },
        { replace: true },
      )
    })
  }, [shareId, addFiles, setSearchParams])

  // Once every file has settled, assign the whole batch to the chosen albums and
  // labels — but only when something is chosen and at least one photo resolved.
  // Editing the selection afterwards puts `assign` back to `idle` (see
  // `useUploadOrganize`), so this also picks up a choice made after the fact
  // instead of dropping it.
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

  // What the batch is tagged with, for the recap in the sticky header.
  const organizeNames = useMemo(
    () => organizeSelectionNames(organizeLoad, albums, labels),
    [organizeLoad, albums, labels],
  )

  // Back to step 2 from anywhere in step 3. Centring the field clears both the
  // sticky navbar and the queue's own sticky header, and focusing it (without a
  // second, competing scroll) means a keyboard user lands *in* the picker rather
  // than merely near it — the field opens its suggestions on focus, so the tap
  // ends ready to type. The id is the one `MultiSelect` already publishes for its
  // label, which is why no ref is threaded down for this.
  const editOrganize = useCallback(() => {
    const field = document.getElementById(UPLOAD_ALBUMS_FIELD_ID)
    field?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    field?.focus({ preventScroll: true })
  }, [])

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('upload.title')}</h1>
      <p className="text-secondary">{t('upload.subtitle')}</p>

      <UploadStep
        index={1}
        title={t('upload.step.pick.title')}
        hint={t('upload.step.pick.hint')}
        status={hasItems ? t('upload.step.pick.picked', { count: items.length }) : undefined}
      >
        {shared !== null && (
          <>
            {shared.accepted.length > 0 && (
              <Alert variant="info" aria-live="polite">
                {t('share.notice.staged', { count: shared.accepted.length })}
              </Alert>
            )}
            {shared.accepted.length === 0 && shared.rejected.length === 0 && (
              <Alert variant="warning" aria-live="polite">
                {t('share.notice.empty')}
              </Alert>
            )}
            {shared.rejected.length > 0 && (
              <Alert variant="warning" aria-live="polite">
                {t('share.notice.rejected', { names: shared.rejected.join(', ') })}
              </Alert>
            )}
          </>
        )}

        <DropZone onFiles={addFiles} />
      </UploadStep>

      <UploadStep
        index={2}
        title={t('upload.organize.heading')}
        hint={
          hasItems
            ? t('upload.organize.hintCount', { count: items.length })
            : t('upload.organize.hint')
        }
      >
        <UploadOrganize
          load={organizeLoad}
          albums={albums}
          labels={labels}
          onAlbums={setAlbums}
          onLabels={setLabels}
          disabled={assigning}
          allowCreate={canWrite}
        />

        {assigning && (
          <Alert
            variant="info"
            className="d-flex align-items-center gap-2 mt-3 mb-0"
            aria-live="polite"
          >
            <Spinner animation="border" role="status" size="sm">
              <span className="visually-hidden">{t('upload.organize.assigning')}</span>
            </Spinner>
            <span>{t('upload.organize.assigning')}</span>
          </Alert>
        )}

        {assign.status === 'done' && (
          <Alert variant="success" className="mt-3 mb-0" aria-live="polite">
            {t('upload.organize.assigned')}
          </Alert>
        )}

        {assign.status === 'error' && (
          <Alert variant="danger" className="mt-3 mb-0" aria-live="polite">
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
      </UploadStep>

      <UploadStep
        index={3}
        title={t('upload.step.upload.title')}
        hint={t('upload.step.upload.hint')}
      >
        {!hasItems && <p className="text-secondary mb-0">{t('upload.step.upload.empty')}</p>}

        {hasItems && (
          <>
            <UploadProgressHeader
              summary={summary}
              progress={progress}
              isComplete={isComplete}
              hasCreated={createdUids.length > 0}
              onRetryFailed={retryFailed}
              // Only while there is a picker to go back to: with the catalogs
              // unloaded (or still loading) step 2 shows a spinner or its error,
              // and a "Change" button pointing at nothing is worse than none.
              organize={
                organizeLoad.status === 'ready'
                  ? { names: organizeNames, onEdit: editOrganize }
                  : undefined
              }
            />

            <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2">
              <h3 className="kk-text-eyebrow text-secondary mb-0">{t('upload.queue.heading')}</h3>
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
      </UploadStep>
    </>
  )
}
