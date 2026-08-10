import { render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'

import { ShareTargetPage } from './ShareTargetPage'

vi.mock('../pwa/shareTarget', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../pwa/shareTarget')>()
  return { ...actual, discardSharedFiles: vi.fn(() => Promise.resolve()) }
})

const { discardSharedFiles } = await import('../pwa/shareTarget')
const discardMock = vi.mocked(discardSharedFiles)

/** A signed-in account of the given persuasion. */
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

/** Surfaces where the router ended up, so a forwarding can be asserted. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  return <span data-testid="location">{`${pathname}${search}`}</span>
}

/** Mounts the page at `url`, with `/upload` as a real (stub) destination. */
function renderPage(url: string, canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[url]}>
          <Routes>
            <Route path="/share-target" element={<ShareTargetPage />} />
            <Route path="/upload" element={<p>upload page</p>} />
            <Route path="/" element={<p>library</p>} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  discardMock.mockClear()
})

describe('ShareTargetPage', () => {
  it('forwards an editor to the upload page, carrying the staged share', () => {
    renderPage('/share-target?share=1700000000000-1')

    expect(screen.getByTestId('location')).toHaveTextContent('/upload?share=1700000000000-1')
    expect(screen.getByText('upload page')).toBeInTheDocument()
  })

  it('keeps the junction out of history, so Back does not walk into a spent share', () => {
    renderPage('/share-target?share=abc')

    // `replace` means the share URL never became an entry of its own; the probe
    // shows the upload page as the current — and only — location.
    expect(screen.getByTestId('location')).toHaveTextContent('/upload?share=abc')
  })

  it('tells a viewer their account cannot upload, and throws the photos away', async () => {
    renderPage('/share-target?share=abc', false)

    expect(screen.getByRole('heading', { name: 'Shared photos' })).toBeInTheDocument()
    expect(
      screen.getByText(/Your account can only view the library, not add to it/),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to the library' })).toHaveAttribute('href', '/')
    await waitFor(() => {
      expect(discardMock).toHaveBeenCalledWith('abc')
    })
  })

  it('says so when the files did not come through at all', () => {
    // No share id: either the worker could not read the POST, or none was
    // installed and the server answered it with the app shell.
    renderPage('/share-target')

    expect(screen.getByText(/The shared files did not reach us/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go to upload' })).toHaveAttribute('href', '/upload')
    expect(discardMock).not.toHaveBeenCalled()
  })

  it('does not discard anything when there was nothing staged for a viewer', () => {
    renderPage('/share-target', false)

    expect(
      screen.getByText(/Your account can only view the library, not add to it/),
    ).toBeInTheDocument()
    expect(discardMock).not.toHaveBeenCalled()
  })
})
