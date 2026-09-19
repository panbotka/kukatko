import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Task, type TaskPage } from '../services/tasks'

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
    ...overrides,
  }
}

/** Wraps tasks in the listing envelope the endpoint returns. */
function page(tasks: Task[]): TaskPage {
  return { tasks, total: tasks.length, limit: 50, offset: 0 }
}

function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(entry = '/tasks', canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/tasks" element={<TasksPage />} />
            <Route path="/tasks/:uid" element={<p>detail</p>} />
          </Routes>
        </MemoryRouter>
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
