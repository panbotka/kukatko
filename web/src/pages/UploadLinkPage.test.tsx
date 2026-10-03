import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type UploadQueueItem } from '../hooks/useUploadQueue'
import i18n from '../i18n'
import { UPLOADER_NAME_KEY } from '../lib/uploadLinks'
import { ApiError } from '../services/auth'
import { type UploadFileResult } from '../services/upload'
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

beforeEach(async () => {
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
    await screen.findByTestId('upload-link-summary')
    expect(screen.queryByTestId('upload-link-register')).not.toBeInTheDocument()
  })

  it('greets a signed-in uploader instead of asking for a name, and offers no registration', async () => {
    uploadMock.mockResolvedValue(result('created'))
    const user = userEvent.setup()
    renderPage(viewer())
    expect(await screen.findByTestId('upload-link-signed-in')).toHaveTextContent('Jana')
    expect(screen.queryByLabelText('Who is it from?')).not.toBeInTheDocument()
    await pick(user, [new File(['a'], 'a.jpg', { type: 'image/jpeg' })])
    await waitFor(() => {
      expect(screen.getByTestId('upload-link-summary')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('upload-link-register')).not.toBeInTheDocument()
  })
})
