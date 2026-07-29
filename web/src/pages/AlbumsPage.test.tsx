import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type TileGridLayout } from '../components/TileGrid'
import i18n from '../i18n'
import { type Album, type AlbumCount } from '../services/organize'

import { AlbumsPage } from './AlbumsPage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout, so the real
// one measures zero and mounts nothing). It renders every album through the real
// `List` component, which keeps the grid's own column template assertable.
interface MockGridProps {
  data: AlbumCount[]
  context: TileGridLayout
  components: { List: ComponentType<{ context: TileGridLayout; children: ReactNode }> }
  itemContent: (index: number, item: AlbumCount) => ReactNode
  computeItemKey: (index: number, item: AlbumCount) => string
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ components, context, data, itemContent, computeItemKey }: MockGridProps) => (
    <components.List context={context}>
      {data.map((item, index) => (
        <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
      ))}
    </components.List>
  ),
}))

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), createAlbum: vi.fn() }
})

const { fetchAlbums, createAlbum } = await import('../services/organize')
const fetchMock = vi.mocked(fetchAlbums)
const createMock = vi.mocked(createAlbum)

function album(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 3,
  }
}

/** Builds a minimal auth context value with the given write capability. */
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

function renderPage(canWrite = true, children?: ReactNode) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter>
          <AlbumsPage />
          {children}
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  createMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AlbumsPage', () => {
  it('lists albums with their photo counts', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays')])
    renderPage()
    expect(await screen.findByText('Holidays')).toBeInTheDocument()
    expect(screen.getByText('3 photos')).toBeInTheDocument()
  })

  it('renders the albums into a virtualized grid that reflows its columns', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays'), album('al_2', 'Alps')])
    renderPage()

    await screen.findByText('Holidays')
    const grid = document.querySelector<HTMLElement>('.kk-tile-grid')
    expect(grid).not.toBeNull()
    // The virtualized grid keeps the geometry of the plain one it replaced (and
    // of the loading skeleton): 160px-floor `auto-fill` tracks, so the column
    // count still follows the container width — one at 320px, two at 360px.
    expect(grid?.style.gridTemplateColumns).toBe('repeat(auto-fill, minmax(160px, 1fr))')
    expect(grid?.style.gap).toBe('12px')
    expect(screen.getAllByRole('link')).toHaveLength(2)
  })

  it('shows the empty state when there are no albums', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No albums yet')).toBeInTheDocument()
  })

  it('renders albums in the order the server returns, without re-sorting', async () => {
    // The server ranks by newest photo, so "Zebra" leading is a valid order that
    // any client-side sort by title would destroy.
    fetchMock.mockResolvedValue([album('al_2', 'Zebra'), album('al_1', 'Alps')])
    renderPage()

    await screen.findByText('Zebra')
    const titles = screen.getAllByRole('link').map((link) => link.getAttribute('aria-label'))
    expect(titles).toEqual(['Zebra', 'Alps'])
  })

  it('creates an album: calls the API and refetches the grid in server order', async () => {
    const created: Album = {
      uid: 'al_new',
      slug: 'trip',
      title: 'Trip',
      description: '',
      type: 'album',
      private: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    // The fresh album only reaches the grid through a refetch: the page never
    // appends it locally, because only the server knows where it ranks.
    fetchMock.mockResolvedValueOnce([]).mockResolvedValueOnce([{ ...created, photo_count: 0 }])
    createMock.mockResolvedValue(created)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('No albums yet')
    await user.click(screen.getByRole('button', { name: 'New album' }))
    await user.type(screen.getByLabelText('Title'), 'Trip')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith(expect.objectContaining({ title: 'Trip' }))
    })
    expect(await screen.findByText('Trip')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('hides the create control from viewers', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays')])
    renderPage(false)
    await screen.findByText('Holidays')
    expect(screen.queryByRole('button', { name: 'New album' })).not.toBeInTheDocument()
  })
})
