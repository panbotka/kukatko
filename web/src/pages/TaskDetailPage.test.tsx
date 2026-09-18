import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ApiError } from '../services/auth'
import i18n from '../i18n'
import { type Task } from '../services/tasks'

import { TaskDetailPage } from './TaskDetailPage'

// jsdom lays nothing out, so the real virtualizer mounts nothing: render it all
// instead (see `test/virtuoso`).
vi.mock('react-virtuoso', async () => (await import('../test/virtuoso')).virtuosoMock())

vi.mock('../services/tasks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/tasks')>()
  return { ...actual, fetchTask: vi.fn(), updateTask: vi.fn(), deleteTask: vi.fn() }
})

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

vi.mock('../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/comments')>()
  return { ...actual, fetchComments: vi.fn(), createComment: vi.fn() }
})

const { fetchTask, updateTask } = await import('../services/tasks')
const { fetchPhotos } = await import('../services/photos')
const { fetchComments } = await import('../services/comments')
const fetchTaskMock = vi.mocked(fetchTask)
const updateTaskMock = vi.mocked(updateTask)
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchCommentsMock = vi.mocked(fetchComments)

/** A task with everything at a sensible default, overridden per case. */
function task(overrides: Partial<Task> = {}): Task {
  return {
    uid: 'tk1',
    title: 'In which year was the house rebuilt?',
    body: 'Three photos of the rebuilding.',
    state: 'question',
    resolution: '',
    query: 'camera:Olympus',
    created_by: 'u1',
    created_by_name: 'Pan Botka',
    created_at: '2026-09-18T10:00:00Z',
    updated_at: '2026-09-18T10:00:00Z',
    state_at: '2026-09-18T10:00:00Z',
    photo_count: 0,
    comment_count: 0,
    has_new_answer: false,
    ...overrides,
  }
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

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={['/tasks/tk1']}>
          <Routes>
            <Route path="/tasks/:uid" element={<TaskDetailPage />} />
            <Route path="/tasks" element={<p>list</p>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchTaskMock.mockReset()
  updateTaskMock.mockReset()
  fetchPhotosMock.mockReset()
  fetchCommentsMock.mockReset()
  fetchTaskMock.mockResolvedValue(task())
  fetchPhotosMock.mockResolvedValue({
    photos: [],
    total: 0,
    limit: 100,
    offset: 0,
    next_offset: null,
  })
  fetchCommentsMock.mockResolvedValue([])
})

describe('TaskDetailPage', () => {
  it('leads with the question, because that is what the link was sent for', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'In which year was the house rebuilt?' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Three photos of the rebuilding.')).toBeInTheDocument()
    // The badge and the state select's option both carry the name.
    expect(screen.getAllByText('Waiting for an answer').length).toBeGreaterThan(0)
  })

  it('reads the photos through the catalogue, scoped to the task', async () => {
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalledWith(
        expect.objectContaining({ task: 'tk1' }),
        expect.anything(),
      )
    })
  })

  it('gives a viewer the answer box and none of the curation controls', async () => {
    renderPage(false)
    await screen.findByRole('heading', { name: /In which year/ })

    expect(screen.getByText('Discussion')).toBeInTheDocument()
    expect(screen.queryByText('Manage the task')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Delete the task/ })).not.toBeInTheDocument()
  })

  it('gives a writer the state control', async () => {
    renderPage()

    expect(await screen.findByText('Manage the task')).toBeInTheDocument()
    expect(screen.getByLabelText('State')).toHaveValue('question')
  })

  it('asks for a resolution as soon as a closing state is picked', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('Manage the task')

    expect(screen.queryByLabelText('How it ended')).not.toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('State'), 'done')

    const resolution = await screen.findByLabelText('How it ended')
    expect(resolution).toBeInTheDocument()
    // Saving stays out of reach until the reason is written.
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.type(resolution, '1987')
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('saves the state and the resolution together', async () => {
    const user = userEvent.setup()
    updateTaskMock.mockResolvedValue(task({ state: 'done', resolution: '1987' }))
    renderPage()
    await screen.findByText('Manage the task')

    await user.selectOptions(screen.getByLabelText('State'), 'done')
    await user.type(await screen.findByLabelText('How it ended'), '1987')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateTaskMock).toHaveBeenCalledWith('tk1', { state: 'done', resolution: '1987' })
    })
  })

  it('shows a closed task how it ended', async () => {
    fetchTaskMock.mockResolvedValue(
      task({ state: 'rejected', resolution: 'The year cannot be established.' }),
    )
    renderPage(false)

    expect(await screen.findByText('How it ended')).toBeInTheDocument()
    expect(screen.getByText('The year cannot be established.')).toBeInTheDocument()
    expect(screen.getByText('Rejected')).toBeInTheDocument()
  })

  it('says so when the task has been deleted since the link was sent', async () => {
    fetchTaskMock.mockRejectedValue(new ApiError(404, 'task not found'))
    renderPage()

    expect(await screen.findByText('This task does not exist')).toBeInTheDocument()
  })
})
