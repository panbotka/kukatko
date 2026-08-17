import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { organizeSelectionNames } from '../components/upload/organizeSelection'
import { type UploadOrganizeProps } from '../components/upload/UploadOrganize'
import { UploadStageDone } from '../components/upload/UploadStageDone'
import { UploadStagePick } from '../components/upload/UploadStagePick'
import { UploadStageUploading } from '../components/upload/UploadStageUploading'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useLeaveGuard } from '../hooks/useLeaveGuard'
import { usePasteFiles } from '../hooks/usePasteFiles'
import { useUploadOrganize } from '../hooks/useUploadOrganize'
import { useUploadQueue } from '../hooks/useUploadQueue'
import { SHARE_PARAM } from '../pwa/shareContract'
import { collectSharedFiles, type SharedFiles } from '../pwa/shareTarget'

/**
 * The upload page: **one stage on screen at a time**, and the upload starts by
 * itself.
 *
 * Which stage is showing is not a mode anyone sets — it is read off the queue.
 * Empty queue → *pick*; files that have not all settled → *uploading*; every file
 * settled → *done*. So there is nothing to keep in sync and no way to be in the
 * wrong stage: picking files puts bytes on the wire and moves the page in one
 * step, adding more mid-flight keeps the page where it is, and **Upload more**
 * (which only empties the queue) walks it back to the start.
 *
 * That is the answer to what the page got wrong on a phone. It used to show
 * three numbered steps at once — an album picker with no batch, an empty queue
 * explaining itself, and a start button that a fifty-file queue promptly pushed
 * below the fold, with the progress in a sticky header somewhere else again. Now
 * every stage ends in one action bar at the bottom edge, which carries both the
 * progress and the stage's primary action and never scrolls away, on a phone and
 * on a desktop alike.
 *
 * The album/label choice moved *into* the wait rather than in front of it, which
 * works without any new backend behaviour: `useUploadOrganize` assigns once the
 * batch settles and re-arms whenever the selection changes, so a pick made while
 * the photos upload — or after they have finished — is applied just the same.
 *
 * Three ways in, all of them the same queue: the picker (gallery or camera),
 * pasting anywhere on the page, and `?share=<id>` from the phone's share sheet.
 * A share is already staged in the browser's cache by then (see
 * `pwa/shareContract.ts`), so it is collected into the queue and lands directly
 * in the uploading stage, with a note saying what came through and what did not.
 *
 * Because the queue only exists in this tab, leaving mid-upload throws it away —
 * so {@link useLeaveGuard} holds an in-app link back and asks first, and lets
 * the browser put its own warning on a tab close.
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
    resolvedUids,
    addFiles,
    removeItem,
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

  // A fresh batch (files added to a finished one, cleared, or a failed upload
  // retried) clears a prior assignment result so the next completion assigns
  // again.
  useEffect(() => {
    if (!isComplete && (assign.status === 'done' || assign.status === 'error')) {
      resetAssign()
    }
  }, [isComplete, assign.status, resetAssign])

  const assigning = assign.status === 'assigning'

  // The queue is browser state: a navigation away ends it mid-flight and the
  // photos never arrive. That includes the assignment, which is the last thing
  // to run and the easiest to lose without noticing.
  const leaving = useLeaveGuard(isUploading || summary.queued > 0 || assigning)

  // What the batch is tagged with, for the outcome sentence.
  const organizeNames = useMemo(
    () => organizeSelectionNames(organizeLoad, albums, labels),
    [organizeLoad, albums, labels],
  )

  // The picker is the same control in both of the stages that show it, so it is
  // wired once here and handed over whole.
  const organize: UploadOrganizeProps = {
    load: organizeLoad,
    albums,
    labels,
    onAlbums: setAlbums,
    onLabels: setLabels,
    disabled: assigning,
    allowCreate: canWrite,
  }

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('upload.title')}</h1>

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

      {items.length === 0 && <UploadStagePick onFiles={addFiles} />}

      {items.length > 0 && !isComplete && (
        <UploadStageUploading
          summary={summary}
          progress={progress}
          items={items}
          organize={organize}
          onFiles={addFiles}
          onRemove={removeItem}
          onRetry={retry}
        />
      )}

      {items.length > 0 && isComplete && (
        <UploadStageDone
          summary={summary}
          items={items}
          organize={organize}
          organizeNames={organizeNames}
          assign={assign}
          onRetryFailed={retryFailed}
          onRemove={removeItem}
          onRetry={retry}
          onRetryAssign={retryAssign}
          onUploadMore={clear}
        />
      )}

      <ConfirmModal
        show={leaving.asking}
        title={t('upload.leave.title')}
        confirmLabel={t('upload.leave.confirm')}
        cancelLabel={t('upload.leave.stay')}
        onConfirm={leaving.confirm}
        onCancel={leaving.cancel}
      >
        {t('upload.leave.body', { count: summary.queued + summary.uploading })}
      </ConfirmModal>
    </>
  )
}
