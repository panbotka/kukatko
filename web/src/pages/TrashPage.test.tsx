import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { TrashPage } from './TrashPage'

// Mock only the network functions, keeping the real types/helpers (thumbUrl, …).
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return {
    ...actual,
    fetchPhotos: vi.fn(),
    fetchTrashInfo: vi.fn(),
    unarchivePhoto: vi.fn(),
    purgePhoto: vi.fn(),
    emptyTrash: vi.fn(),
  }
})

const { fetchPhotos, fetchTrashInfo, unarchivePhoto, purgePhoto, emptyTrash } =
  await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)
const infoMock = vi.mocked(fetchTrashInfo)
const unarchiveMock = vi.mocked(unarchivePhoto)
const purgeMock = vi.mocked(purgePhoto)
const emptyMock = vi.mocked(emptyTrash)

const DAY = 24 * 60 * 60 * 1000

// photo builds a minimal archived Photo; archivedDaysAgo drives the countdown.
function photo(uid: string, name: string, archivedDaysAgo: number): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: name,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    archived_at: new Date(Date.now() - archivedDaysAgo * DAY).toISOString(),
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[]): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null }
}

// auth builds an AuthContext value. The trash is editor-visible, but its purge
// controls (empty, delete forever) are admin-or-higher, so the flag drives them.
function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'editor'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canWrite: true,
    isAdmin,
    isMaintainer: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderTrash(value: AuthContextValue = auth(true)) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/trash']}>
          <TrashPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  infoMock.mockReset()
  unarchiveMock.mockReset()
  purgeMock.mockReset()
  emptyMock.mockReset()
  infoMock.mockResolvedValue({ retention_days: 30 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TrashPage', () => {
  it('scopes the listing to archived photos only', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 1)]))
    renderTrash()
    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchMock.mock.calls[0][0].archived).toBe('only')
  })

  it('renders the auto-purge countdown from the retention window', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 28)]))
    renderTrash()
    // 30-day retention, archived 28 days ago → ~2 days left.
    expect(await screen.findByText('2d left')).toBeInTheDocument()
  })

  it('restores a photo and reloads the list', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 1)]))
    unarchiveMock.mockResolvedValue()
    renderTrash()
    await screen.findByRole('link', { name: 'a.jpg' })

    await user.click(screen.getByRole('button', { name: 'Restore' }))

    await waitFor(() => {
      expect(unarchiveMock).toHaveBeenCalledWith('a')
    })
    // A reload re-queries the list (initial load + post-restore reload).
    await waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2)
    })
  })

  it('permanently deletes a photo only after confirmation', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 1)]))
    purgeMock.mockResolvedValue()
    renderTrash()
    await screen.findByRole('link', { name: 'a.jpg' })

    await user.click(screen.getByRole('button', { name: 'Delete forever' }))
    // The mutation does not fire until the dialog is confirmed.
    expect(purgeMock).not.toHaveBeenCalled()

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete forever' }))

    await waitFor(() => {
      expect(purgeMock).toHaveBeenCalledWith('a')
    })
  })

  it('empties the trash after confirmation', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 1)]))
    emptyMock.mockResolvedValue({ purged: 1, failed: 0 })
    renderTrash()
    await screen.findByRole('link', { name: 'a.jpg' })

    await user.click(screen.getByRole('button', { name: 'Empty trash' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete forever' }))

    await waitFor(() => {
      expect(emptyMock).toHaveBeenCalled()
    })
  })

  it('shows an empty state when the trash has no photos', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderTrash()
    expect(await screen.findByText('Trash is empty')).toBeInTheDocument()
  })

  it('withholds every purge control from a non-admin editor', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg', 1)]))
    renderTrash(auth(false))
    await screen.findByRole('link', { name: 'a.jpg' })

    // Restore stays — an editor curates the trash — but purging is admin-only:
    // no Empty trash, and no per-card Delete forever.
    expect(screen.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Empty trash' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete forever' })).not.toBeInTheDocument()
  })
})
