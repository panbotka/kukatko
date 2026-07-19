import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { GRID_COLUMNS_MAX } from '../lib/gridDensity'
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
  // The grid density lives in localStorage; clear it so the seeded column count
  // is deterministic per test and one density test never leaks into another.
  window.localStorage.clear()
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

  it('scopes each photo tile to this subject so the viewer pages the person set', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    const tile = await screen.findByRole('link', { name: 'a.jpg' })
    // The detail link carries `person=<subjectUid>`, so opening the photo and
    // paging prev/next walks GET /photos?person=sj_1 — this person's photos —
    // rather than falling back to the whole library.
    expect(tile).toHaveAttribute('href', '/photos/a?person=sj_1')
  })

  it('keeps selection and bulk edit away from viewers', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Jana' })
    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('offers the on-page similarity-candidate section to editors', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(true)
    // Editors get the on-page candidate search (a write action gated exactly like the
    // outlier review below it); the expensive search waits for the button, not mount.
    expect(await screen.findByRole('button', { name: /Find suggestions/i })).toBeInTheDocument()
  })

  it('hides the similarity-candidate section from viewers', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.queryByRole('button', { name: /Find suggestions/i })).not.toBeInTheDocument()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    // No "Select" step: the gallery tile is a link that already carries its
    // checkmark, exactly as on the library.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
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
    await user.click(screen.getByRole('button', { name: 'Select b.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()
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
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
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

  it('drops the selection when another person is opened', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: 'next person' }))

    // The other person's gallery must not open onto this one's selection.
    await waitFor(() => {
      expect(screen.queryByText('1 selected')).not.toBeInTheDocument()
    })
    expect(fetchSubjectMock).toHaveBeenLastCalledWith('sj_2', expect.anything())
  })

  it('the set-cover action keeps working, and steps aside once a photo is picked', async () => {
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

    // Once something is picked the tile is a selection target, so the overlay
    // steps aside — but only then, not merely because the tile is selectable.
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.queryByRole('button', { name: 'Cover' })).not.toBeInTheDocument()
  })

  it('exposes the images-per-row control to every viewer and re-columns the grid', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    // A plain viewer: the density control is a view preference, not write-gated.
    renderPage(false)

    await screen.findByRole('link', { name: 'a.jpg' })
    // The shared stepper is present (its group carries the "Tiles per row" label).
    expect(screen.getByRole('group', { name: 'Tiles per row' })).toBeInTheDocument()

    const grid = document.querySelector('[data-density]')
    expect(grid).not.toBeNull()
    const before = Number(grid?.getAttribute('data-density'))

    // Stepping the control re-columns the very same grid: the density attribute
    // and the inline `grid-template-columns` both follow the shared state.
    if (before < GRID_COLUMNS_MAX) {
      await user.click(screen.getByRole('button', { name: 'More tiles per row' }))
      const after = document.querySelector('[data-density]')
      expect(after).toHaveAttribute('data-density', String(before + 1))
      expect(after).toHaveStyle({ gridTemplateColumns: `repeat(${before + 1}, 1fr)` })
    } else {
      await user.click(screen.getByRole('button', { name: 'Fewer tiles per row' }))
      const after = document.querySelector('[data-density]')
      expect(after).toHaveAttribute('data-density', String(before - 1))
      expect(after).toHaveStyle({ gridTemplateColumns: `repeat(${before - 1}, 1fr)` })
    }
  })

  it('offers set-cover as a quiet icon-only control that still calls the handler', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    updateSubjectMock.mockResolvedValue({ ...subject(), cover_photo_uid: 'a' })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    const cover = screen.getByRole('button', { name: 'Set as cover' })
    // Quiet: a hover/focus-revealed icon-only disc, not a loud always-on labelled
    // button — it carries no visible text, only the icon glyph.
    expect(cover).toHaveClass('kk-cover-btn')
    expect(cover.textContent).toBe('')

    // Behaviour is unchanged: the same handler still issues the cover PATCH.
    await user.click(cover)
    await waitFor(() => {
      expect(updateSubjectMock).toHaveBeenCalledWith(
        'sj_1',
        expect.objectContaining({ cover_photo_uid: 'a' }),
      )
    })
  })

  it('marks the current cover with a filled indicator on its tile', async () => {
    fetchSubjectMock.mockResolvedValue({ ...subject(), cover_photo_uid: 'a' })
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    const current = screen.getByRole('button', { name: 'Cover' })
    // The current cover reads as such: a filled disc, and inert so it cannot be
    // re-set onto itself.
    expect(current).toHaveClass('kk-cover-btn--on')
    expect(current).toBeDisabled()
    expect(current.querySelector('.bi-image-fill')).not.toBeNull()

    // Every other photo still offers the plain (settable) affordance.
    expect(screen.getByRole('button', { name: 'Set as cover' })).toBeInTheDocument()
  })
})
