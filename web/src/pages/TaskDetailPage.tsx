import { useCallback, useEffect, useMemo, useState } from 'react'
import { Card } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Markdown } from '../components/Markdown'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { CommentsPanel } from '../components/photo/CommentsPanel'
import { TaskStateBadge } from '../components/tasks/TaskStateBadge'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { LIBRARY_DEFAULTS, viewToParams } from '../lib/libraryView'
import { isNotFound } from '../services/auth'
import { taskSubject } from '../services/comments'
import { deleteTask, fetchTask, type Task, updateTask } from '../services/tasks'

import { TaskControls } from './task/TaskControls'

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
 * box to write in is under those. The curation controls — state, resolution,
 * membership — come last and only for a writer; a viewer sees a question, some
 * photographs and a place to answer, which is all they need.
 *
 * The grid carries no filter bar. The group is frozen by design, so filtering it
 * would only ever hide part of the evidence the question rests on.
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

  const params = useMemo(() => viewToParams(LIBRARY_DEFAULTS), [])
  const scope = useMemo(() => ({ task: uid }), [uid])
  // Each tile carries the task scope, so the photo detail pages prev/next within
  // the task and Back returns to the question rather than to the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...LIBRARY_DEFAULTS, task: uid, mode: '' }),
    [uid],
  )
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const { photos, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey, initialCount: restoreCount },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

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
    <>
      <div className="d-flex align-items-center gap-2 flex-wrap mb-2">
        <BackLink to={TASKS_PATH} label={t('taskDetail.back')} />
        <TaskStateBadge state={task.state} />
      </div>

      {/* The question is the page. Everything else on it exists to answer this. */}
      <h1 className="kk-page-title mb-2">{task.title}</h1>
      <p className="text-body-secondary small mb-3">
        {t('taskDetail.opened', { name: task.created_by_name || t('taskDetail.someone') })}
        {' · '}
        {t('tasks.row.photos', { count: task.photo_count })}
      </p>

      {task.body !== '' && (
        <Card className="mb-4">
          <Card.Body>
            <Markdown>{task.body}</Markdown>
          </Card.Body>
        </Card>
      )}

      {task.resolution !== '' && (
        <Card className="mb-4 border-success">
          <Card.Body>
            <h2 className="h6 text-body-secondary">{t('taskDetail.resolution')}</h2>
            <p className="mb-0">{task.resolution}</p>
          </Card.Body>
        </Card>
      )}

      {status === 'loading' && <GridSkeleton />}
      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}
      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('taskDetail.noPhotos')} hint={t('taskDetail.noPhotosHint')} />
      )}
      {status === 'ready' && photos.length > 0 && (
        <div className="mb-4">
          <PhotoGrid
            photos={photos}
            loadingMore={loadingMore}
            moreError={moreError}
            onEndReached={loadMore}
            onRetry={retry}
            detailQuery={detailQuery}
            scroll={gridScroll}
          />
        </div>
      )}

      {/* Anybody signed in may answer: that is what the link was sent for. */}
      <section className="mb-4">
        <h2 className="h5">{t('taskDetail.discussion')}</h2>
        <CommentsPanel
          subject={taskSubject(task.uid)}
          currentUserUid={user?.uid ?? null}
          canModerate={isAdmin}
        />
      </section>

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
    </>
  )
}
