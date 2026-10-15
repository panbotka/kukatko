import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { ProjectMembership } from '../services/auth'

import { ShareTargetPage } from './ShareTargetPage'

// Collecting a share consumes it; only a chosen project's upload page may do
// that, so the junction must never touch the cache. The spy proves it does not.
vi.mock('../pwa/shareTarget', () => ({
  collectSharedFiles: vi.fn(() => Promise.resolve({ accepted: [], rejected: [] })),
}))
const { collectSharedFiles } = await import('../pwa/shareTarget')
const collectMock = vi.mocked(collectSharedFiles)

/** A membership of the project `slug`, approved and active unless stated. */
function member(slug: string, name: string, over: Partial<ProjectMembership> = {}) {
  return {
    uid: `p-${slug}`,
    slug,
    name,
    active: true,
    role: 'editor',
    approved: true,
    ...over,
  } satisfies ProjectMembership
}

const VESELICE = member('v', 'Veselice')
const RODINA = member('rodina', 'Rodina')

/** A signed-in account holding `projects`, last working in `last`. */
function auth(
  projects: ProjectMembership[],
  last: string | null,
  refresh: () => Promise<void> = vi.fn(() => Promise.resolve()),
): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: 'editor' },
    role: 'editor',
    instanceRole: 'user',
    projects,
    lastProjectSlug: last,
    downloadToken: null,
    canCurate: true,
    canWrite: true,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh,
  } as unknown as AuthContextValue
}

/** Surfaces where the router ended up, so a forwarding can be asserted. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  return <span data-testid="location">{`${pathname}${search}`}</span>
}

/** The router the page lives in, with the places it may forward to as stubs. */
function Routed({ value, url }: { value: AuthContextValue; url: string }) {
  return (
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[url]}>
          <Routes>
            <Route path="/share-target" element={<ShareTargetPage />} />
            <Route path="/p/:slug/upload" element={<p>upload page</p>} />
            <Route path="/projects" element={<p>chooser</p>} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>
  )
}

/** Mounts the page at `url` for an account holding `projects`. */
function renderPage(url: string, projects: ProjectMembership[], last: string | null = null) {
  return render(<Routed value={auth(projects, last)} url={url} />)
}

beforeEach(async () => {
  collectMock.mockClear()
  await i18n.changeLanguage('en')
})

describe('ShareTargetPage — one project', () => {
  it('forwards straight to that project’s upload page, carrying the staged share', () => {
    renderPage('/share-target?share=1700000000000-1', [VESELICE], 'v')

    expect(screen.getByTestId('location')).toHaveTextContent('/p/v/upload?share=1700000000000-1')
    expect(screen.getByText('upload page')).toBeInTheDocument()
  })

  it('goes to the one project the account may enter, not the one it last used', () => {
    // A membership still waiting for approval, or a switched-off project, is
    // no place for photos: the only enterable project is the only choice.
    renderPage(
      '/share-target?share=abc',
      [
        member('stary', 'Starý', { active: false }),
        member('cekam', 'Čekám', { approved: false }),
        RODINA,
      ],
      'stary',
    )

    expect(screen.getByTestId('location')).toHaveTextContent('/p/rodina/upload?share=abc')
  })
})

describe('ShareTargetPage — several projects', () => {
  it('asks which project before anything is uploaded, even with a last-used one', () => {
    renderPage('/share-target?share=abc', [VESELICE, RODINA], 'v')

    expect(
      screen.getByRole('heading', { name: 'Where should the shared photos go?' }),
    ).toBeInTheDocument()
    // Still on the junction: nothing forwarded, nothing collected.
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=abc')
    expect(screen.queryByText('upload page')).not.toBeInTheDocument()
    expect(collectMock).not.toHaveBeenCalled()
    expect(screen.getByRole('link', { name: /Veselice/ })).toHaveAttribute(
      'href',
      '/p/v/upload?share=abc',
    )
    expect(screen.getByRole('link', { name: 'Rodina' })).toHaveAttribute(
      'href',
      '/p/rodina/upload?share=abc',
    )
  })

  it('marks the last-used project without choosing it', () => {
    renderPage('/share-target?share=abc', [VESELICE, RODINA], 'v')

    expect(screen.getByRole('link', { name: /Veselice/ })).toHaveTextContent('Last used')
    expect(screen.getByRole('link', { name: 'Rodina' })).not.toHaveTextContent('Last used')
  })

  it('sends the share into the project picked', async () => {
    const user = userEvent.setup()
    renderPage('/share-target?share=abc', [VESELICE, RODINA], 'v')

    await user.click(screen.getByRole('link', { name: 'Rodina' }))

    expect(screen.getByTestId('location')).toHaveTextContent('/p/rodina/upload?share=abc')
    expect(screen.getByText('upload page')).toBeInTheDocument()
  })
})

describe('ShareTargetPage — no project', () => {
  it('keeps the files and explains instead of dropping them at the chooser', () => {
    renderPage('/share-target?share=abc', [member('cekam', 'Čekám', { approved: false })])

    expect(screen.getByTestId('share-waiting')).toHaveTextContent(
      'not a member of any library you could add these photos to yet',
    )
    expect(screen.getByTestId('share-waiting')).toHaveTextContent('stay on this device for a day')
    // The share stays in the address, so a reload after approval still has it.
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=abc')
    expect(collectMock).not.toHaveBeenCalled()
    expect(screen.getByRole('link', { name: 'Your projects' })).toHaveAttribute('href', '/projects')
  })

  it('carries the share on once a re-read of the session finds an approved membership', async () => {
    const user = userEvent.setup()
    /** Holds the session, so `refresh` can swap it the way AuthProvider would. */
    function Stateful() {
      const [projects, setProjects] = useState<ProjectMembership[]>([
        member('rodina', 'Rodina', { approved: false }),
      ])
      const refresh = () => {
        setProjects([RODINA])
        return Promise.resolve()
      }
      return <Routed value={auth(projects, null, refresh)} url="/share-target?share=abc" />
    }
    render(<Stateful />)

    await user.click(screen.getByRole('button', { name: 'Check again' }))

    expect(screen.getByTestId('location')).toHaveTextContent('/p/rodina/upload?share=abc')
  })
})

describe('ShareTargetPage — nothing staged', () => {
  it('says the files did not come through and offers the one project’s upload page', () => {
    // No share id: either the worker could not read the POST, or none was
    // installed and the server answered it with the app shell.
    renderPage('/share-target', [VESELICE], 'v')

    expect(screen.getByRole('heading', { name: 'Shared photos' })).toBeInTheDocument()
    expect(screen.getByText(/The shared files did not reach us/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go to upload' })).toHaveAttribute(
      'href',
      '/p/v/upload',
    )
  })

  it('offers the chooser rather than guessing when there are several projects', () => {
    renderPage('/share-target', [VESELICE, RODINA], 'v')

    expect(screen.getByText(/The shared files did not reach us/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Choose a library' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })
})
