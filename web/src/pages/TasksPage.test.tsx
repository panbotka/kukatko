import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Role } from '../services/auth'
import { type Task, type TaskPage, type TaskSummary } from '../services/tasks'
import { TaskSummaryContext, type TaskSummaryState } from '../tasks/TaskSummaryContext'

import { TasksPage } from './TasksPage'

vi.mock('../services/tasks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/tasks')>()
  return { ...actual, fetchTasks: vi.fn(), createTask: vi.fn() }
})

const { fetchTasks } = await import('../services/tasks')
const fetchTasksMock = vi.mocked(fetchTasks)

/** A task with everything at a sensible default, overridden per case. */
function task(overrides: Partial<Task> = {}): Task {
  return {
    uid: 'tk1',
    title: 'In which year was the house rebuilt?',
    body: '',
    state: 'question',
    resolution: '',
    query: '',
    created_by: 'u1',
    created_by_name: 'Pan Botka',
    created_at: '2026-09-18T10:00:00Z',
    updated_at: '2026-09-18T10:00:00Z',
    state_at: '2026-09-18T10:00:00Z',
    photo_count: 3,
    comment_count: 0,
    has_new_answer: false,
    last_activity_at: '2026-09-18T10:00:00Z',
    last_activity_by: 'u1',
    last_activity_by_name: 'Pan Botka',
    waiting_on_me: false,
    participants: [],
    options: [],
    ...overrides,
  }
}

/** Wraps tasks in the listing envelope the endpoint returns. */
function page(tasks: Task[]): TaskPage {
  return { tasks, total: tasks.length, limit: 50, offset: 0 }
}

/** `true` is an editor, `false` a viewer; a role name picks that rung exactly. */
function auth(who: boolean | Role): AuthContextValue {
  const role: Role = who === true ? 'editor' : who === false ? 'viewer' : who
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canCurate: role !== 'viewer',
    canWrite: role !== 'viewer' && role !== 'curator',
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The queue's counts as the shell would hold them, overridden per case. */
function summary(overrides: Partial<TaskSummary> = {}): TaskSummary {
  return {
    by_state: { question: 22, working: 3, review: 9, done: 40, rejected: 0 },
    open: 34,
    waiting_on_me: 5,
    answered: 7,
    ...overrides,
  }
}

function renderPage(entry = '/tasks', canWrite: boolean | Role = true, counts?: TaskSummaryState) {
  const page = (
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/tasks" element={<TasksPage />} />
        <Route path="/tasks/:uid" element={<p>detail</p>} />
      </Routes>
    </MemoryRouter>
  )
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        {counts === undefined ? (
          page
        ) : (
          <TaskSummaryContext.Provider value={counts}>{page}</TaskSummaryContext.Provider>
        )}
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchTasksMock.mockReset()
  fetchTasksMock.mockResolvedValue(page([task()]))
})

describe('TasksPage', () => {
  it('lists the open tasks by default', async () => {
    renderPage()

    expect(
      await screen.findByRole('link', { name: /In which year was the house rebuilt/ }),
    ).toBeInTheDocument()
    expect(screen.getByText(/3 photos/)).toBeInTheDocument()
    // The default listing is the open one: no explicit state, the open shorthand.
    expect(fetchTasksMock).toHaveBeenCalledWith(
      expect.objectContaining({ open: true, states: undefined, answered: false }),
      expect.anything(),
    )
  })

  it('marks a task somebody has answered', async () => {
    fetchTasksMock.mockResolvedValue(page([task({ has_new_answer: true, comment_count: 2 })]))
    renderPage()

    expect(await screen.findByText('New answer')).toBeInTheDocument()
    expect(screen.getByText(/2 comments/)).toBeInTheDocument()
  })

  it('narrows to one state when its chip is picked', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/3 photos/)

    await user.click(screen.getByRole('button', { name: 'Rejected' }))

    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ states: ['rejected'], open: false }),
        expect.anything(),
      )
    })
  })

  it('narrows to the answered ones', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/3 photos/)

    await user.click(screen.getByRole('button', { name: 'Answered' }))

    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ answered: true }),
        expect.anything(),
      )
    })
  })

  it('narrows to the tasks waiting on the reader', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/3 photos/)

    await user.click(screen.getByRole('button', { name: /On me/ }))

    await waitFor(() => {
      // A plain flag: the server decides whose move it is, per caller.
      expect(fetchTasksMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ waiting: true }),
        expect.anything(),
      )
    })
  })

  it('keeps "on me" in the URL and combines it with a state', async () => {
    renderPage('/tasks?waiting=1&state=working')
    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenCalledWith(
        expect.objectContaining({ waiting: true, states: ['working'] }),
        expect.anything(),
      )
    })
    expect(await screen.findByRole('button', { name: /On me/ })).toHaveClass('btn-primary')
  })

  it('says who acted last on every row', async () => {
    fetchTasksMock.mockResolvedValue(
      page([
        task({ last_activity_by: 'u2', last_activity_by_name: 'Tomáš Kozák' }),
        task({ uid: 'tk2', last_activity_by: '', last_activity_by_name: '' }),
      ]),
    )
    renderPage()

    expect(await screen.findByText(/last by Tomáš Kozák/)).toBeInTheDocument()
    // An actor whose account is gone still gets a line, not a blank.
    expect(screen.getByText(/last by someone/)).toBeInTheDocument()
    // The stamp is machine-readable as well as human-readable. Found by its
    // attribute, not by "ago": the fixture's date is fixed, so the relative
    // wording it renders in depends on the day the suite runs.
    const stamp = document.querySelector('time[datetime="2026-09-18T10:00:00Z"]')
    expect(stamp).not.toBeNull()
    expect(stamp?.textContent).not.toBe('')
  })

  it('says a task over nothing has no photos, in words and with the placeholder', async () => {
    fetchTasksMock.mockResolvedValue(
      page([
        task({
          uid: 'tk9',
          title: 'Rename the wf: labels',
          photo_count: 0,
          cover_photo_uid: undefined,
        }),
      ]),
    )
    renderPage()

    const row = (await screen.findByText('Rename the wf: labels')).closest('a')
    expect(row).not.toBeNull()
    expect(row).toHaveTextContent(/No photos/)
    expect(row).not.toHaveTextContent(/0 photos/)
    // No cover to show: the placeholder glyph stands in, never a broken image.
    expect(row?.querySelector('img')).toBeNull()
    expect(row?.querySelector('.bi-ui-checks')).not.toBeNull()
  })

  it('narrows to the questions the reader is on', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText(/3 photos/)

    await user.click(screen.getByRole('button', { name: /Mine/ }))

    await waitFor(() => {
      // "me", not the reader's own uid: the server resolves it, so the page
      // never has to learn who it is before it can ask.
      expect(fetchTasksMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ participant: 'me' }),
        expect.anything(),
      )
    })
  })

  it('keeps "mine" in the URL and combines it with a state', async () => {
    renderPage('/tasks?mine=1&state=question')
    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenCalledWith(
        expect.objectContaining({ participant: 'me', states: ['question'] }),
        expect.anything(),
      )
    })
  })

  it('reads the filter back out of the URL, so Back restores it', async () => {
    renderPage('/tasks?state=done')
    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenCalledWith(
        expect.objectContaining({ states: ['done'] }),
        expect.anything(),
      )
    })
  })

  it('offers opening a task to a writer and not to a viewer', async () => {
    const { unmount } = renderPage('/tasks', true)
    expect(await screen.findByRole('button', { name: /New task/ })).toBeInTheDocument()
    unmount()

    renderPage('/tasks', false)
    await screen.findAllByText(/3 photos/)
    expect(screen.queryByRole('button', { name: /New task/ })).not.toBeInTheDocument()
  })

  it('offers a curator no way to open a task — that stays an editor’s', async () => {
    renderPage('/tasks', 'curator')
    await screen.findAllByText(/3 photos/)
    expect(screen.queryByRole('button', { name: /New task/ })).not.toBeInTheDocument()
  })

  it('says so when there is nothing to do', async () => {
    fetchTasksMock.mockResolvedValue(page([]))
    renderPage()

    expect(await screen.findByText('No tasks')).toBeInTheDocument()
  })

  it('reports a failed load', async () => {
    fetchTasksMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('The tasks could not be loaded.')).toBeInTheDocument()
  })
})

describe('the queue tells you where you stand', () => {
  it('puts the counts on the chips and leaves a zero as a plain label', async () => {
    renderPage('/tasks', true, { summary: summary(), refresh: vi.fn() })
    await screen.findByText(/3 photos/)

    expect(screen.getByRole('button', { name: 'Open (34)' })).toBeInTheDocument()
    // Every state, whatever its own count says.
    expect(screen.getByRole('button', { name: 'All (74)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Waiting for an answer (22)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'For approval (9)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Answered (7)' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /On me \(5\)/ })).toBeInTheDocument()
    // Nothing rejected: the label alone, no "(0)".
    expect(screen.getByRole('button', { name: 'Rejected' })).toBeInTheDocument()
    // "Mine" is not in the summary and carries no number.
    expect(screen.getByRole('button', { name: /^Mine$/ })).toBeInTheDocument()
  })

  it('keeps the chips plain without the counts', async () => {
    renderPage()
    await screen.findByText(/3 photos/)

    expect(screen.getByRole('button', { name: 'Open' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /\(\d+\)/ })).not.toBeInTheDocument()
  })

  it('refreshes the counts on arrival', async () => {
    const refresh = vi.fn()
    renderPage('/tasks', true, { summary: null, refresh })
    await screen.findByText(/3 photos/)

    expect(refresh).toHaveBeenCalled()
  })

  it('asks for a page of fifty and says how many there are', async () => {
    fetchTasksMock.mockResolvedValue({ tasks: [task()], total: 34, limit: 50, offset: 0 })
    renderPage()
    await screen.findByText(/3 photos/)

    expect(fetchTasksMock).toHaveBeenCalledWith(
      expect.objectContaining({ limit: 50, offset: 0 }),
      expect.anything(),
    )
    expect(screen.getByText('34 tasks')).toBeInTheDocument()
  })

  it('loads the next page under the first and stops when everything is shown', async () => {
    const user = userEvent.setup()
    fetchTasksMock.mockImplementation((params = {}) =>
      Promise.resolve(
        params.offset === undefined || params.offset === 0
          ? { tasks: [task({ uid: 'tk1', title: 'First page' })], total: 2, limit: 50, offset: 0 }
          : { tasks: [task({ uid: 'tk2', title: 'Second page' })], total: 2, limit: 50, offset: 1 },
      ),
    )
    renderPage()
    await screen.findByText('First page')

    await user.click(screen.getByRole('button', { name: 'Load more' }))

    // Appended, not replaced — and the button is gone once the total is shown.
    expect(await screen.findByText('Second page')).toBeInTheDocument()
    expect(screen.getByText('First page')).toBeInTheDocument()
    expect(fetchTasksMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ limit: 50, offset: 1 }),
      expect.anything(),
    )
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
  })

  it('starts a new filter from the top', async () => {
    const user = userEvent.setup()
    fetchTasksMock.mockImplementation((params = {}) =>
      Promise.resolve({
        tasks: [task({ uid: `tk-${String(params.offset ?? 0)}`, title: 'Row' })],
        total: 3,
        limit: 50,
        offset: params.offset ?? 0,
      }),
    )
    renderPage()
    await screen.findByText('Row')
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    await waitFor(() => {
      expect(screen.getAllByText('Row')).toHaveLength(2)
    })

    await user.click(screen.getByRole('button', { name: 'Rejected' }))

    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ states: ['rejected'], offset: 0 }),
        expect.anything(),
      )
    })
    await waitFor(() => {
      expect(screen.getAllByText('Row')).toHaveLength(1)
    })
  })
})

describe('the state is the colour of the row', () => {
  it('stripes every row with its own state', async () => {
    // Five questions in five states is the page at its most confusing if they
    // all look alike, so each row carries its state as data — the stripe down
    // its left side is drawn from it (`.kk-task-row[data-state=…]`).
    fetchTasksMock.mockResolvedValue(
      page([
        task({ uid: 'tk1', title: 'Waiting', state: 'question' }),
        task({ uid: 'tk2', title: 'Started', state: 'working' }),
        task({ uid: 'tk3', title: 'Checking', state: 'review' }),
        task({ uid: 'tk4', title: 'Finished', state: 'done' }),
        task({ uid: 'tk5', title: 'Refused', state: 'rejected' }),
      ]),
    )
    renderPage()

    await screen.findByText('Waiting')
    const rows = document.querySelectorAll<HTMLElement>('.kk-task-row')
    expect([...rows].map((row) => row.dataset.state)).toEqual([
      'question',
      'working',
      'review',
      'done',
      'rejected',
    ])
  })

  it('paints each badge in its own state class', async () => {
    fetchTasksMock.mockResolvedValue(
      page([
        task({ uid: 'tk1', title: 'Waiting', state: 'question' }),
        task({ uid: 'tk2', title: 'Finished', state: 'done' }),
      ]),
    )
    renderPage()

    await screen.findByText('Waiting')
    // Scoped to the rows: the filter chips above them carry the same words.
    const badge = (uid: string) => document.querySelector(`a[href="/tasks/${uid}"] .kk-task-state`)
    expect(badge('tk1')).toHaveClass('kk-task-state--question')
    expect(badge('tk2')).toHaveClass('kk-task-state--done')
  })

  it('gives "somebody answered" the accent rather than a sixth state hue', async () => {
    fetchTasksMock.mockResolvedValue(page([task({ has_new_answer: true })]))
    renderPage()

    // It sits next to a state badge, so it must not read as another state.
    expect(await screen.findByText('New answer')).toHaveClass('kk-task-answered')
  })
})
