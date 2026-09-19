import { useCallback, useEffect, useMemo, useState } from 'react'
import { Card } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { CommentsPanel } from '../components/photo/CommentsPanel'
import { TaskStateBadge } from '../components/tasks/TaskStateBadge'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { isNotFound } from '../services/auth'
import { taskSubject } from '../services/comments'
import { deleteTask, fetchTask, type Task, updateTask } from '../services/tasks'

import { TaskControls } from './task/TaskControls'
import { TaskQuestion } from './task/TaskQuestion'

/**
 * Fetch lifecycle of the task record. `missing` is a 404 kept apart from
 * `error`: a link sent into a chat outlives the task it points at, and "this
 * question has been deleted" is a different message from "could not be loaded".
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; task: Task }

/** Where the back link leads. */
const TASKS_PATH = '/tasks'

/**
 * One task: the question, the photographs it is about, and the conversation that
 * answers it.
 *
 * This is the page a link is sent to, so it is built for somebody who has never
 * seen Kukátko: the question is the heading, the pictures are under it, and the
 * box to write in is under those. Each of those three is a block of its own —
 * the byline and question, the wall, the conversation on its own card — because
 * a page whose parts run together is a page that has to be read to be
 * understood. The bookkeeping — state, resolution, the source query — comes
 * last and only for a writer; a viewer sees a question, some photographs and a
 * place to answer, which is all they need.
 *
 * Rewording the question happens where the question is, not in the card at the
 * foot: see `TaskQuestion`.
 *
 * The wall behaves like every other scoped list — filters, sort and tiles per
 * row, all round-tripping through the URL — because a task's group is often the
 * one somebody is about to edit, and having to relearn a different set of
 * controls here would be the odd thing. The *membership* stays frozen: a filter
 * narrows what of the group is on screen, never what the group is, and the count
 * beside the filter bar says how much of it is showing.
 */
export function TaskDetailPage() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, user, isAdmin } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // Reported up by the thread so the section's heading can carry its length.
  const [commentCount, setCommentCount] = useState(0)

  // The wall is an ordinary photo list: filters, sort and density live in the
  // URL exactly as they do on a label or an album, so Back restores them and a
  // link to a task carries the view it was shared in. The *membership* is still
  // frozen — a filter only narrows what of the group is on screen, and the count
  // beside the filter bar always says how much of it that is.
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ task: uid }), [uid])
  // Each tile carries the task scope, so the photo detail pages prev/next within
  // the task and Back returns to the question rather than to the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, task: uid, album: '', label: '', favorite: '', mode: '' }),
    [view, uid],
  )
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey, initialCount: restoreCount },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select, as on every other scoped list: the photographs a question is
  // about are usually the ones about to be edited, so the full batch vocabulary
  // belongs on this page rather than one navigation away.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const selecting = selection.count > 0

  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((photo) => photo.uid))
  }, [photos, selection])

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTask(uid, controller.signal)
      .then((task) => {
        setState({ status: 'ready', task })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: isNotFound(err) ? 'missing' : 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid, reloadKey])

  useDocumentTitle(state.status === 'ready' ? state.task.title : null)

  // Every curation control funnels through one save, so the page has one place
  // that knows how to apply an answer and one place that reports it failed.
  const save = useCallback(
    async (edit: Parameters<typeof updateTask>[1]): Promise<boolean> => {
      setBusy(true)
      try {
        const updated = await updateTask(uid, edit)
        setState({ status: 'ready', task: updated })
        return true
      } catch {
        return false
      } finally {
        setBusy(false)
      }
    },
    [uid],
  )

  const remove = useCallback(() => {
    setBusy(true)
    deleteTask(uid)
      .then(() => {
        void navigate(TASKS_PATH)
      })
      .catch(() => {
        setBusy(false)
        setConfirmDelete(false)
      })
  }, [navigate, uid])

  if (state.status === 'missing') {
    return (
      <ErrorState
        title={t('taskDetail.missing')}
        hint={t('taskDetail.missingHint')}
        action={<BackLink to={TASKS_PATH} label={t('taskDetail.back')} />}
      />
    )
  }

  if (state.status === 'error') {
    return (
      <ErrorState
        title={t('taskDetail.error')}
        action={<BackLink to={TASKS_PATH} label={t('taskDetail.back')} />}
      />
    )
  }

  if (state.status === 'loading') {
    return <GridSkeleton />
  }

  const task = state.task

  return (
    // Keep the last section scrollable clear of the floating batch bar while a
    // selection is active, so the discussion never hides behind it.
    <div style={{ paddingBottom: selecting ? 'var(--kk-batch-clearance)' : undefined }}>
      <div className="d-flex align-items-center justify-content-between gap-2 flex-wrap mb-2">
        <div className="d-flex align-items-center gap-2 flex-wrap kk-min-w-0">
          <BackLink to={TASKS_PATH} label={t('taskDetail.back')} />
          <TaskStateBadge state={task.state} />
        </div>
        {status === 'ready' && photos.length > 0 && (
          <SlideshowStart scope={scope} view={view} count={total} />
        )}
      </div>

      <TaskQuestion task={task} canEdit={canWrite} busy={busy} onSave={save} />

      {task.resolution !== '' && (
        <Card className="mb-4 border-success">
          <Card.Body>
            <h2 className="h6 text-body-secondary">{t('taskDetail.resolution')}</h2>
            <p className="mb-0">{task.resolution}</p>
          </Card.Body>
        </Card>
      )}

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}
      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}
      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('taskDetail.noPhotos')} hint={t('taskDetail.noPhotosHint')} />
      )}
      {status === 'ready' && photos.length > 0 && (
        <div className="mb-4">
          <PhotoGrid
            photos={photos}
            // The wall is a section between the question and the discussion, not
            // the page itself: the library's half-viewport reserve would be a
            // hole under the three photographs a task usually has.
            minHeight="0"
            loadingMore={loadingMore}
            moreError={moreError}
            onEndReached={loadMore}
            onRetry={retry}
            selection={bulk.gridSelection}
            detailQuery={detailQuery}
            scroll={gridScroll}
          />
        </div>
      )}

      {/* Anybody signed in may answer: that is what the link was sent for.
          The conversation is the page's other half, so it gets a surface of its
          own rather than running on from the photographs — a question, the
          pictures it is about, and then a visibly separate place to talk. */}
      <Card as="section" className="mb-4">
        <Card.Header className="d-flex align-items-center gap-2">
          <Icon name="chat-left-text" aria-hidden="true" />
          <h2 className="h6 mb-0">{t('taskDetail.discussion')}</h2>
          {commentCount > 0 && (
            <span className="text-body-secondary small">
              {t('photo.comments.count', { count: commentCount })}
            </span>
          )}
        </Card.Header>
        <Card.Body>
          <CommentsPanel
            subject={taskSubject(task.uid)}
            currentUserUid={user?.uid ?? null}
            canModerate={isAdmin}
            // The card is already headed "Diskuse"; the panel's own eyebrow
            // would say it a second time, so the count moves up beside it.
            heading={false}
            onCountChange={setCommentCount}
          />
        </Card.Body>
      </Card>

      {canWrite && (
        <TaskControls
          task={task}
          busy={busy}
          onSave={save}
          onDelete={() => {
            setConfirmDelete(true)
          }}
          onChanged={reload}
        />
      )}

      {bulk.canBulkEdit && selecting && (
        <BatchActionBar bulk={bulk} onSelectAll={selectAllInView} />
      )}

      <ConfirmModal
        show={confirmDelete}
        title={t('taskDetail.delete.title')}
        confirmLabel={t('taskDetail.delete.confirm')}
        variant="danger"
        busy={busy}
        onConfirm={remove}
        onCancel={() => {
          setConfirmDelete(false)
        }}
      >
        <p className="mb-0">{t('taskDetail.delete.body')}</p>
      </ConfirmModal>
    </div>
  )
}
