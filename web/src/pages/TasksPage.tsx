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
import { formatRelativeTime } from '../lib/relativeTime'
import { useUrlState } from '../lib/urlState'
import { thumbUrl } from '../services/photos'
import {
  fetchTasks,
  type Task,
  type TaskListParams,
  TASK_STATES,
  type TaskState,
  type TaskSummary,
} from '../services/tasks'
import { useTaskSummary } from '../tasks/TaskSummaryContext'

/**
 * How many rows one request brings. The server's own default is the same, but
 * the page states it: "load more" appends the next page from `offset`, and a
 * page size the client did not choose is a page size it cannot reason about.
 */
export const PAGE_SIZE = 50

/**
 * Fetch lifecycle of the listing. `more` is the state of a "load more" request
 * appending to an already shown page; a failed one keeps the rows on screen and
 * offers the button again rather than replacing the list with an error.
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; tasks: Task[]; total: number; more: 'idle' | 'loading' | 'error' }

/**
 * Where the listing currently reads from. The offset belongs to one filter and
 * one reload: it is remembered together with the params it was chosen under,
 * so a new filter — or a retry — starts from the top without an effect having
 * to reset it (and without a wasted request at the old offset).
 */
interface Paging {
  params: TaskListParams
  reloadKey: string
  offset: number
}

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
  waiting: string
  mine: string
  q: string
}

const TASKS_DEFAULTS: TasksView = { state: '', answered: '', waiting: '', mine: '', q: '' }

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
  // The counts on the chips. Refreshed on arrival so the numbers are the
  // queue's now, not the shell's last poll — and they refresh themselves after
  // every write made from this browser.
  const { summary, refresh: refreshSummary } = useTaskSummary()

  useDocumentTitle(t('tasks.title'))

  useEffect(() => {
    refreshSummary()
  }, [refreshSummary])

  const params = useMemo<TaskListParams>(() => {
    const states: TaskState[] | undefined =
      view.state === '' || view.state === 'all' ? undefined : [view.state as TaskState]
    return {
      states,
      open: view.state === '',
      answered: view.answered === '1',
      waiting: view.waiting === '1',
      // "me" rather than the reader's own uid: the server resolves it, so the
      // page never has to learn who it is before it can ask.
      participant: view.mine === '1' ? 'me' : '',
      q: view.q,
    }
  }, [view])

  // The offset is not in the URL on purpose: a reload starts from the top, and
  // Back restores the filter, which is what the reader chose — not how far down
  // they had scrolled.
  const [paging, setPaging] = useState<Paging>({ params, reloadKey, offset: 0 })
  const offset = paging.params === params && paging.reloadKey === reloadKey ? paging.offset : 0

  // Retry re-runs the same effect rather than firing a second fetch of its own,
  // so a retried load is aborted on unmount exactly like the first. A "load
  // more" is the same effect at a later offset, appending instead of replacing.
  useEffect(() => {
    const controller = new AbortController()
    const appending = offset > 0
    setState((prev) =>
      appending && prev.status === 'ready' ? { ...prev, more: 'loading' } : { status: 'loading' },
    )
    fetchTasks({ ...params, limit: PAGE_SIZE, offset }, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) {
          setState((prev) => ({
            status: 'ready',
            tasks:
              appending && prev.status === 'ready' ? [...prev.tasks, ...page.tasks] : page.tasks,
            total: page.total,
            more: 'idle',
          }))
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState((prev) =>
            appending && prev.status === 'ready' ? { ...prev, more: 'error' } : { status: 'error' },
          )
        }
      })
    return () => {
      controller.abort()
    }
  }, [params, reloadKey, offset])

  /** Puts a count after a chip's label, or leaves the label alone for zero. */
  function chip(label: string, count: number | undefined): string {
    return count !== undefined && count > 0 ? t('tasks.chipCount', { label, count }) : label
  }

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
            {choice === '' && chip(t('tasks.filters.open'), summary?.open)}
            {choice === 'all' && chip(t('tasks.filters.all'), totalOf(summary))}
            {choice !== '' &&
              choice !== 'all' &&
              chip(t(`tasks.state.${choice}`), summary?.by_state[choice])}
          </Button>
        ))}
        <Button
          size="sm"
          variant={view.answered === '1' ? 'primary' : 'outline-secondary'}
          onClick={() => {
            setView({ answered: view.answered === '1' ? '' : '1' })
          }}
        >
          {chip(t('tasks.filters.answered'), summary?.answered)}
        </Button>
        {/* "Whose move is it?" — the tasks that are open, have the reader on
            them, and where somebody else acted last. The server evaluates it
            per caller (`waiting=1`), so the page never has to reason about who
            wrote what. */}
        <Button
          size="sm"
          variant={view.waiting === '1' ? 'primary' : 'outline-secondary'}
          onClick={() => {
            setView({ waiting: view.waiting === '1' ? '' : '1' })
          }}
        >
          <Icon name="hourglass-split" /> {chip(t('tasks.filters.waiting'), summary?.waiting_on_me)}
        </Button>
        {/* "What am I on?" — the questions the reader opened, answered, moved
            along or was put on. It narrows whatever state filter is already
            chosen rather than replacing it, so "mine, still open" is one click
            from "everything open". */}
        <Button
          size="sm"
          variant={view.mine === '1' ? 'primary' : 'outline-secondary'}
          onClick={() => {
            setView({ mine: view.mine === '1' ? '' : '1' })
          }}
        >
          <Icon name="person-circle" /> {t('tasks.filters.mine')}
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
        <>
          {/* How many match — the whole queue, not the page — so a reader who
              sees fifty rows knows there are thirty-four more behind the button. */}
          <p className="small text-body-secondary mb-2">
            {t('tasks.count', { count: state.total })}
          </p>
          <ul className="list-unstyled d-flex flex-column gap-2 mb-0">
            {state.tasks.map((task) => (
              <TaskRow key={task.uid} task={task} />
            ))}
          </ul>
          {state.tasks.length < state.total && (
            <div className="text-center mt-3">
              {state.more === 'error' && (
                <p className="small text-danger mb-2">{t('tasks.error')}</p>
              )}
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={state.more === 'loading'}
                onClick={() => {
                  setPaging({ params, reloadKey, offset: state.tasks.length })
                }}
              >
                {state.more === 'loading' && (
                  <Spinner animation="border" size="sm" className="me-2" aria-hidden="true" />
                )}
                {t('tasks.loadMore')}
              </Button>
            </div>
          )}
        </>
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

/** Every task there is, whatever its state — the number behind „Všechny". */
function totalOf(summary: TaskSummary | null): number | undefined {
  if (summary === null) {
    return undefined
  }
  return Object.values(summary.by_state).reduce((sum, n) => sum + n, 0)
}

/**
 * One line of the listing: the thumbnail of the first photograph, the question,
 * the state, the two counts and who acted last. The "answered" mark is
 * deliberately loud — it is the one thing a reader scans the page for — and the
 * last activity ("naposledy Tomáš Kozák · před 2 h") says whose move it is
 * without opening the task.
 *
 * The state is on the row twice over: as the badge on the right, and as the
 * coloured stripe down the left that `data-state` picks (see `.kk-task-row` in
 * `styles/app.css`). That is what makes the queue scannable — the shape of the
 * page tells you what is waiting on you before you read a word of it.
 */
function TaskRow({ task }: { task: Task }) {
  const { t, i18n } = useTranslation()
  const lastBy =
    task.last_activity_by_name !== '' ? task.last_activity_by_name : t('taskDetail.someone')
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
            {task.photo_count === 0
              ? t('tasks.row.noPhotos')
              : t('tasks.row.photos', { count: task.photo_count })}
            {' · '}
            {t('tasks.row.comments', { count: task.comment_count })}
            {' · '}
            {t('tasks.row.lastActivity', { name: lastBy })}
            {' · '}
            <time dateTime={task.last_activity_at}>
              {formatRelativeTime(task.last_activity_at, i18n.language)}
            </time>
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
