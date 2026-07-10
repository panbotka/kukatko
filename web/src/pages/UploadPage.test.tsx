import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type UploadFileResult } from '../services/upload'

import { UploadPage } from './UploadPage'

vi.mock('../services/upload', () => ({
  uploadFile: vi.fn(),
  isAbortError: (error: unknown): boolean =>
    error instanceof DOMException && error.name === 'AbortError',
}))

const { uploadFile } = await import('../services/upload')
const uploadMock = vi.mocked(uploadFile)

function file(name: string): File {
  return new File(['data'], name, { type: 'image/jpeg' })
}

function created(uid: string): UploadFileResult {
  return { filename: 'x', status: 201, outcome: 'created', photo_uid: uid }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <UploadPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** Picks files through the hidden gallery input (labelled for a11y). */
async function pickFiles(user: ReturnType<typeof userEvent.setup>, files: File[]): Promise<void> {
  const input = screen.getByLabelText('Choose photos or videos to upload')
  await user.upload(input, files)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  uploadMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('UploadPage', () => {
  it('queues selected files and shows them with a queued status', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
    expect(screen.getAllByText('Queued')).toHaveLength(2)
    expect(uploadMock).not.toHaveBeenCalled()
  })

  it('removes a queued file before upload', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    expect(screen.getByText('a.jpg')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Remove' }))
    expect(screen.queryByText('a.jpg')).not.toBeInTheDocument()
  })

  it('uploads on start and shows the created status and a library link', async () => {
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
    const link = await screen.findByRole('link', { name: 'View in library' })
    expect(link).toHaveAttribute('href', '/?sort=added')
  })

  it('renders duplicate and error outcomes from the responses', async () => {
    uploadMock
      .mockResolvedValueOnce({ filename: 'a', status: 409, outcome: 'duplicate', photo_uid: 'ph2' })
      .mockResolvedValueOnce({ filename: 'b', status: 500, outcome: 'error', error: 'boom' })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    expect(await screen.findByText('Already in library')).toBeInTheDocument()
    expect(await screen.findByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('boom')).toBeInTheDocument()
  })

  it('shows a near-duplicate warning without blocking', async () => {
    uploadMock.mockResolvedValue({
      filename: 'a.jpg',
      status: 201,
      outcome: 'created',
      photo_uid: 'ph1',
      warnings: [{ code: 'near_duplicate', message: 'similar' }],
    })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
    expect(screen.getByText('Looks similar to a photo already in the library.')).toBeInTheDocument()
  })

  it('retries a failed upload', async () => {
    uploadMock.mockRejectedValueOnce(new ApiError(0, 'network error'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Failed')).toBeInTheDocument()

    uploadMock.mockResolvedValueOnce(created('ph1'))
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
  })
})
