import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppRoutes } from './App'
import { AuthContext, type AuthContextValue } from './auth/AuthContext'
import i18n from './i18n'
import { type PhotoListResponse, type Timeline } from './services/photos'

// The library page is what `/` renders, so the routing tests need its data
// source stubbed. An empty catalog is enough: the route resolved is what matters.
vi.mock('./services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/photos')>()
  return { ...actual, fetchPhotos: vi.fn(), fetchTimeline: vi.fn() }
})

// The statistics route fetches its counts on mount; stub the call so the route
// test exercises the wiring rather than the network.
vi.mock('./services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/system')>()
  return { ...actual, fetchLibraryStats: vi.fn(() => new Promise(() => undefined)) }
})

// The own-activity route loads the caller's audit entries on mount; stub the
// call (never resolving) so the route test exercises the wiring, not the network.
vi.mock('./services/audit', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/audit')>()
  return { ...actual, fetchMyActivity: vi.fn(() => new Promise(() => undefined)) }
})

// The public registration route asks the instance whether registration is open,
// and the shell's first-run welcome asks for the administrator's greeting; stub
// both so the route tests exercise the wiring, not the network. The greeting
// never resolves, which keeps the welcome closed over every routed page.
vi.mock('./services/settings', () => ({
  fetchPublicSettings: vi.fn(() => Promise.resolve({ registration_enabled: true })),
  fetchWelcomeMarkdown: vi.fn(() => new Promise(() => undefined)),
}))

// Before the shell's welcome decides whether to ask who the reader is, it asks
// the library who is named; answer "nobody", which is also what every routed page
// under test wants from the subject list.
vi.mock('./services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/people')>()
  return { ...actual, fetchSubjects: vi.fn(() => Promise.resolve([])) }
})

// The password-reset landing route checks its link on mount; stub the call (never
// resolving) so the route test exercises the wiring, not the network.
vi.mock('./services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/auth')>()
  return { ...actual, fetchPasswordResetStatus: vi.fn(() => new Promise(() => undefined)) }
})

const { fetchPhotos, fetchTimeline } = await import('./services/photos')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchTimelineMock = vi.mocked(fetchTimeline)

const EMPTY_PAGE: PhotoListResponse = {
  photos: [],
  total: 0,
  limit: 100,
  offset: 0,
  next_offset: null,
}
const EMPTY_TIMELINE: Timeline = { buckets: [], total: 0 }

/** A signed-in viewer: enough to pass `RequireAuth` on the library route. */
const viewerAuth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  downloadToken: null,
  canWrite: false,
  isAdmin: false,
  isMaintainer: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

/**
 * A signed-in admin: every governance power, but not the top of the ladder — the
 * operations routes (`/system` among them) stay closed. Used to pin down who the
 * footer's job-queue badges may link there.
 */
const adminAuth = {
  ...viewerAuth,
  user: { uid: 'u2', username: 'a', display_name: 'A', role: 'admin' },
  role: 'admin',
  canWrite: true,
  isAdmin: true,
  isMaintainer: false,
} as unknown as AuthContextValue

/** Nobody signed in — what a visitor arriving at a public route looks like. */
const unauthenticated = {
  ...viewerAuth,
  status: 'unauthenticated',
  user: null,
  role: null,
  canWrite: false,
  isAdmin: false,
  isMaintainer: false,
} as unknown as AuthContextValue

/** Surfaces the resolved location and offers a Back control. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="pathname">{pathname}</span>
      <span data-testid="search">{search}</span>
      <button
        type="button"
        onClick={() => {
          void navigate(-1)
        }}
      >
        __back
      </button>
    </>
  )
}

/** Mounts the real route table at `entries[index]`, signed in as `auth`. */
function renderRoutes(entries: string[], index = entries.length - 1, auth = viewerAuth) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={entries} initialIndex={index}>
          <AppRoutes />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchPhotosMock.mockResolvedValue(EMPTY_PAGE)
  fetchTimelineMock.mockReset()
  fetchTimelineMock.mockResolvedValue(EMPTY_TIMELINE)
})

describe('routing', () => {
  it('renders the photo library at the root route', async () => {
    renderRoutes(['/'])

    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
    expect(fetchPhotosMock).toHaveBeenCalled()
  })

  it('renders the registration page at /register without a session', async () => {
    // Registration is public by definition: nobody has an account yet, so the
    // route must resolve outside RequireAuth rather than bouncing to /login.
    renderRoutes(['/register'], 0, unauthenticated)

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Registration' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/register')
  })

  it('renders the password-reset landing page at /password-reset/:token without a session', async () => {
    // Whoever follows a reset link is locked out of the very account it belongs
    // to, so the route must resolve outside RequireAuth like registration does.
    renderRoutes(['/password-reset/tok-123'], 0, unauthenticated)

    expect(
      await screen.findByRole('heading', { level: 1, name: 'New password' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/password-reset/tok-123')
  })

  it('renders the help page at /help for any authenticated user', async () => {
    // The help route carries no role guard, so a plain viewer reaches it.
    renderRoutes(['/help'])

    expect(await screen.findByRole('heading', { level: 1, name: 'Help' })).toBeInTheDocument()
  })

  it('renders the library statistics at /stats for any authenticated user', async () => {
    // Like /help, the statistics route carries no role guard: a viewer reaches it.
    renderRoutes(['/stats'])

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Library statistics' }),
    ).toBeInTheDocument()
  })

  it('renders the own activity at /account/activity for any authenticated user', async () => {
    // Reading one's own actions is self-repair, not governance: no role guard,
    // so a plain viewer reaches it — unlike the admin-only /audit next door.
    renderRoutes(['/account/activity'])

    expect(
      await screen.findByRole('heading', { level: 1, name: 'My activity' }),
    ).toBeInTheDocument()
  })

  it('explains the refusal on a fullscreen route a viewer may not enter', async () => {
    // /review is editors-only and lives outside Layout. A viewer who typed it —
    // or followed a shared link — used to land in the library with no word of
    // explanation; now the route itself says why, and the address survives.
    renderRoutes(['/review'])

    expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/editor role/i)
    expect(screen.getByTestId('pathname')).toHaveTextContent('/review')
  })

  it('explains the refusal inside the shell for a viewer on /upload', async () => {
    renderRoutes(['/upload'])

    expect(await screen.findByTestId('forbidden-page')).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/upload')
    // The library never loads behind the refusal — the guard replaced the route,
    // it did not navigate away from it.
    expect(fetchPhotosMock).not.toHaveBeenCalled()
  })

  it('keeps /system for maintainers, the audience the footer queue badge links there', async () => {
    // The footer's job-queue badges are a link to /system and are rendered for
    // `isMaintainer` only. This is the other end of that promise: an admin — the
    // nearest role below — is refused here, so nobody is offered a link to a page
    // that would answer them with a refusal.
    renderRoutes(['/system'], 0, adminAuth)

    expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/maintainer role/i)
    expect(screen.getByTestId('pathname')).toHaveTextContent('/system')
  })

  it('lets a viewer reach the share landing, which explains itself', async () => {
    // The share target sits outside the editor gate on purpose: a viewer must
    // be told their shared photos cannot be uploaded (and have them discarded),
    // not handed the generic refusal of a route they never typed.
    renderRoutes(['/share-target?share=abc'])

    expect(await screen.findByTestId('share-target-page')).toBeInTheDocument()
    expect(screen.queryByTestId('forbidden-page')).not.toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/share-target')
  })

  it('leaves Back pointing at wherever the refused user came from', async () => {
    const user = userEvent.setup()
    renderRoutes(['/nowhere', '/duplicates'])

    expect(await screen.findByTestId('forbidden-page')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '__back' }))

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/nowhere')
    })
  })

  it('redirects /library?year=2024 to /?year=2024, preserving the query', async () => {
    renderRoutes(['/library?year=2024'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent('?year=2024')
    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
  })

  it('redirects a bare /library to the bare root route', async () => {
    renderRoutes(['/library'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })
    expect(screen.getByTestId('search')).toBeEmptyDOMElement()
  })

  it('redirects the inherited PhotoPrism login address to the library', async () => {
    // The instance took over the domain PhotoPrism used to serve, and PhotoPrism
    // kept its whole UI under `/library/…` — so browser history, bookmarks and
    // address-bar autocomplete still aim there. `/library/login` is the one
    // returning users land on first, and it used to greet them with the 404 page
    // *after* a successful sign-in (the guard faithfully returns you to the
    // address you asked for).
    renderRoutes(['/library/login'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })
    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
  })

  it('redirects a deep PhotoPrism link of any shape, query and all', async () => {
    renderRoutes(['/library/albums/at9lxuqxpogaaba1?q=svatba'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent('?q=svatba')
  })

  it('names the browser tab after the route, and re-names it on navigation', async () => {
    // Browser history is how "the photo I saw last week" is found again, so each
    // entry has to be labelled with the page it is — and, crucially, stop being
    // labelled with it the moment the reader moves on.
    const user = userEvent.setup()
    renderRoutes(['/', '/stats'])

    await screen.findByRole('heading', { level: 1, name: 'Library statistics' })
    expect(document.title).toBe('Library statistics · Kukátko')

    await user.click(screen.getByRole('button', { name: '__back' }))

    await screen.findByRole('heading', { name: 'Library' })
    expect(document.title).toBe('Library · Kukátko')
  })

  it('replaces the /library history entry so Back does not bounce', async () => {
    const user = userEvent.setup()
    // `/nowhere` renders the 404 page: a static previous entry to go Back to.
    renderRoutes(['/nowhere', '/library?year=2024'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })

    await user.click(screen.getByRole('button', { name: '__back' }))

    // Back skips the retired route entirely instead of redirecting forward again.
    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/nowhere')
    })
  })
})
