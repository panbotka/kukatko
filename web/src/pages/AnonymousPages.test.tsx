import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { setCurrentProjectSlug } from '../services/api'

import { LoginPage } from './LoginPage'
import { PasswordResetPage } from './PasswordResetPage'
import { ProjectRegisterPage, RegisterPage } from './RegisterPage'
import { UploadLinkPage } from './UploadLinkPage'

// The real apiPath, watched: an anonymous page has no project, so a single call
// is the bug this file guards against even when the page swallows the error.
vi.mock('../services/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/api')>()
  return { ...actual, apiPath: vi.fn(actual.apiPath) }
})

const { apiPath } = await import('../services/api')
const apiPathMock = vi.mocked(apiPath)

const LINK = {
  title: 'Pouť 2026',
  note: '',
  albums: [],
  labels: [],
  expires_at: '2026-11-01T12:00:00Z',
}

/** Answers the public routes the pages ask; anything else is a 404. */
function stubFetch() {
  // Every service passes a string URL; a Request object would be news.
  const fetchMock = vi.fn((url: string) => {
    if (url === '/api/v1/settings/public') {
      return Promise.resolve(Response.json({ passkeys_enabled: false, mail_enabled: false }))
    }
    if (url === '/api/v1/p/rodina/settings/public') {
      return Promise.resolve(Response.json({ project_name: 'Rodina', registration_enabled: true }))
    }
    if (url === '/api/v1/u/Ab3dEf7h') {
      return Promise.resolve(Response.json(LINK))
    }
    return Promise.resolve(Response.json({ error: 'not found' }, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

/**
 * The URLs an anonymous page may ask: instance routes, plus the one anonymous
 * project route — a named project's public settings, which its registration
 * page reads by slug through `instancePath`, never `apiPath`.
 */
const ANONYMOUS_URL = /^\/api\/v1\/((?!p\/)|p\/[^/]+\/settings\/public$)/

/** A visitor nobody has signed in yet. */
const ANONYMOUS = {
  status: 'unauthenticated',
  user: null,
  canCurate: false,
  canWrite: false,
  isAdmin: false,
} as unknown as AuthContextValue

/** Renders `page` at `url`, matched by `path`, as App.tsx mounts it. */
function renderAt(url: string, path: string, page: ReactNode) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={ANONYMOUS}>
        <MemoryRouter initialEntries={[url]}>
          <Routes>
            <Route path={path} element={page} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(() => {
  // Signed out: no project at all, as before the first GET /auth/me.
  setCurrentProjectSlug(null)
})

describe.each([
  { name: 'login', url: '/login', path: '/login', page: <LoginPage /> },
  // The bare /register asks nothing at all; through an upload link it shows the
  // form, which reads the instance's mail flag.
  {
    name: 'register',
    url: '/register?link=Ab3dEf7h',
    path: '/register',
    page: <RegisterPage />,
  },
  {
    name: 'project register',
    url: '/p/rodina/register',
    path: '/p/:slug/register',
    page: <ProjectRegisterPage />,
  },
  {
    name: 'password reset',
    url: '/password-reset/tok123',
    path: '/password-reset/:token',
    page: <PasswordResetPage />,
  },
  { name: 'upload link', url: '/u/Ab3dEf7h', path: '/u/:code', page: <UploadLinkPage /> },
])('the anonymous $name page', ({ url, path, page }) => {
  it('renders with no current project and only asks instance routes', async () => {
    const fetchMock = stubFetch()

    expect(() => renderAt(url, path, page)).not.toThrow()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument()
    expect(apiPathMock).not.toHaveBeenCalled()
    for (const [url] of fetchMock.mock.calls) {
      expect(url).toMatch(ANONYMOUS_URL)
    }
  })
})
