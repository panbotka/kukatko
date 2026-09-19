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

/** One catalogued photograph, enough for the wall to render a row. */
function gridPhoto(uid: string) {
  return {
    uid,
    file_hash: uid,
    file_name: `${uid}.jpg`,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 100,
    file_height: 100,
    taken_at_source: 'exif',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/** The `minHeight` the page handed the wall, read off the mocked Virtuoso. */
function gridMinHeight(): string | undefined {
  // Scoped to the wall itself: the filter bar mounts a list of its own.
  const el = document.querySelector<HTMLElement>('.kukatko-photo-grid [data-testid="grid"]')
  return el?.style.minHeight
}

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
      // The card saves its three bookkeeping fields together; the source query
      // rides along unchanged, which the server diffs away.
      expect(updateTaskMock).toHaveBeenCalledWith('tk1', {
        state: 'done',
        resolution: '1987',
        query: 'camera:Olympus',
      })
    })
  })

  it('shows a closed task how it ended', async () => {
    fetchTaskMock.mockResolvedValue(
      task({ state: 'rejected', resolution: 'The year cannot be established.' }),
    )
    renderPage(false)

    expect(await screen.findByText('How it ended')).toBeInTheDocument()
    expect(screen.getByText('The year cannot be established.')).toBeInTheDocument()
    // The filter bar's own "Rejected" flag option carries the same word.
    expect(screen.getAllByText('Rejected').length).toBeGreaterThan(0)
  })

  it('says so when the task has been deleted since the link was sent', async () => {
    fetchTaskMock.mockRejectedValue(new ApiError(404, 'task not found'))
    renderPage()

    expect(await screen.findByText('This task does not exist')).toBeInTheDocument()
  })
})

describe('the question block', () => {
  it('puts the byline above the question, not under it', async () => {
    renderPage()

    const heading = await screen.findByRole('heading', { name: /In which year/ })
    const byline = screen.getByText(/Opened by Pan Botka/)
    // The byline is the eyebrow of the page: it says where the question came
    // from, which is worth knowing before reading it and never worth reading
    // first. So it precedes the heading in the document.
    expect(byline.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('rewords the question in place, where the question is', async () => {
    const user = userEvent.setup()
    updateTaskMock.mockResolvedValue(task({ title: 'Which year exactly?', body: 'Two of them.' }))
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Edit the question/ }))

    // The fields open beside the heading they belong to — not in the card at
    // the foot of the page, a whole conversation away from the words.
    const title = screen.getByLabelText('Question')
    const body = screen.getByLabelText('Context')
    expect(title).toHaveValue('In which year was the house rebuilt?')
    expect(body).toHaveValue('Three photos of the rebuilding.')

    await user.clear(title)
    await user.type(title, 'Which year exactly?')
    await user.clear(body)
    await user.type(body, 'Two of them.')
    // The editor's own Save, not the bookkeeping card's.
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0])

    await waitFor(() => {
      expect(updateTaskMock).toHaveBeenCalledWith('tk1', {
        title: 'Which year exactly?',
        body: 'Two of them.',
      })
    })
    // A landed save closes the editor and the new wording stands as the heading.
    expect(await screen.findByRole('heading', { name: 'Which year exactly?' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Question')).not.toBeInTheDocument()
  })

  it('keeps the editor open and says so when the save did not land', async () => {
    const user = userEvent.setup()
    updateTaskMock.mockRejectedValue(new Error('nope'))
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Edit the question/ }))
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0])

    expect(await screen.findByText('Saving failed.')).toBeInTheDocument()
    expect(screen.getByLabelText('Question')).toBeInTheDocument()
  })

  it('offers a viewer no way to reword it', async () => {
    renderPage(false)
    await screen.findByRole('heading', { name: /In which year/ })

    expect(screen.queryByRole('button', { name: /Edit the question/ })).not.toBeInTheDocument()
  })
})

describe('the discussion', () => {
  it('stands on a surface of its own, apart from the photographs', async () => {
    renderPage()

    const heading = await screen.findByRole('heading', { name: 'Discussion' })
    // A card, not a run of text under the wall: the conversation is the page's
    // other half and is bounded like one.
    const card = heading.closest('.card')
    expect(card).not.toBeNull()
    expect(card).toContainElement(screen.getByLabelText('New comment'))
  })
})

describe('the photo wall', () => {
  it('reserves no empty height, so the discussion follows the photographs', async () => {
    fetchPhotosMock.mockResolvedValue({
      photos: [gridPhoto('ph1')],
      total: 1,
      limit: 100,
      offset: 0,
      next_offset: null,
    })
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    // The library's half-viewport reserve keeps a page whose whole content is
    // the grid from collapsing; here the grid is a section between the question
    // and the answer box, where that reserve is a hole.
    expect(gridMinHeight()).toBe('0')
  })
})

describe('the wall behaves like every other scoped list', () => {
  it('offers the filter bar and round-trips its view through the URL', async () => {
    const user = userEvent.setup()
    fetchPhotosMock.mockResolvedValue({
      photos: [gridPhoto('ph1')],
      total: 1,
      limit: 100,
      offset: 0,
      next_offset: null,
    })
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    await user.click(screen.getByRole('button', { name: /Filters/i }))
    expect(await screen.findByLabelText('Sort')).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Sort'), 'oldest')

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenLastCalledWith(
        // The task scope survives the filter change: a filter narrows what of
        // the frozen group is on screen, never what the group is.
        expect.objectContaining({ task: 'tk1', sort: 'oldest' }),
        expect.anything(),
      )
    })
  })
})
