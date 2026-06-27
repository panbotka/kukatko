import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Album, type AlbumCount } from '../services/organize'

import { AlbumsPage } from './AlbumsPage'

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
    order_by: 'added',
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

  it('shows the empty state when there are no albums', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No albums yet')).toBeInTheDocument()
  })

  it('creates an album: calls the API and adds it to the grid', async () => {
    fetchMock.mockResolvedValue([])
    const created: Album = {
      uid: 'al_new',
      slug: 'trip',
      title: 'Trip',
      description: '',
      type: 'album',
      private: false,
      order_by: 'added',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
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
  })

  it('hides the create control from viewers', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays')])
    renderPage(false)
    await screen.findByText('Holidays')
    expect(screen.queryByRole('button', { name: 'New album' })).not.toBeInTheDocument()
  })
})
