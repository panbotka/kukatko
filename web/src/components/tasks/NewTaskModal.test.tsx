import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type Task } from '../../services/tasks'

import { NewTaskModal } from './NewTaskModal'

vi.mock('../../services/tasks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/tasks')>()
  return { ...actual, createTask: vi.fn(), fetchTasks: vi.fn(), addTaskPhotos: vi.fn() }
})

vi.mock('../../services/directory', () => ({
  listDirectory: vi.fn(),
}))

const { createTask, fetchTasks, addTaskPhotos } = await import('../../services/tasks')
const createTaskMock = vi.mocked(createTask)
const fetchTasksMock = vi.mocked(fetchTasks)
const addTaskPhotosMock = vi.mocked(addTaskPhotos)
const { listDirectory } = await import('../../services/directory')
const listDirectoryMock = vi.mocked(listDirectory)

/** A task with everything at a sensible default. */
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
    photo_count: 1,
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

/** Renders the dialog inside a router that records where it navigates to. */
function renderModal(photoUids?: string[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<NewTaskModal show onClose={vi.fn()} photoUids={photoUids} />} />
          <Route path="/tasks/:uid" element={<p>the task page</p>} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  createTaskMock.mockReset()
  fetchTasksMock.mockReset()
  addTaskPhotosMock.mockReset()
  listDirectoryMock.mockReset()
  listDirectoryMock.mockResolvedValue([
    { uid: 'u2', name: 'Anna' },
    { uid: 'u3', name: 'Agent' },
  ])
})

describe('NewTaskModal', () => {
  it('hands the task to somebody and opens it as work, in one request', async () => {
    const user = userEvent.setup()
    createTaskMock.mockResolvedValue(task({ state: 'working' }))
    renderModal([])

    await user.type(screen.getByLabelText('Question'), 'Rename the wf: labels')

    // The state offers only the two openings, never a closed one, and the
    // helper text says what each means.
    const state = screen.getByLabelText('State')
    expect(
      within(state)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Waiting for an answer', 'In progress'])
    expect(screen.getByText('someone should answer')).toBeInTheDocument()
    await user.selectOptions(state, 'working')
    expect(screen.getByText('someone should do it')).toBeInTheDocument()

    // Who: the picker over the directory, the pick shown as a chip.
    await user.click(screen.getByRole('button', { name: 'Add' }))
    const picker = await screen.findByRole('dialog', { name: 'Who should be on this task?' })
    await user.click(within(picker).getByRole('button', { name: /Anna/ }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Who should be on this task?' })).toBeNull()
    })
    const who = screen.getByRole('group', { name: 'Who' })
    expect(within(who).getByText('Anna')).toBeInTheDocument()

    // A second pick no longer offers the first person.
    await user.click(screen.getByRole('button', { name: 'Add' }))
    const again = await screen.findByRole('dialog', { name: 'Who should be on this task?' })
    expect(within(again).queryByRole('button', { name: /Anna/ })).toBeNull()
    await user.click(within(again).getByRole('button', { name: /Agent/ }))

    // And a chip can be taken off again before anything is sent.
    await user.click(await screen.findByRole('button', { name: 'Remove Agent' }))
    expect(within(who).queryByText('Agent')).toBeNull()

    await user.click(screen.getByRole('button', { name: 'New task' }))
    await waitFor(() => {
      expect(createTaskMock).toHaveBeenCalledWith({
        title: 'Rename the wf: labels',
        body: '',
        photo_uids: [],
        participants: ['u2'],
        state: 'working',
      })
    })
    expect(await screen.findByText('the task page')).toBeInTheDocument()
  })

  it('says in one line that a task from the bare button has no photos', () => {
    renderModal()
    expect(screen.getByText('A task without photos; they can be added later.')).toBeInTheDocument()
    expect(screen.queryByText(/\d+ photos?/)).not.toBeInTheDocument()
  })

  it('keeps the selection and says how big it is', () => {
    renderModal(['ph1', 'ph2'])
    expect(screen.getByText('2 photos')).toBeInTheDocument()
    expect(screen.queryByText('A task without photos; they can be added later.')).toBeNull()
  })

  it('opens a question over the selection and lands on its page', async () => {
    const user = userEvent.setup()
    createTaskMock.mockResolvedValue(task())
    renderModal(['ph1', 'ph2'])

    await user.type(screen.getByLabelText('Question'), 'In which year?')
    await user.click(screen.getByRole('button', { name: 'New task' }))

    await waitFor(() => {
      expect(createTaskMock).toHaveBeenCalledWith({
        title: 'In which year?',
        body: '',
        photo_uids: ['ph1', 'ph2'],
      })
    })
    // The page it lands on is both the confirmation and the link to send on.
    expect(await screen.findByText('the task page')).toBeInTheDocument()
  })

  it('opens a question with answer options, added one at a time', async () => {
    const user = userEvent.setup()
    createTaskMock.mockResolvedValue(task())
    renderModal(['ph1'])

    await user.type(screen.getByLabelText('Question'), 'Is it 1936 or 1938?')
    const field = screen.getByLabelText('Answer options')
    // Enter adds an option and does not submit the dialog.
    await user.type(field, '1936{Enter}')
    expect(createTaskMock).not.toHaveBeenCalled()
    await user.type(field, '1938')
    await user.click(screen.getByRole('button', { name: 'Add an option' }))
    // A repeat is refused quietly: the plus stays disabled.
    await user.type(field, '1938')
    expect(screen.getByRole('button', { name: 'Add an option' })).toBeDisabled()
    await user.clear(field)

    expect(screen.getByText('1936')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Remove option 1938' }))
    await user.type(field, 'nevím{Enter}')

    await user.click(screen.getByRole('button', { name: 'New task' }))
    await waitFor(() => {
      expect(createTaskMock).toHaveBeenCalledWith({
        title: 'Is it 1936 or 1938?',
        body: '',
        photo_uids: ['ph1'],
        options: ['1936', 'nevím'],
      })
    })
  })

  it('stops at five options', async () => {
    const user = userEvent.setup()
    renderModal(['ph1'])

    const field = screen.getByLabelText('Answer options')
    for (const option of ['a', 'b', 'c', 'd', 'e']) {
      await user.type(field, `${option}{Enter}`)
    }
    expect(screen.queryByLabelText('Answer options')).not.toBeInTheDocument()
    expect(screen.getByText(/No more than 5 options/)).toBeInTheDocument()
  })

  it('refuses to open a question with no question in it', () => {
    renderModal(['ph1'])
    expect(screen.getByRole('button', { name: 'New task' })).toBeDisabled()
  })

  it('adds the selection to a question that already exists', async () => {
    const user = userEvent.setup()
    fetchTasksMock.mockResolvedValue({ tasks: [task()], total: 1, limit: 50, offset: 0 })
    addTaskPhotosMock.mockResolvedValue({ changed: 1, task: task() })
    renderModal(['ph3'])

    await user.click(screen.getByRole('button', { name: 'Add to a question' }))

    // Only the open ones are offered: a closed task's frozen group is the record
    // of what the work touched, and adding to it after the fact would be a lie.
    await waitFor(() => {
      expect(fetchTasksMock).toHaveBeenCalledWith(
        expect.objectContaining({ open: true }),
        expect.anything(),
      )
    })

    await user.click(await screen.findByRole('button', { name: /In which year/ }))
    await waitFor(() => {
      expect(addTaskPhotosMock).toHaveBeenCalledWith('tk1', ['ph3'])
    })
    expect(await screen.findByText('the task page')).toBeInTheDocument()
  })

  it('keeps a long question inside the dialog', async () => {
    const user = userEvent.setup()
    fetchTasksMock.mockResolvedValue({
      tasks: [
        task({ title: 'A question long enough to be wider than the dialog it is listed in' }),
      ],
      total: 1,
      limit: 50,
      offset: 0,
    })
    renderModal(['ph3'])

    await user.click(screen.getByRole('button', { name: 'Add to a question' }))
    const row = await screen.findByRole('button', { name: /A question long enough/ })

    // jsdom lays nothing out, so what the guard can hold is the mechanism: the
    // rows are a stretched flex column (a `d-grid` column takes the width of its
    // widest item, which pushed the row and its state badge outside the dialog)
    // and the title is the part allowed to truncate.
    expect(row.parentElement).toHaveClass('d-flex', 'flex-column')
    expect(row.parentElement).not.toHaveClass('d-grid')
    expect(row.querySelector('.text-truncate')).toHaveTextContent(/A question long enough/)
  })

  it('says so when there is no open question to add to', async () => {
    const user = userEvent.setup()
    fetchTasksMock.mockResolvedValue({ tasks: [], total: 0, limit: 50, offset: 0 })
    renderModal(['ph3'])

    await user.click(screen.getByRole('button', { name: 'Add to a question' }))
    expect(await screen.findByText(/No open question yet/)).toBeInTheDocument()
  })

  it('offers only the new question when there is nothing to add', () => {
    // The bare "new task" button opens the dialog with no photographs, and
    // adding nothing to an existing question is not a thing to offer.
    renderModal()
    expect(screen.queryByRole('button', { name: 'Add to a question' })).not.toBeInTheDocument()
  })
})
