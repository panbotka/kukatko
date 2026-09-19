import { render, screen, waitFor } from '@testing-library/react'
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

const { createTask, fetchTasks, addTaskPhotos } = await import('../../services/tasks')
const createTaskMock = vi.mocked(createTask)
const fetchTasksMock = vi.mocked(fetchTasks)
const addTaskPhotosMock = vi.mocked(addTaskPhotos)

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
    participants: [],
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
})

describe('NewTaskModal', () => {
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
