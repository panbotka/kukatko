import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Card } from 'react-bootstrap'
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
import {
  type BatchDiscussion,
  type BatchExtraAction,
  BatchActionBar,
} from '../components/organize/BatchActionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { CommentsPanel } from '../components/photo/CommentsPanel'
import { TaskLedger } from '../components/tasks/TaskLedger'
import { TaskStateBadge } from '../components/tasks/TaskStateBadge'
import { TaskViewToggle } from '../components/tasks/TaskViewToggle'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { isTaskListView, TASK_DEFAULTS, type TaskView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { isNotFound } from '../services/auth'
import { type Comment, taskSubject } from '../services/comments'
import {
  deleteTask,
  fetchTask,
  type Participant,
  removeTaskPhotos,
  type Task,
  updateTask,
} from '../services/tasks'

import { TaskAnswers } from './task/TaskAnswers'
import { TaskControls } from './task/TaskControls'
import { TaskParticipants } from './task/TaskParticipants'
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
 * The largest frozen group the page still shows without the filter bar. A
 * two-photo question does not need a search field, a sort and a density control
 * between the question and its pictures; a batch of forty does.
 */
export const SMALL_GROUP_MAX = 12

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
  // The thread itself, reported up so the answer buttons can mark the option
  // matching the reader's latest comment as chosen; and the key that makes the
  // panel refetch after a button posted a comment behind its back.
  const [thread, setThread] = useState<Comment[]>([])
  const [threadKey, reloadThread] = useReloadKey()
  // The people on the task. It starts from the fetched task and is replaced by
  // every membership change's own answer, so a change redraws without a refetch.
  const [people, setPeople] = useState<Participant[]>([])
  // Set when taking photographs out of the group failed, so the page can say so
  // instead of looking as though nothing was clicked.
  const [removeFailed, setRemoveFailed] = useState(false)

  // The wall is an ordinary photo list: filters, sort and density live in the
  // URL exactly as they do on a label or an album, so Back restores them and a
  // link to a task carries the view it was shared in. The *membership* is still
  // frozen — a filter only narrows what of the group is on screen, and the count
  // beside the filter bar always says how much of it that is. The layout — wall
  // or ledger (`?view=list`) — is one more piece of that view state.
  const [view, setView] = useUrlState<TaskView>(TASK_DEFAULTS)
  const listView = isTaskListView(view.view)
  // Switching wall ⇄ ledger changes the URL (and so `view`) but not the query:
  // the params compile to the same value, which is what the paginated hook keys
  // on, so the group is not refetched for a change in how it is drawn.
  const params = useMemo(() => viewToParams(view), [view])
  const setLayout = useCallback(
    (next: string) => {
      setView({ view: next })
    },
    [setView],
  )
  const scope = useMemo(() => ({ task: uid }), [uid])
  // Each tile carries the task scope, so the photo detail pages prev/next within
  // the task and Back returns to the question rather than to the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, task: uid, album: '', label: '', favorite: '', mode: '' }),
    [view, uid],
  )
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  // A task about the library rather than about photographs has no wall at all:
  // the photo list is fetched only once the task is known to be over something
  // — a request for an empty group would only come back empty — and nothing
  // below draws a grid, a filter bar or a removal for it. Waiting for the task
  // costs nothing visible: the page is a skeleton until the task has loaded.
  const knownEmpty = state.status === 'ready' && state.task.photo_count === 0
  const hasGroup = state.status === 'ready' && state.task.photo_count > 0
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey, initialCount: restoreCount, enabled: hasGroup },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select, as on every other scoped list: the photographs a question is
  // about are usually the ones about to be edited, so the full batch vocabulary
  // belongs on this page rather than one navigation away. Open to viewers too:
  // pointing at pictures from the discussion is an answer, and answering has
  // always been open to every role — the bar hides the writer-only actions.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true, openToViewers: true })
  const selection = bulk.selection
  const selecting = selection.count > 0

  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((photo) => photo.uid))
  }, [photos, selection])

  // Taking photographs out of the group is the task page's own action, merged
  // into the shared bar rather than shown on a toolbar of its own — the same
  // shape an album's "remove from album" uses.
  const removeSelected = useCallback(async () => {
    const uids = [...selection.selected]
    if (uids.length === 0) {
      return
    }
    setRemoveFailed(false)
    try {
      await removeTaskPhotos(uid, uids)
      // Leave selection mode before reloading: the removed photographs vanish
      // from the wall, and a selection still holding their uids would send them
      // to the next action. A failed removal keeps it, so it can be retried.
      selection.disable()
      reload()
    } catch {
      setRemoveFailed(true)
    }
  }, [selection, uid, reload])

  // Removal changes the group and is a writer's; a viewer's bar has none, and
  // neither does a task with nothing to remove.
  const extraActions = useMemo<BatchExtraAction[]>(
    () =>
      canWrite && !knownEmpty
        ? [
            {
              id: 'remove-from-task',
              icon: 'dash-lg',
              label: t('taskDetail.removeSelected'),
              danger: true,
              onClick: () => void removeSelected(),
            },
          ]
        : [],
    [canWrite, knownEmpty, t, removeSelected],
  )

  // Putting a selection into the discussion posts a comment behind the panel's
  // back, so the thread is asked to refetch, exactly as after a quick answer.
  const discussion = useMemo<BatchDiscussion>(
    () => ({ taskUid: uid, onPosted: reloadThread }),
    [uid, reloadThread],
  )

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTask(uid, controller.signal)
      .then((task) => {
        setState({ status: 'ready', task })
        setPeople(task.participants)
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

  // The reader's most recent live comment, oldest-first thread read backwards.
  const chosen = useMemo(() => {
    if (!user) {
      return null
    }
    for (let i = thread.length - 1; i >= 0; i--) {
      if (thread[i]?.author_uid === user.uid) {
        return thread[i]?.body ?? null
      }
    }
    return null
  }, [thread, user])

  // Every curation control funnels through one save, so the page has one place
  // that knows how to apply an answer and one place that reports it failed.
  const save = useCallback(
    async (edit: Parameters<typeof updateTask>[1]): Promise<boolean> => {
      setBusy(true)
      try {
        const updated = await updateTask(uid, edit)
        setState({ status: 'ready', task: updated })
        setPeople(updated.participants)
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

      <TaskAnswers task={task} chosen={chosen} onAnswered={reloadThread} />

      <TaskParticipants
        taskUid={task.uid}
        participants={people}
        canEdit={canWrite}
        onChange={setPeople}
      />

      {task.resolution !== '' && (
        <Card className="mb-4 border-success">
          <Card.Body>
            <h2 className="h6 text-body-secondary">{t('taskDetail.resolution')}</h2>
            <p className="mb-0">{task.resolution}</p>
          </Card.Body>
        </Card>
      )}

      {/* A task over nothing says so in one muted line and draws no wall: no
          grid, no filter bar, nothing to select. A small group gets its count
          and its pictures, nothing to filter them with: a search field over
          two photographs is furniture. */}
      {task.photo_count === 0 && (
        <p className="text-body-secondary small mb-4">{t('tasks.row.noPhotos')}</p>
      )}
      {task.photo_count > SMALL_GROUP_MAX && (
        <FilterBar
          view={view}
          onChange={setView}
          total={total}
          displayExtras={<TaskViewToggle value={view.view} onChange={setLayout} />}
        />
      )}
      {/* A small group has no filter bar to host the layout toggle, so it sits
          on the count line: a five-photo batch in review is read as a ledger
          just as a sixty-photo one is. */}
      {task.photo_count > 0 && task.photo_count <= SMALL_GROUP_MAX && status === 'ready' && (
        <div className="d-flex align-items-center justify-content-between gap-2 mb-2">
          <p className="text-body-secondary small mb-0">
            {t('tasks.row.photos', { count: total })}
          </p>
          <TaskViewToggle value={view.view} onChange={setLayout} size="sm" />
        </div>
      )}

      {status === 'loading' && <GridSkeleton />}
      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}
      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('taskDetail.noPhotos')} hint={t('taskDetail.noPhotosHint')} />
      )}
      {removeFailed && (
        <Alert variant="danger" className="py-2">
          {t('taskDetail.removeFailed')}
        </Alert>
      )}
      {status === 'ready' && photos.length > 0 && listView && (
        <div className="mb-4">
          <TaskLedger
            photos={photos}
            loadingMore={loadingMore}
            moreError={moreError}
            onEndReached={loadMore}
            onRetry={retry}
            selection={bulk.gridSelection}
            detailQuery={detailQuery}
          />
        </div>
      )}
      {status === 'ready' && photos.length > 0 && !listView && (
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
            onThreadChange={setThread}
            reloadKey={threadKey}
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
        <BatchActionBar
          bulk={bulk}
          onSelectAll={selectAllInView}
          extraActions={extraActions}
          discussion={discussion}
        />
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
