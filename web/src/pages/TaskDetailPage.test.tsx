import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ApiError } from '../services/auth'
import i18n from '../i18n'
import { type Task } from '../services/tasks'

import { SMALL_GROUP_MAX, TaskDetailPage } from './TaskDetailPage'

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
  return {
    ...actual,
    fetchTask: vi.fn(),
    updateTask: vi.fn(),
    deleteTask: vi.fn(),
    assignTaskParticipant: vi.fn(),
    unassignTaskParticipant: vi.fn(),
    removeTaskPhotos: vi.fn(),
  }
})

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

vi.mock('../services/directory', () => ({
  listDirectory: vi.fn(),
}))

vi.mock('../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/comments')>()
  return { ...actual, fetchComments: vi.fn(), createComment: vi.fn() }
})

const { fetchTask, updateTask, assignTaskParticipant, unassignTaskParticipant, removeTaskPhotos } =
  await import('../services/tasks')
const removeTaskPhotosMock = vi.mocked(removeTaskPhotos)
const { listDirectory } = await import('../services/directory')
const listDirectoryMock = vi.mocked(listDirectory)
const assignMock = vi.mocked(assignTaskParticipant)
const unassignMock = vi.mocked(unassignTaskParticipant)
const { fetchPhotos } = await import('../services/photos')
const { fetchComments, createComment } = await import('../services/comments')
const createCommentMock = vi.mocked(createComment)
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

/** Where the router is, readable off the document: the URL is view state here. */
function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname + location.search}</output>
}

/** The URL the page currently sits at. */
function currentLocation(): string {
  return screen.getByTestId('location').textContent
}

function renderPage(canWrite = true, initialEntry = '/tasks/tk1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route path="/tasks/:uid" element={<TaskDetailPage />} />
            <Route path="/tasks" element={<p>list</p>} />
            <Route path="/photos/:uid" element={<p>viewer</p>} />
          </Routes>
          <LocationProbe />
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
  createCommentMock.mockReset()
  listDirectoryMock.mockReset()
  assignMock.mockReset()
  unassignMock.mockReset()
  listDirectoryMock.mockResolvedValue([])
  removeTaskPhotosMock.mockReset()
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

describe('who is on the task', () => {
  /** A participant who joined by acting on the task. */
  const acted = { user_uid: 'u2', name: 'Anna', joined_at: '2026-09-18T10:00:00Z' }

  it('names the people on it, and says who was asked rather than turned up', async () => {
    fetchTaskMock.mockResolvedValue(
      task({
        participants: [
          acted,
          {
            user_uid: 'u3',
            name: 'Teta',
            joined_at: '2026-09-18T11:00:00Z',
            added_by: 'u1',
            added_by_name: 'Pan Botka',
          },
        ],
      }),
    )
    renderPage()

    expect(await screen.findByText('Anna')).toBeInTheDocument()
    // The distinction is in the title, not a second visual language: both are
    // the same fact arrived at two ways.
    expect(screen.getByTitle(/Anna — took part/)).toBeInTheDocument()
    expect(screen.getByTitle(/Teta — added by Pan Botka/)).toBeInTheDocument()
  })

  it('says so when nobody is on it yet', async () => {
    renderPage()
    expect(await screen.findByText('nobody yet')).toBeInTheDocument()
  })

  it('lets a writer put somebody on it, from the people of the library', async () => {
    const user = userEvent.setup()
    listDirectoryMock.mockResolvedValue([
      { uid: 'u2', name: 'Anna' },
      { uid: 'u3', name: 'Teta' },
    ])
    assignMock.mockResolvedValue([acted])
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Add/ }))
    await user.click(await screen.findByRole('button', { name: 'Anna' }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('tk1', 'u2')
    })
    // The reply redraws the row, so no refetch of the task is needed.
    expect(await screen.findByText('Anna')).toBeInTheDocument()
  })

  it('leaves out the people already on it', async () => {
    const user = userEvent.setup()
    fetchTaskMock.mockResolvedValue(task({ participants: [acted] }))
    listDirectoryMock.mockResolvedValue([
      { uid: 'u2', name: 'Anna' },
      { uid: 'u3', name: 'Teta' },
    ])
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Add/ }))

    expect(await screen.findByRole('button', { name: 'Teta' })).toBeInTheDocument()
    // Anna is on the task, so she is a chip in the row and not a choice in the
    // dialog — the only "Anna" on screen is the chip.
    expect(screen.queryByRole('button', { name: 'Anna' })).not.toBeInTheDocument()
  })

  it('lets a writer take somebody off it', async () => {
    const user = userEvent.setup()
    fetchTaskMock.mockResolvedValue(task({ participants: [acted] }))
    unassignMock.mockResolvedValue([])
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Remove Anna' }))

    await waitFor(() => {
      expect(unassignMock).toHaveBeenCalledWith('tk1', 'u2')
    })
    expect(await screen.findByText('nobody yet')).toBeInTheDocument()
  })

  it('offers a viewer neither control', async () => {
    fetchTaskMock.mockResolvedValue(task({ participants: [acted] }))
    renderPage(false)

    expect(await screen.findByText('Anna')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Remove Anna/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Add$/ })).not.toBeInTheDocument()
  })
})

describe('taking photographs out of the group', () => {
  /** The wall with one tile in it, selectable by a writer. */
  function withOnePhoto() {
    fetchPhotosMock.mockResolvedValue({
      photos: [gridPhoto('ph1')],
      total: 1,
      limit: 100,
      offset: 0,
      next_offset: null,
    })
  }

  it('offers removal in the shared batch bar, not on a toolbar of its own', async () => {
    const user = userEvent.setup()
    withOnePhoto()
    removeTaskPhotosMock.mockResolvedValue({ changed: 1, task: task() })
    renderPage()

    await screen.findByRole('heading', { name: /In which year/ })
    await user.click(await screen.findByRole('button', { name: /^Select / }))

    // The task's own action sits among the library's shared vocabulary.
    const remove = await screen.findByRole('button', { name: 'Remove from the task' })
    expect(screen.getByRole('toolbar')).toContainElement(remove)

    await user.click(remove)
    await waitFor(() => {
      expect(removeTaskPhotosMock).toHaveBeenCalledWith('tk1', ['ph1'])
    })
  })

  it('says so when the removal did not land, instead of looking unclicked', async () => {
    const user = userEvent.setup()
    withOnePhoto()
    removeTaskPhotosMock.mockRejectedValue(new Error('nope'))
    renderPage()

    await screen.findByRole('heading', { name: /In which year/ })
    await user.click(await screen.findByRole('button', { name: /^Select / }))
    await user.click(await screen.findByRole('button', { name: 'Remove from the task' }))

    expect(
      await screen.findByText('The photos could not be removed from the task.'),
    ).toBeInTheDocument()
  })

  it('lets a viewer select and put the photos into the discussion, and nothing else', async () => {
    const user = userEvent.setup()
    withOnePhoto()
    createCommentMock.mockResolvedValue({
      uid: 'cm1',
      task_uid: 'tk1',
      author_uid: 'u1',
      author_name: 'U',
      body: 'ph1',
      created_at: '2026-09-20T10:00:00Z',
    })
    renderPage(false)

    await screen.findByRole('heading', { name: /In which year/ })
    // Commenting is open to a viewer, so pointing at pictures from the
    // discussion must be too: the tiles select for them.
    await user.click(await screen.findByRole('button', { name: /^Select / }))

    const bar = await screen.findByRole('toolbar')
    expect(within(bar).getByRole('button', { name: 'To the discussion' })).toBeInTheDocument()
    // The writer-only actions — the page's own removal included — are not there.
    expect(within(bar).queryByRole('button', { name: 'Remove from the task' })).toBeNull()
    expect(within(bar).queryByRole('button', { name: 'Archive' })).toBeNull()
    expect(within(bar).queryByRole('button', { name: 'Add to album' })).toBeNull()

    await user.click(within(bar).getByRole('button', { name: 'To the discussion' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith({ kind: 'task', uid: 'tk1' }, 'ph1')
    })
    // The thread is asked to refetch, as after a quick answer.
    await waitFor(() => {
      expect(fetchCommentsMock).toHaveBeenCalledTimes(2)
    })
    // The selection is spent, so the bar goes.
    await waitFor(() => {
      expect(screen.queryByRole('toolbar')).toBeNull()
    })
  })

  it('offers a writer the discussion beside the removal', async () => {
    const user = userEvent.setup()
    withOnePhoto()
    renderPage()

    await screen.findByRole('heading', { name: /In which year/ })
    await user.click(await screen.findByRole('button', { name: /^Select / }))

    const bar = await screen.findByRole('toolbar')
    expect(within(bar).getByRole('button', { name: 'To the discussion' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Remove from the task' })).toBeInTheDocument()
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
    // The card names the section, so the panel does not name it again.
    expect(screen.queryByText('Comments')).not.toBeInTheDocument()
  })

  it("carries the thread's length in the section heading", async () => {
    fetchCommentsMock.mockResolvedValue([
      {
        uid: 'cm1',
        task_uid: 'tk1',
        author_uid: 'u2',
        author_name: 'Pamětník',
        body: '1987.',
        created_at: '2026-09-18T11:00:00Z',
      },
    ])
    renderPage()

    expect(await screen.findByText('1 comment')).toBeInTheDocument()
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
    // A batch, not a two-photo question: a small group drops the bar.
    fetchTaskMock.mockResolvedValue(task({ photo_count: 40 }))
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

describe('quick answers', () => {
  /** One live comment on the task, as the thread serves it. */
  function comment(uid: string, author: string, body: string) {
    return {
      uid,
      task_uid: 'tk1',
      author_uid: author,
      author_name: author,
      body,
      created_at: '2026-09-18T11:00:00Z',
    }
  }

  it('draws the options as buttons under the question, above the people', async () => {
    fetchTaskMock.mockResolvedValue(task({ options: ['1936', '1938', 'nevím'] }))
    renderPage(false)

    const heading = await screen.findByRole('heading', { name: /In which year/ })
    const first = screen.getByRole('button', { name: '1936' })
    const people = screen.getByText('On this task')
    expect(screen.getByRole('button', { name: '1938' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'nevím' })).toBeInTheDocument()
    // Question, then the answers, then who is on it — the choice sits where the
    // question is, not at the foot of the page past the photographs.
    expect(heading.compareDocumentPosition(first) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(first.compareDocumentPosition(people) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    // Large enough for a thumb, and nothing is chosen yet.
    expect(first).toHaveClass('btn-lg', 'btn-outline-primary')
    expect(first).toHaveAttribute('aria-pressed', 'false')
  })

  it('posts the option text as a comment and marks it chosen', async () => {
    const user = userEvent.setup()
    fetchTaskMock.mockResolvedValue(task({ options: ['1936', '1938'] }))
    const posted = comment('cm1', 'u1', '1938')
    createCommentMock.mockResolvedValue(posted)
    renderPage()
    await screen.findByRole('button', { name: '1938' })

    // Once the answer is in, the thread is fetched again and carries it.
    fetchCommentsMock.mockResolvedValue([posted])
    await user.click(screen.getByRole('button', { name: '1938' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith({ kind: 'task', uid: 'tk1' }, '1938')
    })
    const chosen = await screen.findByRole('button', { name: '1938', pressed: true })
    expect(chosen).toHaveClass('btn-primary')
    expect(screen.getByRole('button', { name: '1936' })).toHaveClass('btn-outline-primary')
    // The thread was refreshed rather than left as it was.
    expect(fetchCommentsMock.mock.calls.length).toBeGreaterThan(1)

    // Tapping the chosen answer again posts nothing: it is already in the thread.
    await user.click(chosen)
    expect(createCommentMock).toHaveBeenCalledTimes(1)
  })

  it("reads the chosen answer off the reader's latest comment, not anybody's", async () => {
    fetchTaskMock.mockResolvedValue(task({ options: ['1936', '1938'] }))
    fetchCommentsMock.mockResolvedValue([
      comment('cm1', 'u2', '1936'),
      comment('cm2', 'u1', '1938'),
      comment('cm3', 'u1', 'nebo spíš 1938, podle kroniky'),
    ])
    renderPage()

    await screen.findByRole('button', { name: '1938' })
    await waitFor(() => {
      // The reader's *latest* comment is free text, so nothing is chosen — and
      // Anna's 1936 is hers, not the reader's.
      expect(screen.getByRole('button', { name: '1936' })).toHaveAttribute('aria-pressed', 'false')
      expect(screen.getByRole('button', { name: '1938' })).toHaveAttribute('aria-pressed', 'false')
    })
  })

  it('gives a review with no options the built-in pair, posting the fixed strings', async () => {
    const user = userEvent.setup()
    fetchTaskMock.mockResolvedValue(task({ state: 'review' }))
    createCommentMock.mockResolvedValue(comment('cm1', 'u1', 'Schvaluji.'))
    renderPage(false)

    await user.click(await screen.findByRole('button', { name: 'Approve' }))
    await waitFor(() => {
      // Localised label, fixed Czech body: the agent matches the string, and it
      // cannot know which language the person read the page in.
      expect(createCommentMock).toHaveBeenCalledWith({ kind: 'task', uid: 'tk1' }, 'Schvaluji.')
    })

    createCommentMock.mockResolvedValue(comment('cm2', 'u1', 'Vrátit k přepracování.'))
    await user.click(screen.getByRole('button', { name: 'Send back' }))
    await waitFor(() => {
      expect(createCommentMock).toHaveBeenLastCalledWith(
        { kind: 'task', uid: 'tk1' },
        'Vrátit k přepracování.',
      )
    })
  })

  it('lets a review with its own options keep them', async () => {
    fetchTaskMock.mockResolvedValue(task({ state: 'review', options: ['1936', '1938'] }))
    renderPage()

    expect(await screen.findByRole('button', { name: '1936' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument()
  })

  it('offers no buttons for a free-text question', async () => {
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    expect(screen.queryByText('Answer with one tap')).not.toBeInTheDocument()
  })

  it('says so when the answer did not post, and stays usable', async () => {
    const user = userEvent.setup()
    fetchTaskMock.mockResolvedValue(task({ options: ['1936', '1938'] }))
    createCommentMock.mockRejectedValue(new ApiError(500, 'boom'))
    renderPage()

    await user.click(await screen.findByRole('button', { name: '1936' }))

    expect(await screen.findByText(/The answer could not be posted/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1936' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '1936' })).toHaveAttribute('aria-pressed', 'false')
  })
})

describe('a task over nothing', () => {
  it('draws one muted line instead of a wall, and fetches no photos', async () => {
    fetchTaskMock.mockResolvedValue(task({ photo_count: 0 }))
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    // The byline says it and so does the line where the wall would be.
    expect(screen.getAllByText('No photos').length).toBeGreaterThan(0)
    expect(screen.queryByText('0 photos')).not.toBeInTheDocument()
    expect(document.querySelector('.kukatko-photo-grid')).not.toBeInTheDocument()
    expect(screen.queryByRole('search')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Filters/ })).not.toBeInTheDocument()
    // The "nothing matched" hint is for a filter that narrowed a group away,
    // not for a group that was never there.
    expect(
      screen.queryByText('No photos have been added to this task yet.'),
    ).not.toBeInTheDocument()
    expect(fetchPhotosMock).not.toHaveBeenCalled()
    // Nothing to select means nothing to take out of the task.
    expect(screen.queryByRole('button', { name: 'Remove from the task' })).not.toBeInTheDocument()
  })
})

describe('the filter bar on a small group', () => {
  it('is left out for a two-photo question', async () => {
    fetchTaskMock.mockResolvedValue(task({ photo_count: 2 }))
    fetchPhotosMock.mockResolvedValue({
      photos: [gridPhoto('ph1'), gridPhoto('ph2')],
      total: 2,
      limit: 100,
      offset: 0,
      next_offset: null,
    })
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    await waitFor(() => {
      expect(document.querySelector('.kukatko-photo-grid')).toBeInTheDocument()
    })
    expect(screen.queryByRole('search')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Filters/ })).not.toBeInTheDocument()
    // The count stays: the byline says it and so does the line over the wall.
    expect(screen.getAllByText('2 photos').length).toBeGreaterThan(0)
  })

  it('stays for a batch of forty', async () => {
    fetchTaskMock.mockResolvedValue(task({ photo_count: 40 }))
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    expect(screen.getByRole('search')).toBeInTheDocument()
  })
})

describe('the review ledger', () => {
  /** The ledger row a title link sits in. */
  function rowOf(link: HTMLElement | undefined): HTMLElement {
    const row = link?.closest<HTMLElement>('.kk-ledger-row')
    if (row === null || row === undefined) {
      throw new Error('the link is not in a ledger row')
    }
    return row
  }

  /** A dated, commented photograph: what a reviewed batch's rows are made of. */
  function ledgerPhoto(
    uid: string,
    overrides: Partial<ReturnType<typeof gridPhoto>> & {
      taken_at?: string
      taken_at_precision?: string
      taken_at_estimated?: boolean
      last_comment?: {
        uid: string
        body: string
        author_uid: string
        author_name: string
        created_at: string
      } | null
    } = {},
  ) {
    return { ...gridPhoto(uid), title: `Photo ${uid}`, ...overrides }
  }

  /** A group of `count` photographs, so the page draws a wall or a ledger. */
  function withPhotos(photos: ReturnType<typeof ledgerPhoto>[]) {
    fetchTaskMock.mockResolvedValue(task({ photo_count: photos.length }))
    fetchPhotosMock.mockResolvedValue({
      photos,
      total: photos.length,
      limit: 100,
      offset: 0,
      next_offset: null,
    })
  }

  it('offers the toggle and keeps the choice in the URL, so Back restores it', async () => {
    const user = userEvent.setup()
    withPhotos([ledgerPhoto('ph1')])
    renderPage()
    await screen.findByRole('heading', { name: /In which year/ })

    // The wall by default: tiles, no rows.
    expect(await screen.findByRole('link', { name: 'Photo ph1' })).toBeInTheDocument()
    expect(document.querySelector('.kk-ledger-row')).toBeNull()
    expect(currentLocation()).toBe('/tasks/tk1')

    await user.click(screen.getByRole('button', { name: 'List' }))
    expect(currentLocation()).toBe('/tasks/tk1?view=list')
    expect(document.querySelector('.kk-ledger-row')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'List' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Grid' }))
    expect(currentLocation()).toBe('/tasks/tk1')
    expect(document.querySelector('.kk-ledger-row')).toBeNull()
  })

  it('renders the date at its stated precision, the source, and the last comment', async () => {
    withPhotos([
      ledgerPhoto('ph1', {
        taken_at: '1974-06-01T00:00:00Z',
        taken_at_precision: 'month',
        taken_at_source: 'manual',
        taken_at_estimated: true,
        last_comment: {
          uid: 'cm1',
          body: 'Dated from the reunion badge.\n\nThe badge reads 1974.',
          author_uid: 'u2',
          author_name: 'Pan Botka',
          created_at: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
        },
      }),
      ledgerPhoto('ph2', { last_comment: null }),
    ])
    renderPage(true, '/tasks/tk1?view=list')
    await screen.findByRole('heading', { name: /In which year/ })

    const rows = await screen.findAllByRole('link', { name: /^Photo ph/ })
    expect(rows).toHaveLength(2)
    const first = rowOf(rows[0])
    // June, not the first of June: the month is all the catalogue claims.
    expect(within(first).getByText('June 1974')).toBeInTheDocument()
    expect(within(first).getByText('Set manually')).toBeInTheDocument()
    expect(within(first).getByText('estimate')).toBeInTheDocument()
    // The first line only, with who said it and how long ago.
    expect(within(first).getByText('Dated from the reunion badge.')).toBeInTheDocument()
    expect(within(first).queryByText(/The badge reads/)).toBeNull()
    expect(within(first).getByText(/Pan Botka/)).toBeInTheDocument()
    expect(within(first).getByText('2h ago')).toBeInTheDocument()

    const second = rowOf(rows[1])
    expect(within(second).getByText('no date')).toBeInTheDocument()
    expect(within(second).getByText('no comment')).toBeInTheDocument()
  })

  it('opens a row in the viewer with the task context, so prev/next stay inside the task', async () => {
    const user = userEvent.setup()
    withPhotos([ledgerPhoto('ph1')])
    renderPage(true, '/tasks/tk1?view=list')
    await screen.findByRole('heading', { name: /In which year/ })

    await user.click(await screen.findByRole('link', { name: 'Photo ph1' }))
    expect(currentLocation()).toBe('/photos/ph1?task=tk1&view=list')
  })

  it('selects from the rows, so the batch actions apply from the ledger too', async () => {
    const user = userEvent.setup()
    withPhotos([ledgerPhoto('ph1'), ledgerPhoto('ph2')])
    removeTaskPhotosMock.mockResolvedValue({ changed: 1, task: task() })
    renderPage(true, '/tasks/tk1?view=list')
    await screen.findByRole('heading', { name: /In which year/ })

    await user.click(await screen.findByRole('button', { name: 'Select Photo ph1' }))
    expect(screen.getByRole('button', { name: 'Select Photo ph1' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    // Selection-first now: clicking another row picks it rather than opening it.
    await user.click(screen.getByRole('link', { name: 'Photo ph2' }))
    expect(currentLocation()).toBe('/tasks/tk1?view=list')
    expect(screen.getByRole('button', { name: 'Select Photo ph2' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    const remove = await screen.findByRole('button', { name: 'Remove from the task' })
    expect(screen.getByRole('toolbar')).toContainElement(remove)
    await user.click(remove)
    await waitFor(() => {
      expect(removeTaskPhotosMock).toHaveBeenCalledWith('tk1', ['ph1', 'ph2'])
    })
  })

  it('reads the group in the sort the filter bar asks for', async () => {
    withPhotos(Array.from({ length: SMALL_GROUP_MAX + 1 }, (_, i) => ledgerPhoto(`ph${String(i)}`)))
    renderPage(true, '/tasks/tk1?view=list&sort=oldest')
    await screen.findByRole('heading', { name: /In which year/ })

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalledWith(
        expect.objectContaining({ task: 'tk1', sort: 'oldest' }),
        expect.anything(),
      )
    })
    expect(await screen.findAllByRole('link', { name: /^Photo ph/ })).toHaveLength(
      SMALL_GROUP_MAX + 1,
    )
    // The toggle sits in the filter bar for a group this size.
    expect(screen.getByRole('button', { name: 'List' })).toHaveAttribute('aria-pressed', 'true')
  })
})
