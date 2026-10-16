import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { type InitialEntry, MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppRoutes } from './App'
import { AuthContext, type AuthContextValue } from './auth/AuthContext'
import i18n from './i18n'
import { ProjectProvider } from './project/ProjectProvider'
import type { ProjectMembership, ProjectRole } from './services/auth'
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

// The public invitation route asks what the invite grants; stub the call (never
// resolving) so the route test exercises the wiring, not the network.
vi.mock('./services/invites', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/invites')>()
  return { ...actual, fetchPublicInvite: vi.fn(() => new Promise(() => undefined)) }
})

// The public registration route asks the instance whether registration is open,
// and the shell's first-run welcome asks for the administrator's greeting; stub
// both so the route tests exercise the wiring, not the network. The greeting
// never resolves, which keeps the welcome closed over every routed page.
vi.mock('./services/settings', () => ({
  fetchPublicSettings: vi.fn(() =>
    Promise.resolve({ passkeys_enabled: false, mail_enabled: true }),
  ),
  fetchProjectPublicSettings: vi.fn(() =>
    Promise.resolve({ project_name: 'Rodina', registration_enabled: true }),
  ),
  fetchWelcomeMarkdown: vi.fn(() => new Promise(() => undefined)),
  fetchFooterText: vi.fn(() => Promise.resolve('')),
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

// The notification deeplink page reads its notification and marks it read on
// mount; stub both so the route tests exercise the wiring, not the network.
vi.mock('./services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/projects')>()
  return { ...actual, fetchAdminProjects: vi.fn() }
})
// The members page lists the project's members on mount; it never needs to answer.
vi.mock('./services/members', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/members')>()
  return { ...actual, fetchMembers: vi.fn(() => new Promise(() => undefined)) }
})
vi.mock('./services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/notifications')>()
  return {
    ...actual,
    fetchNotification: vi.fn(() =>
      Promise.resolve({
        uid: 'nt1',
        kind: 'tagged',
        title: 'You were tagged in 2 photos',
        body: '',
        link: '/n/nt1',
        created_at: '2026-09-20T10:00:00Z',
        read_at: null,
        photos: [],
        total_count: 0,
        dropped_count: 0,
      }),
    ),
    markNotificationRead: vi.fn(() => new Promise(() => undefined)),
  }
})

// An old entity link asks the resolver which project it belongs to; unless a
// test says otherwise, the answer is the bootstrap project every fixture is in.
vi.mock('./services/resolve', () => ({
  resolveEntityProject: vi.fn(() => Promise.resolve('v')),
}))

const { fetchPhotos, fetchTimeline } = await import('./services/photos')
const { resolveEntityProject } = await import('./services/resolve')
const resolveMock = vi.mocked(resolveEntityProject)
const { fetchAdminProjects } = await import('./services/projects')
const adminProjectsMock = vi.mocked(fetchAdminProjects)
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

/** The account's membership of the bootstrap project `v`, with `role` in it. */
function memberOfV(role: ProjectRole): ProjectMembership {
  return { uid: 'pv', slug: 'v', name: 'Veselice', active: true, role, approved: true }
}

/** A signed-in viewer: enough to pass `RequireAuth` on the library route. */
const viewerAuth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  instanceRole: 'user',
  projects: [memberOfV('viewer')],
  lastProjectSlug: 'v',
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
 * footer's job-queue status line may link there.
 */
const adminAuth = {
  ...viewerAuth,
  user: { uid: 'u2', username: 'a', display_name: 'A', role: 'admin' },
  role: 'admin',
  projects: [memberOfV('admin')],
  canWrite: true,
  isAdmin: true,
  isMaintainer: false,
} as unknown as AuthContextValue

/**
 * A signed-in curator: people, faces, albums, labels, review and upload, but
 * none of the editor's access to the photos themselves.
 */
const curatorAuth = {
  ...viewerAuth,
  user: { uid: 'u3', username: 'c', display_name: 'C', role: 'curator' },
  role: 'curator',
  projects: [memberOfV('curator')],
  canCurate: true,
  canWrite: false,
} as unknown as AuthContextValue

/** Nobody signed in — what a visitor arriving at a public route looks like. */
const unauthenticated = {
  ...viewerAuth,
  status: 'unauthenticated',
  user: null,
  role: null,
  instanceRole: null,
  projects: [],
  lastProjectSlug: null,
  canWrite: false,
  isAdmin: false,
  isMaintainer: false,
} as unknown as AuthContextValue

/** Surfaces the resolved location and offers a Back control. */
function LocationProbe() {
  const { pathname, search, hash } = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="pathname">{pathname}</span>
      <span data-testid="search">{search}</span>
      <span data-testid="hash">{hash}</span>
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
function renderRoutes(entries: InitialEntry[], index = entries.length - 1, auth = viewerAuth) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={entries} initialIndex={index}>
          <ProjectProvider>
            <AppRoutes />
            <LocationProbe />
          </ProjectProvider>
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
  resolveMock.mockReset()
  resolveMock.mockResolvedValue('v')
  adminProjectsMock.mockReset()
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

  it("renders a project's registration page at /p/:slug/register without a session", async () => {
    // The one public page under /p/:slug: it must resolve outside RequireAuth
    // (and ahead of the guarded project routes) rather than bouncing to /login.
    renderRoutes(['/p/rodina/register'], 0, unauthenticated)

    expect(await screen.findByTestId('register-intro')).toHaveTextContent('Rodina')
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/rodina/register')
  })

  it('renders the upload-link page at /u/:code without a session', async () => {
    // Whoever holds an upload link may upload without an account, so the route
    // must resolve outside RequireAuth rather than bouncing to /login.
    renderRoutes(['/u/Ab3dEf7h'], 0, unauthenticated)

    expect(await screen.findByTestId('upload-link-page')).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/u/Ab3dEf7h')
  })

  it('renders the invitation page at /invite/:code without a session', async () => {
    // Whoever follows an invitation may have no account yet, so the route must
    // resolve outside RequireAuth and outside any project's layout.
    renderRoutes(['/invite/AB3DEF7H'], 0, unauthenticated)

    expect(await screen.findByRole('heading', { level: 1, name: 'Invitation' })).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/invite/AB3DEF7H')
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
    // /review is curators-and-up and lives outside Layout. A viewer who typed it —
    // or followed a shared link — used to land in the library with no word of
    // explanation; now the route itself says why, and the address survives.
    renderRoutes(['/review'])

    expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/curator role/i)
    expect(screen.getByTestId('pathname')).toHaveTextContent('/review')
  })

  it('gates the curation routes on the curator role', async () => {
    // A viewer is told the *curator* role is what is missing — proof the gate
    // sits at curator, not at editor where it used to be.
    for (const path of [
      '/review',
      '/albums/a1/faces',
      '/people/clusters',
      '/faces',
      '/expand',
      '/recognition',
      '/outliers',
      '/duplicate-markers',
      '/upload-links',
    ]) {
      const { unmount } = renderRoutes([`/p/v${path}`])
      expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/curator role/i)
      unmount()
    }
  })

  it('keeps duplicates and the trash behind the editor gate, out of a curator’s reach', async () => {
    for (const path of ['/duplicates', '/duplicates/compare', '/trash']) {
      const { unmount } = renderRoutes([`/p/v${path}`], 0, curatorAuth)
      expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/editor role/i)
      expect(screen.getByTestId('pathname')).toHaveTextContent(path)
      unmount()
    }
  })

  it('explains the refusal inside the shell for a viewer on a curation route', async () => {
    renderRoutes(['/upload-links'])

    expect(await screen.findByTestId('forbidden-page')).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/upload-links')
    // The library never loads behind the refusal — the guard replaced the route,
    // it did not navigate away from it.
    expect(fetchPhotosMock).not.toHaveBeenCalled()
  })

  it('lets a viewer upload: /upload is open to every signed-in role', async () => {
    renderRoutes(['/upload'])

    expect(await screen.findByRole('heading', { level: 1, name: 'Upload' })).toBeInTheDocument()
    expect(screen.queryByTestId('forbidden-page')).not.toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/upload')
  })

  it('keeps /system for maintainers, the audience the footer queue status links there', async () => {
    // The footer's job-queue status line is a link to /system, rendered for
    // `isMaintainer` only. This is the other end of that promise: an admin — the
    // nearest role below — is refused here, so nobody is offered a link to a page
    // that would answer them with a refusal.
    renderRoutes(['/system'], 0, adminAuth)

    expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/maintainer role/i)
    expect(screen.getByTestId('pathname')).toHaveTextContent('/system')
  })

  it('opens the system project pages to a maintainer, outside any project', async () => {
    const maintainerAuth = {
      ...adminAuth,
      role: 'maintainer',
      instanceRole: 'maintainer',
      isMaintainer: true,
      canImport: true,
    } as unknown as AuthContextValue
    const { fetchAdminProjects } = await import('./services/projects')
    vi.mocked(fetchAdminProjects).mockResolvedValue([])
    renderRoutes(['/system/projects'], 0, maintainerAuth)

    expect(await screen.findByRole('heading', { level: 1, name: 'Projects' })).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/system/projects')
  })

  it("lands a project admin signing in off a system administrator's page on its home", async () => {
    // The login page is handed the real guarded table: /system/projects is
    // maintainer-only there, so the stashed address falls back to the front
    // door, which forwards into the account's project — never "no access".
    renderRoutes(
      [{ pathname: '/login', state: { from: { pathname: '/system/projects' } } }],
      0,
      adminAuth,
    )

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })
    expect(screen.queryByTestId('forbidden-page')).not.toBeInTheDocument()
  })

  it('returns a signed-in member to its own project page from the login screen', async () => {
    renderRoutes(
      [{ pathname: '/login', state: { from: { pathname: '/p/v/albums', search: '?sort=name' } } }],
      0,
      adminAuth,
    )

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v\/albums$/)
    })
  })

  it('keeps the system project and account pages from an admin', async () => {
    for (const path of ['/system/projects', '/system/projects/v', '/system/users']) {
      const { unmount } = renderRoutes([path], 0, adminAuth)
      expect(await screen.findByTestId('forbidden-page')).toHaveTextContent(/maintainer role/i)
      expect(screen.getByTestId('pathname')).toHaveTextContent(path)
      unmount()
    }
  })

  it('forwards a viewer’s share from the share landing to the upload page', async () => {
    // Every signed-in role may upload, so a viewer's shared photos go where
    // anybody's do: to /upload, carrying the staged share.
    renderRoutes(['/share-target?share=abc'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/upload')
    })
    expect(screen.queryByTestId('forbidden-page')).not.toBeInTheDocument()
  })

  it('asks a member of two projects where a share goes instead of using the last one', async () => {
    // The share sheet posts to the unprefixed /share-target; it used to be
    // forwarded into the last-used project, which is how family photos ended
    // up in the other library.
    const twoProjects = {
      ...viewerAuth,
      projects: [
        memberOfV('viewer'),
        { uid: 'pr', slug: 'rodina', name: 'Rodina', active: true, role: 'viewer', approved: true },
      ],
    } as unknown as AuthContextValue
    renderRoutes(['/share-target?share=abc'], 0, twoProjects)

    expect(
      await screen.findByRole('heading', { name: 'Where should the shared photos go?' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/share-target')
    expect(screen.getByRole('link', { name: 'Rodina' })).toHaveAttribute(
      'href',
      '/p/rodina/upload?share=abc',
    )
  })

  it('lets a viewer reach the share landing when nothing was staged', async () => {
    renderRoutes(['/share-target'])

    expect(await screen.findByTestId('share-target-page')).toBeInTheDocument()
    expect(screen.queryByTestId('forbidden-page')).not.toBeInTheDocument()
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

  it('redirects /library?year=2024 to /p/v?year=2024, preserving the query', async () => {
    renderRoutes(['/library?year=2024'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent('?year=2024')
    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
  })

  it('redirects a bare /library to the bare project root', async () => {
    renderRoutes(['/library'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
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
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })
    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
  })

  it('redirects a deep PhotoPrism link of any shape, query and all', async () => {
    renderRoutes(['/library/albums/at9lxuqxpogaaba1?q=svatba'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent('?q=svatba')
  })

  it('names the browser tab after the route, and re-names it on navigation', async () => {
    // Browser history is how "the photo I saw last week" is found again, so each
    // entry has to be labelled with the page it is — and, crucially, stop being
    // labelled with it the moment the reader moves on. Inside a project the
    // project's name rides along, so two libraries in two tabs are told apart.
    const user = userEvent.setup()
    renderRoutes(['/', '/stats'])

    await screen.findByRole('heading', { level: 1, name: 'Library statistics' })
    expect(document.title).toBe('Library statistics · Veselice · Kukátko')

    await user.click(screen.getByRole('button', { name: '__back' }))

    await screen.findByRole('heading', { name: 'Library' })
    expect(document.title).toBe('Library · Veselice · Kukátko')
  })

  it('replaces the /library history entry so Back does not bounce', async () => {
    const user = userEvent.setup()
    // `/nowhere` renders the 404 page: a static previous entry to go Back to.
    renderRoutes(['/nowhere', '/library?year=2024'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })

    await user.click(screen.getByRole('button', { name: '__back' }))

    // Back skips the retired route entirely instead of redirecting forward again.
    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/nowhere')
    })
  })

  it('renders a notification’s own page at the short /n/:uid deeplink', async () => {
    renderRoutes(['/n/nt1'])

    expect(
      await screen.findByRole('heading', { level: 1, name: 'You were tagged in 2 photos' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/n/nt1')
  })

  it('brings a signed-out push tap back to the notification after signing in', async () => {
    // A push tapped on a phone that has since been signed out: the guard bounces
    // to /login, and signing in must land on the notification, not the library.
    const tree = (auth: AuthContextValue) => (
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={auth}>
          <MemoryRouter initialEntries={['/n/nt1?from=push']}>
            <ProjectProvider>
              <AppRoutes />
              <LocationProbe />
            </ProjectProvider>
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>
    )
    const { rerender } = render(tree(unauthenticated))
    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/login')
    })

    rerender(tree(viewerAuth))

    expect(
      await screen.findByRole('heading', { level: 1, name: 'You were tagged in 2 photos' }),
    ).toBeInTheDocument()
    // The unprefixed push deeplink was forwarded into the reader's project.
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/v/n/nt1')
    expect(screen.getByTestId('search')).toHaveTextContent('?from=push')
  })
})

describe('routing — projects', () => {
  /** A signed-in viewer with no project at all: nowhere to forward to. */
  const homelessAuth = {
    ...viewerAuth,
    projects: [],
    lastProjectSlug: null,
  } as unknown as AuthContextValue

  it('forwards the front door into the only project', async () => {
    renderRoutes(['/'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v$/)
    })
    expect(await screen.findByRole('heading', { name: 'Library' })).toBeInTheDocument()
  })

  it('forwards an old address into the project, query string and hash intact', async () => {
    renderRoutes(['/photos/abc?x=1#h'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v\/photos\/abc$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent(/^\?x=1$/)
    expect(screen.getByTestId('hash')).toHaveTextContent(/^#h$/)
  })

  it('forwards the retired user list, prefixed or not, to the members page', async () => {
    for (const path of ['/users', '/p/v/users']) {
      const { unmount } = renderRoutes([path], 0, adminAuth)
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v\/members$/)
      })
      expect(await screen.findByRole('heading', { name: 'Members' })).toBeInTheDocument()
      unmount()
    }
  })

  it('sends an old address to the chooser when there is no project to forward to', async () => {
    // A list page: an entity page would ask the resolver, which knows best.
    renderRoutes(['/albums?x=1#h'], 0, homelessAuth)

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/projects$/)
    })
    expect(
      await screen.findByRole('heading', { name: 'Which library do you want to open?' }),
    ).toBeInTheDocument()
  })

  it('sends the front door to the chooser for an account without a project', async () => {
    renderRoutes(['/'], 0, homelessAuth)

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/projects$/)
    })
  })

  it('answers a project the reader does not belong to exactly like an unknown address', async () => {
    renderRoutes(['/p/rodina/albums'])

    expect(await screen.findByTestId('not-found-page')).toBeInTheDocument()
    // Rendered in place: the address is kept, nothing about the project leaks.
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/rodina/albums')
  })

  it('keeps an unknown address that is no old project page a plain 404', async () => {
    renderRoutes(['/nowhere/at/all'])

    expect(await screen.findByTestId('not-found-page')).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/nowhere/at/all')
  })

  it('opens an old entity link in the project holding the entity, not the last one', async () => {
    // Two projects, the last one used is v — but the task lives in rodina.
    const twoProjects: AuthContextValue = {
      ...viewerAuth,
      projects: [memberOfV('viewer'), { ...memberOfV('viewer'), uid: 'pr', slug: 'rodina' }],
    }
    resolveMock.mockResolvedValue('rodina')
    renderRoutes(['/tasks/tk1?c=2#x'], 0, twoProjects)

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/rodina\/tasks\/tk1$/)
    })
    expect(screen.getByTestId('search')).toHaveTextContent(/^\?c=2$/)
    expect(screen.getByTestId('hash')).toHaveTextContent(/^#x$/)
    expect(resolveMock).toHaveBeenCalledWith('task', 'tk1', expect.any(AbortSignal))
  })

  it('asks the resolver about each kind of entity page, and only those', async () => {
    const cases: [string, string, string][] = [
      ['/photos/ph1', 'photo', 'ph1'],
      ['/albums/al1/faces', 'album', 'al1'],
      ['/labels/lb1', 'label', 'lb1'],
      ['/people/su1/tree', 'subject', 'su1'],
      ['/n/nt1', 'notification', 'nt1'],
    ]
    for (const [path, kind, uid] of cases) {
      resolveMock.mockClear()
      const { unmount } = renderRoutes([path], 0, curatorAuth)
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent(`/p/v${path}`)
      })
      expect(resolveMock).toHaveBeenCalledWith(kind, uid, expect.any(AbortSignal))
      unmount()
    }

    // A list page — and the cluster review under /people — names no entity.
    for (const path of ['/albums', '/people/clusters']) {
      resolveMock.mockClear()
      const { unmount } = renderRoutes([path], 0, curatorAuth)
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent(`/p/v${path}`)
      })
      expect(resolveMock).not.toHaveBeenCalled()
      unmount()
    }
  })

  it('says not found for an old entity link the reader may not open', async () => {
    resolveMock.mockResolvedValue(null)
    renderRoutes(['/photos/foreign'])

    expect(await screen.findByTestId('not-found-page')).toBeInTheDocument()
    // Rendered in place: no project is guessed for the link.
    expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/photos\/foreign$/)
  })

  it('falls back on the entry project when the resolver cannot be asked', async () => {
    resolveMock.mockRejectedValue(new Error('resolve request failed: 429'))
    renderRoutes(['/photos/abc'])

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/p\/v\/photos\/abc$/)
    })
  })

  it('tells a member still waiting for approval so, in a neutral shell', async () => {
    const pending = {
      ...viewerAuth,
      projects: [
        memberOfV('viewer'),
        {
          uid: 'pr',
          slug: 'rodina',
          name: 'Rodina',
          active: true,
          role: 'viewer',
          approved: false,
        },
      ],
    } as unknown as AuthContextValue
    renderRoutes(['/p/rodina/albums'], 0, pending)

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Waiting for approval' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/membership of the project Rodina/)).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/rodina/albums')
    // Not the entry project's chrome: no library navigation, no project name.
    expect(screen.queryByRole('link', { name: 'Albums' })).not.toBeInTheDocument()
    expect(screen.queryByText('Veselice')).not.toBeInTheDocument()
  })

  it('draws no other project’s chrome around an unknown slug or an instance page', async () => {
    for (const path of ['/p/zzz', '/account']) {
      const { unmount } = renderRoutes([path])
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent(path)
      })
      // A neutral title, not the fallback project's name.
      expect(await screen.findByTestId('project-identity')).toHaveTextContent(/^Kukátko$/)
      expect(screen.queryByRole('link', { name: 'Albums' })).not.toBeInTheDocument()
      // The chrome names no project; the account page's own content may (its
      // "who am I" card has a row per project, each naming it).
      const bar = document.querySelector('nav.navbar')
      expect(bar).not.toBeNull()
      expect(within(bar as HTMLElement).queryByText('Veselice')).not.toBeInTheDocument()
      unmount()
    }
  })

  it('lets the maintainer into a deactivated project, with a banner saying so', async () => {
    const maintainer = {
      ...adminAuth,
      instanceRole: 'maintainer',
      isMaintainer: true,
      canImport: true,
    } as unknown as AuthContextValue
    adminProjectsMock.mockResolvedValue([
      {
        uid: 'pc',
        slug: 'stary',
        name: 'Starý',
        description: '',
        active: false,
        storage_prefix: '',
        max_file_bytes: null,
        max_total_bytes: null,
        members_count: 1,
        photos_count: 0,
        originals_bytes: 0,
        created_at: '2026-10-01T00:00:00Z',
        updated_at: '2026-10-01T00:00:00Z',
      },
    ])
    renderRoutes(['/p/stary/members'], 0, maintainer)

    expect(await screen.findByRole('heading', { name: 'Members' })).toBeInTheDocument()
    const banner = screen.getByTestId('deactivated-project-banner')
    expect(banner).toHaveTextContent('The project Starý is deactivated.')
    expect(screen.getByRole('link', { name: 'project settings' })).toHaveAttribute(
      'href',
      '/system/projects/stary',
    )
  })

  it('lists the projects on the chooser as links into each', async () => {
    renderRoutes(['/projects'])

    expect(await screen.findByRole('link', { name: 'Veselice' })).toHaveAttribute('href', '/p/v')
  })
})
