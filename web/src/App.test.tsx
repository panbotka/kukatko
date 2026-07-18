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

/** Mounts the real route table at `entries[index]`, signed in as a viewer. */
function renderRoutes(entries: string[], index = entries.length - 1) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={viewerAuth}>
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
