import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Subject } from '../services/people'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { SubjectPage } from './SubjectPage'

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return {
    ...actual,
    fetchSubject: vi.fn(),
    fetchSubjectPhotos: vi.fn(),
    updateSubject: vi.fn(),
    fetchOutliers: vi.fn(),
  }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchSubject, fetchSubjectPhotos, updateSubject, fetchOutliers } =
  await import('../services/people')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const fetchSubjectMock = vi.mocked(fetchSubject)
const fetchPhotosMock = vi.mocked(fetchSubjectPhotos)
const updateSubjectMock = vi.mocked(updateSubject)
const outliersMock = vi.mocked(fetchOutliers)
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

function photo(uid: string, name: string): Photo {
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
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[]): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null }
}

function subject(): Subject {
  return {
    uid: 'sj_1',
    slug: 'jana',
    name: 'Jana',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

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

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={['/people/sj_1']}>
          {/* The link stands in for any navigation to another person: the route
              is the same, so React Router keeps this very page mounted. */}
          <Link to="/people/sj_2">next person</Link>
          <Routes>
            <Route path="/people/:uid" element={<SubjectPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchSubjectMock.mockReset()
  fetchPhotosMock.mockReset()
  updateSubjectMock.mockReset()
  outliersMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  fetchSubjectMock.mockResolvedValue(subject())
  outliersMock.mockResolvedValue({
    subject_uid: 'sj_1',
    count: 0,
    meaningful: false,
    avg_distance: 0,
    no_embedding: 0,
    faces: [],
  })
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SubjectPage', () => {
  it('offers a back link that names the people list it returns to', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    const back = await screen.findByRole('link', { name: 'Back to people' })
    expect(back).toHaveAttribute('href', '/people')
  })

  it('keeps selection and bulk edit away from viewers', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Jana' })
    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.queryByRole('button', { name: 'Select' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('disables the bulk-edit trigger until a photo is picked', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))

    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('bulk-edits exactly the picked photos, then reloads the gallery', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'b.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    expect(screen.getByText('0 selected')).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('a failed bulk edit keeps the selection and shows the reason', async () => {
    const { ApiError } = await import('../services/auth')
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    bulkMock.mockRejectedValue(new ApiError(400, 'archive and unarchive are mutually exclusive'))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(
      await screen.findByText('archive and unarchive are mutually exclusive'),
    ).toBeInTheDocument()
    // The dialog's own Cancel, not the selection bar's: dismissing the failed
    // apply must leave the picked photo selected for a retry.
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
  })

  it('leaves selection mode when another person is opened', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: 'next person' }))

    // The other person's gallery must not open onto this one's selection.
    await waitFor(() => {
      expect(screen.queryByText('1 selected')).not.toBeInTheDocument()
    })
    expect(fetchSubjectMock).toHaveBeenLastCalledWith('sj_2', expect.anything())
  })

  it('the set-cover action keeps working, and steps aside in selection mode', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    updateSubjectMock.mockResolvedValue({ ...subject(), cover_photo_uid: 'a' })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Set as cover' }))

    await waitFor(() => {
      expect(updateSubjectMock).toHaveBeenCalledWith(
        'sj_1',
        expect.objectContaining({
          cover_photo_uid: 'a',
        }),
      )
    })
    expect(await screen.findByRole('button', { name: 'Cover' })).toBeInTheDocument()

    // In selection mode the tile is one selection target, so the overlay is gone.
    await user.click(screen.getByRole('button', { name: 'Select' }))
    expect(screen.queryByRole('button', { name: 'Cover' })).not.toBeInTheDocument()
  })
})
