import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type AlbumSummary } from '../services/organize'
import { type UploadLink, type UploadLinkList } from '../services/uploadLinks'

import { UploadLinksPage } from './UploadLinksPage'

vi.mock('../services/uploadLinks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/uploadLinks')>()
  return {
    ...actual,
    fetchUploadLinks: vi.fn(),
    createUploadLink: vi.fn(),
    extendUploadLink: vi.fn(),
    revokeUploadLink: vi.fn(),
    restoreUploadLinkCode: vi.fn(),
    newUploadLinkCode: vi.fn(),
  }
})
vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    createAlbum: vi.fn(),
    createLabel: vi.fn(),
  }
})

const {
  fetchUploadLinks,
  createUploadLink,
  extendUploadLink,
  revokeUploadLink,
  restoreUploadLinkCode,
  newUploadLinkCode,
} = await import('../services/uploadLinks')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const listMock = vi.mocked(fetchUploadLinks)
const createMock = vi.mocked(createUploadLink)
const extendMock = vi.mocked(extendUploadLink)
const revokeMock = vi.mocked(revokeUploadLink)
const restoreMock = vi.mocked(restoreUploadLinkCode)
const newCodeMock = vi.mocked(newUploadLinkCode)

/** A link record with overrides. */
function link(overrides: Partial<UploadLink> = {}): UploadLink {
  return {
    uid: 'ul1',
    title: 'Pouť 2026',
    note: '',
    created_by: 'u1',
    created_by_name: 'Kurátor',
    created_at: '2026-10-01T10:00:00Z',
    expires_at: '2026-10-31T10:00:00Z',
    revoked_at: null,
    upload_count: 3,
    last_used_at: '2026-10-02T10:00:00Z',
    albums: [{ uid: 'al1', name: 'Pouť' }],
    labels: [{ uid: 'lb1', name: 'pouť' }],
    state: 'active',
    code: 'Ab3dEf7h',
    path: '/u/Ab3dEf7h',
    ...overrides,
  }
}

/** A list response holding links. */
function list(...links: UploadLink[]): UploadLinkList {
  return { links, default_days: 30, max_days: 365 }
}

/** An album as the catalog lists it. */
function album(uid: string, title: string): AlbumSummary {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

/** A signed-in curator (or admin). */
function auth(isAdmin = false): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'kurator', display_name: 'Kurátor', role: 'curator' },
    canCurate: true,
    canWrite: false,
    isAdmin,
  } as unknown as AuthContextValue
}

/** Prints the current location's search, so a test can see the URL change. */
function Search() {
  return <output data-testid="search">{useLocation().search}</output>
}

/** Renders the page at url. */
function renderPage(url = '/upload-links', isAdmin = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <MemoryRouter initialEntries={[url]}>
          <Routes>
            <Route
              path="/upload-links"
              element={
                <>
                  <UploadLinksPage />
                  <Search />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  listMock.mockReset().mockResolvedValue(list(link()))
  createMock.mockReset()
  extendMock.mockReset()
  revokeMock.mockReset()
  restoreMock.mockReset()
  newCodeMock.mockReset()
  vi.mocked(fetchAlbums)
    .mockReset()
    .mockResolvedValue([album('al1', 'Pouť'), album('al2', 'Hody')])
  vi.mocked(fetchLabels).mockReset().mockResolvedValue([])
})

describe('UploadLinksPage', () => {
  it('lists links with their targets, expiry and use', async () => {
    renderPage()
    const card = await screen.findByTestId('upload-link-card')
    expect(within(card).getByRole('heading', { name: 'Pouť 2026' })).toBeInTheDocument()
    expect(within(card).getByText('Active')).toBeInTheDocument()
    expect(within(card).getByRole('link', { name: /Pouť/ })).toHaveAttribute('href', '/albums/al1')
    expect(within(card).getByText(/3 uploads/)).toBeInTheDocument()
    // The curator's own link does not name its creator.
    expect(within(card).queryByText(/created by/)).not.toBeInTheDocument()
  })

  it('names the creator of somebody else’s link for an administrator', async () => {
    listMock.mockResolvedValue(list(link({ created_by: 'u9', created_by_name: 'Pepa' })))
    renderPage('/upload-links', true)
    expect(await screen.findByText(/created by Pepa/)).toBeInTheDocument()
  })

  it('shows an empty state', async () => {
    listMock.mockResolvedValue(list())
    renderPage()
    expect(await screen.findByTestId('upload-links-empty')).toBeInTheDocument()
  })

  it('opens the create form with the album from ?album= chosen, and shows the link once created', async () => {
    createMock.mockResolvedValue({
      link: link({ uid: 'ul2' }),
      code: 'Ab3dEf7h',
      path: '/u/Ab3dEf7h',
    })
    const user = userEvent.setup()
    renderPage('/upload-links?album=al2')
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Title shown to uploaders'), 'Hody')
    await waitFor(() => {
      expect(within(dialog).getByRole('button', { name: 'Create link' })).toBeEnabled()
    })
    await user.click(within(dialog).getByRole('button', { name: 'Create link' }))

    expect(createMock).toHaveBeenCalledWith({
      title: 'Hody',
      note: '',
      album_uids: ['al2'],
      label_uids: [],
      valid_days: 30,
    })
    const created = await within(dialog).findByTestId('upload-link-created')
    expect(within(created).getByRole('textbox')).toHaveValue(`${window.location.origin}/u/Ab3dEf7h`)

    await user.click(within(dialog).getByRole('button', { name: 'Done' }))
    await waitFor(() => {
      expect(screen.getByTestId('search')).toHaveTextContent('')
    })
    expect(listMock).toHaveBeenCalledTimes(2)
  })

  it('refuses a validity out of range', async () => {
    const user = userEvent.setup()
    renderPage('/upload-links?album=al1')
    const dialog = await screen.findByRole('dialog')
    const days = within(dialog).getByLabelText('Validity in days')
    await user.clear(days)
    await user.type(days, '400')
    expect(within(dialog).getByRole('button', { name: 'Create link' })).toBeDisabled()
    expect(within(dialog).getByText(/from 1 to 365/)).toBeInTheDocument()
  })

  it('extends and revokes a link', async () => {
    extendMock.mockResolvedValue(link({ expires_at: '2026-12-31T10:00:00Z' }))
    revokeMock.mockResolvedValue(link({ state: 'revoked', revoked_at: '2026-10-03T10:00:00Z' }))
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('upload-link-card')

    await user.click(screen.getByRole('button', { name: 'Extend' }))
    const extendDialog = await screen.findByRole('dialog')
    const days = within(extendDialog).getByLabelText('Valid for days (from today)')
    await user.clear(days)
    await user.type(days, '60')
    await user.click(within(extendDialog).getByRole('button', { name: 'Extend' }))
    expect(extendMock).toHaveBeenCalledWith('ul1', 60)

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Revoke link' }))
    const confirm = await screen.findByRole('dialog')
    await user.click(within(confirm).getByRole('button', { name: 'Revoke link' }))
    expect(revokeMock).toHaveBeenCalledWith('ul1')
    expect(await screen.findByText('Revoked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Extend' })).not.toBeInTheDocument()
  })

  it('copies a live link’s absolute address from its card', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const user = userEvent.setup()
    renderPage()
    const card = await screen.findByTestId('upload-link-card')
    const url = `${window.location.origin}/u/Ab3dEf7h`
    expect(within(card).getByRole('textbox', { name: 'Upload link' })).toHaveValue(url)
    // userEvent.setup() installs its own clipboard stub; put ours back.
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await user.click(within(card).getByRole('button', { name: 'Copy' }))
    expect(writeText).toHaveBeenCalledWith(url)
    expect(await within(card).findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(
      within(card).queryByRole('button', { name: 'Restore original code' }),
    ).not.toBeInTheDocument()
  })

  it('offers no copy on a revoked or an expired link', async () => {
    listMock.mockResolvedValue(
      list(
        link({ uid: 'ul1', state: 'revoked', revoked_at: '2026-10-03T10:00:00Z' }),
        link({ uid: 'ul2', state: 'expired' }),
      ),
    )
    renderPage()
    const cards = await screen.findAllByTestId('upload-link-card')
    expect(cards).toHaveLength(2)
    for (const card of cards) {
      expect(within(card).queryByRole('button', { name: 'Copy' })).not.toBeInTheDocument()
      expect(within(card).queryByRole('textbox')).not.toBeInTheDocument()
    }
  })

  it('offers restore first and a warned new code second on a link without a known code', async () => {
    listMock.mockResolvedValue(list(link({ code: undefined, path: undefined })))
    newCodeMock.mockResolvedValue(link({ code: 'Nw5cDe9k', path: '/u/Nw5cDe9k' }))
    const user = userEvent.setup()
    renderPage()
    const card = await screen.findByTestId('upload-link-card')
    expect(within(card).getByTestId('upload-link-code-unknown')).toBeInTheDocument()
    expect(within(card).queryByRole('button', { name: 'Copy' })).not.toBeInTheDocument()
    const restore = within(card).getByRole('button', { name: 'Restore original code' })
    expect(restore).toHaveClass('btn-primary')
    const renew = within(card).getByRole('button', { name: 'Generate new code' })
    expect(renew).not.toHaveClass('btn-primary')

    await user.click(renew)
    const confirm = await screen.findByRole('dialog')
    expect(within(confirm).getByTestId('upload-link-new-code-warning')).toHaveTextContent(
      /stops working immediately/,
    )
    expect(newCodeMock).not.toHaveBeenCalled()
    await user.click(within(confirm).getByRole('button', { name: 'Generate new code' }))
    expect(newCodeMock).toHaveBeenCalledWith('ul1')
    expect(
      await screen.findByDisplayValue(`${window.location.origin}/u/Nw5cDe9k`),
    ).toBeInTheDocument()
  })

  it('never claims an expired link without a known code still works', async () => {
    listMock.mockResolvedValue(
      list(
        link({ uid: 'ul1', code: undefined, path: undefined }),
        link({ uid: 'ul2', state: 'expired', code: undefined, path: undefined }),
      ),
    )
    renderPage()
    const [active, expired] = await screen.findAllByTestId('upload-link-card')
    expect(within(active).getByTestId('upload-link-code-unknown')).toHaveTextContent(
      /It still works/,
    )
    const notice = within(expired).getByTestId('upload-link-code-unknown')
    expect(notice).toHaveTextContent(/has expired/)
    expect(notice).toHaveTextContent(/“Extend” revives it at the same address/)
    expect(notice).not.toHaveTextContent(/still works/)
    expect(
      within(expired).getByRole('button', { name: 'Restore original code' }),
    ).toBeInTheDocument()
    expect(within(expired).getByRole('button', { name: 'Generate new code' })).toBeInTheDocument()
    expect(within(expired).getByRole('button', { name: 'Extend' })).toBeInTheDocument()
  })

  it('says an expired link has expired in Czech too', async () => {
    await i18n.changeLanguage('cs')
    listMock.mockResolvedValue(list(link({ state: 'expired', code: undefined, path: undefined })))
    renderPage()
    const card = await screen.findByTestId('upload-link-card')
    const notice = within(card).getByTestId('upload-link-code-unknown')
    expect(notice).toHaveTextContent(/Platnost odkazu vypršela/)
    expect(notice).not.toHaveTextContent(/dál funguje/)
  })

  it('restores the original code, and says so plainly on a mismatch', async () => {
    listMock.mockResolvedValue(list(link({ code: undefined, path: undefined })))
    restoreMock
      .mockRejectedValueOnce(new ApiError(422, 'uploadlink: the code does not match this link'))
      .mockResolvedValueOnce(link())
    const user = userEvent.setup()
    renderPage()
    const card = await screen.findByTestId('upload-link-card')
    await user.click(within(card).getByRole('button', { name: 'Restore original code' }))
    const dialog = await screen.findByRole('dialog')
    const field = within(dialog).getByLabelText('Original code or link')

    await user.type(field, 'Zz9Zz9Zz')
    await user.click(within(dialog).getByRole('button', { name: 'Restore original code' }))
    expect(await within(dialog).findByTestId('upload-link-restore-error')).toHaveTextContent(
      /not this link's code/,
    )
    expect(restoreMock).toHaveBeenLastCalledWith('ul1', 'Zz9Zz9Zz')

    await user.clear(field)
    await user.type(field, 'https://fotky.example/u/Ab3dEf7h')
    await user.click(within(dialog).getByRole('button', { name: 'Restore original code' }))
    expect(restoreMock).toHaveBeenLastCalledWith('ul1', 'Ab3dEf7h')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(
      within(screen.getByTestId('upload-link-card')).getByRole('button', { name: 'Copy' }),
    ).toBeInTheDocument()
  })
})
