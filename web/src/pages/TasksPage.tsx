import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, Form, Spinner } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { NewTaskModal } from '../components/tasks/NewTaskModal'
import { TaskStateBadge } from '../components/tasks/TaskStateBadge'
import { useAuth } from '../auth/AuthContext'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import { useUrlState } from '../lib/urlState'
import { thumbUrl } from '../services/photos'
import { fetchTasks, type Task, TASK_STATES, type TaskState } from '../services/tasks'

/** Fetch lifecycle of the listing. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; tasks: Task[]; total: number }

/**
 * View state kept in the URL, so Back restores the filter the reader was on —
 * the project's "Back always works" rule. `state` is empty for the default (the
 * open ones), `all` for every state, or one state's name.
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
type TasksView = {
  state: string
  answered: string
  q: string
}

const TASKS_DEFAULTS: TasksView = { state: '', answered: '', q: '' }

/** The filter row's choices: the default, every state, and one chip per state. */
const FILTER_CHOICES = ['', 'all', ...TASK_STATES] as const

/**
 * The work queue: every question waiting on somebody, most recently touched
 * first, with the ones that have been answered marked as such.
 *
 * Open tasks come first and a reply counts as activity, so a task somebody
 * answered an hour ago sits above one edited last week. That ordering is the
 * whole point of the page: it is read to find out what to do next.
 *
 * Every signed-in role sees it, viewers included. A viewer cannot open or close
 * a task but can answer one, and the listing is how they find the question they
 * were sent a link to when the link has scrolled out of their chat.
 */
export function TasksPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [view, setView] = useUrlState<TasksView>(TASKS_DEFAULTS)
  const [state, setState] = useState<State>({ status: 'loading' })
  const [draft, setDraft] = useState(view.q)
  const [creating, setCreating] = useState(false)
  const [reloadKey, reload] = useReloadKey()

  useDocumentTitle(t('tasks.title'))

  const params = useMemo(() => {
    const states: TaskState[] | undefined =
      view.state === '' || view.state === 'all' ? undefined : [view.state as TaskState]
    return {
      states,
      open: view.state === '',
      answered: view.answered === '1',
      q: view.q,
    }
  }, [view])

  // Retry re-runs the same effect rather than firing a second fetch of its own,
  // so a retried load is aborted on unmount exactly like the first.
  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTasks(params, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) {
          setState({ status: 'ready', tasks: page.tasks, total: page.total })
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [params, reloadKey])

  const submitSearch = useCallback(
    (event: React.SyntheticEvent) => {
      event.preventDefault()
      setView({ q: draft })
    },
    [draft, setView],
  )

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('tasks.title')}</h1>
        {canWrite && (
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setCreating(true)
            }}
          >
            <Icon name="plus-lg" /> {t('tasks.create')}
          </Button>
        )}
      </div>

      <div className="d-flex flex-wrap gap-2 align-items-center mb-3">
        {FILTER_CHOICES.map((choice) => (
          <Button
            key={choice === '' ? 'open' : choice}
            size="sm"
            variant={view.state === choice ? 'primary' : 'outline-secondary'}
            onClick={() => {
              setView({ state: choice })
            }}
          >
            {choice === '' && t('tasks.filters.open')}
            {choice === 'all' && t('tasks.filters.all')}
            {choice !== '' && choice !== 'all' && t(`tasks.state.${choice}`)}
          </Button>
        ))}
        <Button
          size="sm"
          variant={view.answered === '1' ? 'primary' : 'outline-secondary'}
          onClick={() => {
            setView({ answered: view.answered === '1' ? '' : '1' })
          }}
        >
          {t('tasks.filters.answered')}
        </Button>
        <Form className="ms-auto d-flex gap-2" onSubmit={submitSearch} role="search">
          <Form.Control
            size="sm"
            type="search"
            value={draft}
            placeholder={t('tasks.filters.searchPlaceholder')}
            aria-label={t('tasks.filters.search')}
            onChange={(event) => {
              setDraft(event.target.value)
            }}
          />
          <Button size="sm" variant="outline-secondary" type="submit">
            <Icon name="search" />
          </Button>
        </Form>
      </div>

      {state.status === 'loading' && (
        <div className="text-center py-5">
          <Spinner animation="border" role="status" aria-label={t('tasks.loading')} />
        </div>
      )}

      {state.status === 'error' && <ErrorState title={t('tasks.error')} onRetry={reload} />}

      {state.status === 'ready' && state.tasks.length === 0 && (
        <EmptyState title={t('tasks.empty.title')} hint={t('tasks.empty.hint')} />
      )}

      {state.status === 'ready' && state.tasks.length > 0 && (
        <ul className="list-unstyled d-flex flex-column gap-2 mb-0">
          {state.tasks.map((task) => (
            <TaskRow key={task.uid} task={task} />
          ))}
        </ul>
      )}

      <NewTaskModal
        show={creating}
        onClose={() => {
          setCreating(false)
        }}
      />
    </>
  )
}

/**
 * One line of the listing: the thumbnail of the first photograph, the question,
 * the state, and the two counts. The "answered" mark is deliberately loud — it
 * is the one thing a reader scans the page for.
 *
 * The state is on the row twice over: as the badge on the right, and as the
 * coloured stripe down the left that `data-state` picks (see `.kk-task-row` in
 * `styles/app.css`). That is what makes the queue scannable — the shape of the
 * page tells you what is waiting on you before you read a word of it.
 */
function TaskRow({ task }: { task: Task }) {
  const { t } = useTranslation()
  return (
    <li>
      <Link
        to={`/tasks/${task.uid}`}
        data-state={task.state}
        className="d-flex align-items-center gap-3 p-2 rounded text-decoration-none kk-task-row"
      >
        {task.cover_photo_uid !== undefined && task.cover_photo_uid !== '' ? (
          <img
            src={thumbUrl(task.cover_photo_uid, 'tile_100')}
            alt=""
            width={56}
            height={56}
            className="rounded flex-shrink-0 object-fit-cover"
          />
        ) : (
          <span
            className="d-flex align-items-center justify-content-center rounded flex-shrink-0 bg-body-secondary"
            style={{ width: 56, height: 56 }}
            aria-hidden="true"
          >
            <Icon name="ui-checks" />
          </span>
        )}
        <span className="kk-task-row__text">
          <span className="d-block fw-semibold text-body">{task.title}</span>
          <span className="d-block small text-body-secondary">
            {t('tasks.row.photos', { count: task.photo_count })}
            {' · '}
            {t('tasks.row.comments', { count: task.comment_count })}
          </span>
        </span>
        <span className="kk-task-row__badges d-flex align-items-center gap-2">
          {task.has_new_answer && (
            <Badge className="kk-task-answered">
              <Icon name="chat-left-text" /> {t('tasks.row.answered')}
            </Badge>
          )}
          <TaskStateBadge state={task.state} />
        </span>
      </Link>
    </li>
  )
}
