import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type UploadQueueItem } from '../hooks/useUploadQueue'
import i18n from '../i18n'
import { PICKER_ACCEPT } from '../lib/mediaFiles'
import { UPLOAD_BATCH_KEY } from '../lib/uploadLinkBatch'
import { UPLOADER_NAME_KEY } from '../lib/uploadLinks'
import { ApiError } from '../services/auth'
import { type UploadFileOptions, type UploadFileResult } from '../services/upload'
import { type PublicUploadLink, UploadLinkGoneError } from '../services/uploadLinks'

import { UploadLinkPage } from './UploadLinkPage'

vi.mock('../services/upload', () => ({
  uploadFile: vi.fn(),
  isAbortError: (error: unknown): boolean =>
    error instanceof DOMException && error.name === 'AbortError',
}))
vi.mock('../services/uploadLinks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/uploadLinks')>()
  return { ...actual, fetchPublicUploadLink: vi.fn() }
})
vi.mock('../services/settings', () => ({ fetchPublicSettings: vi.fn() }))
// jsdom has no layout, so the real virtualized list renders nothing.
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
  }: {
    data: UploadQueueItem[]
    itemContent: (index: number, item: UploadQueueItem) => ReactNode
  }) => (
    <div>
      {data.map((item, index) => (
        <div key={item.id}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

const { uploadFile } = await import('../services/upload')
const { fetchPublicUploadLink } = await import('../services/uploadLinks')
const { fetchPublicSettings } = await import('../services/settings')
const uploadMock = vi.mocked(uploadFile)
const linkMock = vi.mocked(fetchPublicUploadLink)
const settingsMock = vi.mocked(fetchPublicSettings)

/** The link description every case starts from. */
const LINK: PublicUploadLink = {
  title: 'Pouť 2026',
  note: 'Díky za fotky!',
  albums: ['Pouť'],
  labels: ['pouť'],
  expires_at: '2026-11-01T12:00:00Z',
}

/** An anonymous visitor. */
function anonymous(): AuthContextValue {
  return {
    status: 'unauthenticated',
    user: null,
    canCurate: false,
    canWrite: false,
    isAdmin: false,
  } as unknown as AuthContextValue
}

/** A signed-in viewer. */
function viewer(): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'jana', display_name: 'Jana', role: 'viewer' },
    canCurate: false,
  } as unknown as AuthContextValue
}

/** Renders the page at /u/Ab3dEf7h with a stand-in registration route. */
function renderPage(auth: AuthContextValue = anonymous()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={['/u/Ab3dEf7h']}>
          <Routes>
            <Route path="/u/:code" element={<UploadLinkPage />} />
            <Route path="/register" element={<h1>Register here</h1>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** A per-file result. */
function result(outcome: UploadFileResult['outcome']): UploadFileResult {
  return { filename: 'x', status: 200, outcome, photo_uid: outcome === 'error' ? undefined : 'ph1' }
}

/** Picks files through the gallery input. */
async function pick(user: ReturnType<typeof userEvent.setup>, files: File[]) {
  const inputs = screen.getAllByLabelText('Choose photos or videos to upload')
  await user.upload(inputs[0], files)
}

/** One captured in-flight upload, settled from the test. */
interface Pending {
  file: File
  options: UploadFileOptions
  resolve: (value: UploadFileResult) => void
  reject: (error: unknown) => void
}

/** Makes every upload wait for the test, collecting them in the returned list. */
function holdUploads(): Pending[] {
  const pending: Pending[] = []
  uploadMock.mockImplementation(
    (file: File, options: UploadFileOptions = {}) =>
      new Promise<UploadFileResult>((resolve, reject) => {
        pending.push({ file, options, resolve, reject })
      }),
  )
  return pending
}

let onLine = true

/** Flips the connectivity and fires the matching window event. */
function setOnline(next: boolean) {
  onLine = next
  act(() => {
    window.dispatchEvent(new Event(next ? 'online' : 'offline'))
  })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'onLine')
})

beforeEach(async () => {
  onLine = true
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => onLine })
  await i18n.changeLanguage('en')
  localStorage.clear()
  uploadMock.mockReset()
  linkMock.mockReset().mockResolvedValue(LINK)
  settingsMock.mockReset().mockResolvedValue({
    registration_enabled: true,
    passkeys_enabled: false,
    mail_enabled: false,
  })
})

describe('UploadLinkPage', () => {
  it('shows the title, the note, where the photos go and the expiry — and only that', async () => {
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Pouť 2026' })).toBeInTheDocument()
    expect(screen.getByText('Díky za fotky!')).toBeInTheDocument()
    expect(screen.getByText('Pouť')).toBeInTheDocument()
    expect(screen.getByText('pouť')).toBeInTheDocument()
    expect(screen.getByText(/valid until/i)).toBeInTheDocument()
    // The chips name the targets; they must not lead into the library.
    expect(screen.queryByRole('link', { name: 'Pouť' })).not.toBeInTheDocument()
    expect(linkMock).toHaveBeenCalledWith('Ab3dEf7h', expect.any(AbortSignal))
  })

  it('says clearly when the link expired, was revoked or does not exist', async () => {
    linkMock.mockRejectedValueOnce(new UploadLinkGoneError('expired'))
    const { unmount } = renderPage()
    expect(await screen.findByText(/has expired/)).toBeInTheDocument()
    unmount()

    linkMock.mockRejectedValueOnce(new UploadLinkGoneError('revoked'))
    const second = renderPage()
    expect(await screen.findByText(/was revoked/)).toBeInTheDocument()
    second.unmount()

    linkMock.mockRejectedValueOnce(new ApiError(404, 'not found'))
    renderPage()
    expect(await screen.findByText(/does not exist/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Who is it from?')).not.toBeInTheDocument()
  })

  it('uploads through the link with the remembered name and sums the batch up', async () => {
    localStorage.setItem(UPLOADER_NAME_KEY, 'Babička')
    uploadMock
      .mockResolvedValueOnce(result('created'))
      .mockResolvedValueOnce(result('duplicate'))
      .mockResolvedValueOnce(result('error'))
    const user = userEvent.setup()
    renderPage()
    const name = await screen.findByLabelText('Who is it from?')
    expect(name).toHaveValue('Babička')

    await pick(user, [
      new File(['a'], 'a.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'b.heic', { type: 'image/heic' }),
      new File(['c'], 'c.mp4', { type: 'video/mp4' }),
    ])

    expect(await screen.findByTestId('upload-link-summary')).toHaveTextContent(
      '1 uploaded, 1 duplicate, 1 error',
    )
    expect(uploadMock).toHaveBeenCalledTimes(3)
    expect(uploadMock.mock.calls[0][1]).toMatchObject({
      url: '/api/v1/u/Ab3dEf7h/upload',
      fields: { name: 'Babička' },
    })
    expect(screen.getByRole('button', { name: 'Retry the failed files' })).toBeInTheDocument()
  })

  it('says a refused non-photo is not a photo, in words, and offers no retry for it', async () => {
    uploadMock.mockResolvedValueOnce({
      filename: 'broken.jpg',
      status: 415,
      outcome: 'error',
      code: 'not_media',
      error: 'ingest: not a photo or video: broken.jpg',
    })
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')

    await pick(user, [new File(['junk'], 'broken.jpg', { type: 'image/jpeg' })])

    expect(await screen.findByTestId('upload-link-summary')).toHaveTextContent(
      '0 uploaded, 1 error',
    )
    expect(screen.getByText('This is not a photo or a video')).toBeInTheDocument()
    expect(screen.queryByText(/ingest: not a photo/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry the failed files' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })

  it('still offers a retry for the failures beside a refused non-photo that might go differently', async () => {
    uploadMock
      .mockResolvedValueOnce({
        filename: 'broken.jpg',
        status: 415,
        outcome: 'error',
        code: 'not_media',
      })
      .mockResolvedValueOnce({ filename: 'b.jpg', status: 500, outcome: 'error', error: 'boom' })
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')

    await pick(user, [
      new File(['junk'], 'broken.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'b.jpg', { type: 'image/jpeg' }),
    ])

    await screen.findByTestId('upload-link-summary')
    expect(screen.getAllByRole('button', { name: 'Retry' })).toHaveLength(1)
    uploadMock.mockResolvedValueOnce(result('created'))
    await user.click(screen.getByRole('button', { name: 'Retry the failed files' }))
    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(3)
    })
    expect(uploadMock.mock.calls[2][0].name).toBe('b.jpg')
  })

  it('offers the pickers exactly the formats the server takes, AVIF not among them', async () => {
    const { container } = renderPage()
    await screen.findByLabelText('Who is it from?')

    const inputs = Array.from(container.querySelectorAll<HTMLInputElement>('input[type="file"]'))
    expect(inputs.length).toBeGreaterThan(0)
    for (const input of inputs) {
      expect(input.accept).toBe(PICKER_ACCEPT)
      expect(input.accept.split(',')).not.toContain('.avif')
    }
  })

  it('says a damaged image is damaged, and offers no retry for it', async () => {
    uploadMock.mockResolvedValueOnce({
      filename: 'cut.jpg',
      status: 415,
      outcome: 'error',
      code: 'damaged',
      error: 'ingest: damaged or incomplete image: cut.jpg',
    })
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')

    await pick(user, [new File(['cut'], 'cut.jpg', { type: 'image/jpeg' })])

    expect(await screen.findByText('This file is damaged or incomplete')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })

  it('remembers a typed name on the device', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(await screen.findByLabelText('Who is it from?'), 'Jana')
    expect(localStorage.getItem(UPLOADER_NAME_KEY)).toBe('Jana')
  })

  it('offers registration through the link once an anonymous upload finishes', async () => {
    uploadMock.mockResolvedValue(result('created'))
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    expect(screen.queryByTestId('upload-link-register')).not.toBeInTheDocument()

    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    const offer = await screen.findByTestId('upload-link-register')
    await user.click(screen.getByRole('link', { name: 'Register' }))
    expect(offer).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Register here' })).toBeInTheDocument()
  })

  it('makes no registration offer when registration is closed', async () => {
    settingsMock.mockResolvedValue({
      registration_enabled: false,
      passkeys_enabled: false,
      mail_enabled: false,
    })
    uploadMock.mockResolvedValue(result('created'))
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    await screen.findByTestId('upload-link-done')
    expect(screen.queryByTestId('upload-link-register')).not.toBeInTheDocument()
  })

  it('greets a signed-in uploader instead of asking for a name, and offers no registration', async () => {
    uploadMock.mockResolvedValue(result('created'))
    const user = userEvent.setup()
    renderPage(viewer())
    expect(await screen.findByTestId('upload-link-signed-in')).toHaveTextContent('Jana')
    expect(screen.queryByLabelText('Who is it from?')).not.toBeInTheDocument()
    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    await screen.findByTestId('upload-link-done')
    expect(screen.queryByTestId('upload-link-register')).not.toBeInTheDocument()
  })

  it('puts one large picker first and keeps the drop zone for wider screens', async () => {
    const { container } = renderPage()
    await screen.findByLabelText('Who is it from?')
    const buttons = screen.getAllByRole('button')
    expect(buttons[0]).toHaveTextContent('Choose photos')
    expect(buttons[0]).toHaveClass('kk-upload-link__primary')
    const drop = container.querySelector('.kk-upload-drop')
    expect(drop?.closest('.d-none.d-md-block')).not.toBeNull()
  })

  it('shows only the progress while uploading — nothing to cancel, nowhere to leave', async () => {
    const pending = holdUploads()
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')

    await pick(user, [
      new File(['aaaa'], 'a.jpg', { type: 'image/jpeg' }),
      new File(['bbbb'], 'b.jpg', { type: 'image/jpeg' }),
      new File(['cccc'], 'c.jpg', { type: 'image/jpeg' }),
      new File(['dddd'], 'd.jpg', { type: 'image/jpeg' }),
    ])

    expect(await screen.findByTestId('upload-link-uploading')).toBeInTheDocument()
    expect(screen.getByTestId('upload-link-count')).toHaveTextContent('0 of 4 photos')
    expect(screen.getByTestId('upload-link-eta')).toHaveTextContent('calculating…')
    expect(screen.getByTestId('upload-link-percent')).toHaveTextContent('0%')
    expect(screen.getByText(/keep this page open/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Who is it from?')).not.toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()

    // The details are closed, and opening them offers no Remove for the waiting file.
    const toggle = screen.getByRole('button', { name: 'Details' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(screen.getByText('d.jpg')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()

    await act(async () => {
      pending[0].resolve(result('created'))
      await Promise.resolve()
    })
    expect(await screen.findByText('1 of 4 photos')).toBeInTheDocument()
    expect(screen.getByTestId('upload-link-percent')).toHaveTextContent('25%')
  })

  it('ends on an unmistakable done screen that can start another batch', async () => {
    uploadMock.mockResolvedValue(result('created'))
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    await pick(user, [
      new File(['a'], 'a.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'b.jpg', { type: 'image/jpeg' }),
    ])

    expect(
      await screen.findByRole('heading', { name: 'Done, 2 photos uploaded' }),
    ).toBeInTheDocument()
    // Nothing more to say than the heading: no breakdown line under it.
    expect(screen.queryByTestId('upload-link-summary')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Upload more' }))
    expect(screen.getByRole('button', { name: 'Choose photos' })).toBeInTheDocument()
    expect(screen.queryByTestId('upload-link-done')).not.toBeInTheDocument()
  })

  it('carries on by itself after a dropped connection, without an error', async () => {
    const pending = holdUploads()
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    await waitFor(() => {
      expect(pending).toHaveLength(1)
    })

    await act(async () => {
      pending[0].reject(new ApiError(0, 'network error'))
      await Promise.resolve()
    })
    expect(await screen.findByText('Connection lost, carrying on…')).toBeInTheDocument()
    expect(screen.queryByTestId('upload-link-summary')).not.toBeInTheDocument()
    expect(screen.getByText('0 failed')).toBeInTheDocument()

    // The network comes back: the file goes again with no tap.
    setOnline(true)
    await waitFor(() => {
      expect(pending).toHaveLength(2)
    })
    expect(pending[1].file.name).toBe('a.jpg')
    await act(async () => {
      pending[1].resolve(result('created'))
      await Promise.resolve()
    })
    expect(
      await screen.findByRole('heading', { name: 'Done, 1 photo uploaded' }),
    ).toBeInTheDocument()
  })

  it('waits for the network while offline instead of failing the waiting files', async () => {
    const pending = holdUploads()
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    await pick(user, [
      new File(['a'], 'a.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'b.jpg', { type: 'image/jpeg' }),
      new File(['c'], 'c.jpg', { type: 'image/jpeg' }),
      new File(['d'], 'd.jpg', { type: 'image/jpeg' }),
    ])
    await waitFor(() => {
      expect(pending).toHaveLength(3)
    })

    setOnline(false)
    expect(await screen.findByTestId('upload-link-offline')).toHaveTextContent(
      'You are offline — waiting for a connection.',
    )
    await act(async () => {
      pending[0].resolve(result('created'))
      await Promise.resolve()
    })
    // The freed slot is not used while offline.
    expect(pending).toHaveLength(3)

    setOnline(true)
    await waitFor(() => {
      expect(pending).toHaveLength(4)
    })
    expect(screen.queryByTestId('upload-link-offline')).not.toBeInTheDocument()
  })

  it('explains a batch a reload interrupted, and forgets it once a batch finishes', async () => {
    localStorage.setItem(
      UPLOAD_BATCH_KEY,
      JSON.stringify({ code: 'Ab3dEf7h', count: 12, startedAt: Date.now() - 60_000 }),
    )
    uploadMock.mockResolvedValue(result('duplicate'))
    const user = userEvent.setup()
    renderPage()
    expect(await screen.findByTestId('upload-link-lost-batch')).toHaveTextContent(
      'Uploading 12 photos was interrupted',
    )

    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    await screen.findByTestId('upload-link-done')
    expect(localStorage.getItem(UPLOAD_BATCH_KEY)).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Upload more' }))
    expect(screen.queryByTestId('upload-link-lost-batch')).not.toBeInTheDocument()
  })

  it('remembers a running batch on the device', async () => {
    holdUploads()
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Who is it from?')
    await pick(user, [
      new File(['a'], 'a.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'b.jpg', { type: 'image/jpeg' }),
    ])
    await screen.findByTestId('upload-link-uploading')
    expect(JSON.parse(localStorage.getItem(UPLOAD_BATCH_KEY) ?? '{}')).toMatchObject({
      code: 'Ab3dEf7h',
      count: 2,
    })
  })

  it('estimates the remaining time once a few seconds of progress are in', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const pending = holdUploads()
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      renderPage()
      await screen.findByLabelText('Who is it from?')
      await pick(user, [new File(['x'.repeat(100_000)], 'big.mp4', { type: 'video/mp4' })])
      await waitFor(() => {
        expect(pending).toHaveLength(1)
      })
      const { onProgress } = pending[0].options
      for (let second = 1; second <= 6; second += 1) {
        // Two steps, as in a browser: the progress renders before the next sample.
        act(() => {
          onProgress?.(second / 600)
        })
        act(() => {
          vi.advanceTimersByTime(1000)
        })
      }
      // ~1/600 of the file a second: some nine minutes to go.
      expect(screen.getByTestId('upload-link-eta')).toHaveTextContent(/^about (9|10) min left$/)
    } finally {
      vi.useRealTimers()
    }
  })
})
