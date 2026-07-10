import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type AlbumCount, type LabelCount } from '../services/organize'
import { type FacesResponse } from '../services/people'
import { type PhotoDetail, type PhotoEdit, type PhotoListResponse } from '../services/photos'

import { PhotoDetailPage } from './PhotoDetailPage'

// Reused leaf components render their own data and (for Leaflet) need a real DOM;
// stub them so this suite stays focused on the detail page's own behaviour. Their
// own behaviour is covered by SimilarPhotos.test.tsx. The face overlay is *not*
// stubbed: this suite asserts the page renders exactly one image of the photo,
// which only means something with the real overlay mounted.
vi.mock('../components/library/SimilarPhotos', () => ({
  SimilarPhotos: ({ uid }: { uid: string }) => <div data-testid="similar" data-uid={uid} />,
}))
vi.mock('../components/map/LeafletMap', () => ({
  LeafletMap: () => <div data-testid="map" />,
}))

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return {
    ...actual,
    fetchPhoto: vi.fn(),
    fetchEdit: vi.fn(),
    saveEdit: vi.fn(),
    updatePhoto: vi.fn(),
    favoritePhoto: vi.fn(),
    fetchPhotos: vi.fn(),
  }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    addAlbumPhotos: vi.fn(),
    removeAlbumPhotos: vi.fn(),
    attachLabel: vi.fn(),
    detachLabel: vi.fn(),
  }
})

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchFaces: vi.fn(), assignFace: vi.fn() }
})

const { fetchPhoto, fetchEdit, saveEdit, updatePhoto, favoritePhoto, fetchPhotos } =
  await import('../services/photos')
const { fetchAlbums, fetchLabels, addAlbumPhotos, removeAlbumPhotos, attachLabel, detachLabel } =
  await import('../services/organize')
const { fetchFaces, assignFace } = await import('../services/people')
const fetchFacesMock = vi.mocked(fetchFaces)
const assignFaceMock = vi.mocked(assignFace)

const fetchPhotoMock = vi.mocked(fetchPhoto)
const fetchEditMock = vi.mocked(fetchEdit)
const saveEditMock = vi.mocked(saveEdit)
const updatePhotoMock = vi.mocked(updatePhoto)
const favoritePhotoMock = vi.mocked(favoritePhoto)
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchAlbumsMock = vi.mocked(fetchAlbums)
const fetchLabelsMock = vi.mocked(fetchLabels)
const addAlbumPhotosMock = vi.mocked(addAlbumPhotos)
const removeAlbumPhotosMock = vi.mocked(removeAlbumPhotos)
const attachLabelMock = vi.mocked(attachLabel)
const detachLabelMock = vi.mocked(detachLabel)

const NEUTRAL: PhotoEdit = { rotation: 0, brightness: 0, contrast: 0 }

function photo(overrides: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'b',
    file_hash: 'b',
    file_name: 'b.jpg',
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    taken_at: '2026-01-02T10:00:00Z',
    taken_at_source: 'exif',
    thumb_url: '/api/v1/photos/b/thumb/tile_500',
    download_url: '/api/v1/photos/b/download?original=true',
    title: 'Beach',
    description: 'A sunny day',
    notes: '',
    camera_make: 'Canon',
    camera_model: 'EOS R5',
    lens_model: 'RF 24-70',
    iso: 200,
    aperture: 2.8,
    exposure: '1/250',
    focal_length: 50,
    lat: 50.08,
    lng: 14.42,
    media_type: 'image',
    private: false,
    is_favorite: false,
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    files: [],
    albums: [{ uid: 'al_1', title: 'Holidays' }],
    labels: [{ uid: 'lb_1', name: 'Sunset' }],
    ...overrides,
  }
}

function listPhoto(uid: string): PhotoDetail {
  return photo({ uid, file_name: `${uid}.jpg`, title: uid })
}

/** A faces response carrying `faces` detections on photo `b`. */
function facesResponse(count: number): FacesResponse {
  return {
    photo_uid: 'b',
    width: 4000,
    height: 3000,
    orientation: 1,
    faces: Array.from({ length: count }, (_, index) => ({
      face_index: index,
      bbox: [0.1, 0.2, 0.3, 0.4] as [number, number, number, number],
      det_score: 0.9,
      action: 'create_marker' as const,
      suggestions: [{ subject_uid: 'su_a', subject_name: 'Alice', distance: 0.1, confidence: 0.9 }],
    })),
  }
}

function page(uids: string[]): PhotoListResponse {
  return {
    photos: uids.map(listPhoto),
    total: uids.length,
    limit: 100,
    offset: 0,
    next_offset: null,
  }
}

function albumCount(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function labelCount(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
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

/** Surfaces the current pathname so keyboard-navigation tests can assert routes. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="pathname">{location.pathname}</span>
}

function renderPage(canWrite = true, entry = '/photos/b?sort=oldest') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/photos/:uid" element={<PhotoDetailPage />} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  // The face-overlay toggle persists to localStorage; start every test from the
  // shipped default (overlay on).
  window.localStorage.removeItem('kukatko.faces.overlay')
  fetchFacesMock.mockResolvedValue(facesResponse(0))
  assignFaceMock.mockResolvedValue(undefined)
  fetchPhotoMock.mockResolvedValue(photo())
  fetchEditMock.mockResolvedValue(NEUTRAL)
  fetchPhotosMock.mockResolvedValue(page(['a', 'b', 'c']))
  fetchAlbumsMock.mockResolvedValue([albumCount('al_1', 'Holidays'), albumCount('al_2', 'Trips')])
  fetchLabelsMock.mockResolvedValue([labelCount('lb_1', 'Sunset'), labelCount('lb_2', 'Forest')])
  addAlbumPhotosMock.mockResolvedValue(['b'])
  removeAlbumPhotosMock.mockResolvedValue([])
  attachLabelMock.mockResolvedValue(undefined)
  detachLabelMock.mockResolvedValue(undefined)
  favoritePhotoMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PhotoDetailPage', () => {
  it('renders exactly one image of the photo when no face was detected', async () => {
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    await screen.findByText('No faces detected in this photo.')

    // The whole point of the rework: faces are an overlay on the single preview,
    // never a second copy of the image — and a photo with none only says so.
    expect(container.querySelectorAll('img')).toHaveLength(1)
    expect(container.querySelector('img')).toHaveAttribute('alt', 'Beach')
    expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    expect(screen.getByTestId('similar')).toHaveAttribute('data-uid', 'b')
  })

  it('exposes star rating and pick/reject flagging in the detail view', async () => {
    // These curation controls were removed from the grid/list tiles; the detail
    // view stays their home, reachable for keyboard and screen-reader users.
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    expect(screen.getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rate 5 of 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Pick' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument()
  })

  it('draws detected faces as an overlay on the single preview', async () => {
    fetchFacesMock.mockResolvedValue(facesResponse(2))
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    expect(await screen.findByTestId('face-overlay')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Unnamed face 2' })).toBeEnabled()
    // Still exactly one image: the boxes are drawn over it.
    expect(container.querySelectorAll('img')).toHaveLength(1)
    expect(screen.queryByText('No faces detected in this photo.')).not.toBeInTheDocument()
  })

  it('toggles the face overlay and remembers the choice', async () => {
    const user = userEvent.setup()
    fetchFacesMock.mockResolvedValue(facesResponse(1))
    renderPage()
    await screen.findByTestId('face-overlay')

    await user.click(screen.getByRole('button', { name: 'Hide faces' }))
    expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    // The choice is persisted, so it carries across photos and reloads.
    expect(window.localStorage.getItem('kukatko.faces.overlay')).toBe('false')

    await user.click(screen.getByRole('button', { name: 'Show faces' }))
    expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
    expect(window.localStorage.getItem('kukatko.faces.overlay')).toBe('true')
  })

  it('closes the naming panel when the overlay is hidden', async () => {
    const user = userEvent.setup()
    fetchFacesMock.mockResolvedValue(facesResponse(1))
    renderPage()
    await screen.findByTestId('face-overlay')

    await user.click(screen.getByRole('button', { name: 'Unnamed face 1' }))
    expect(screen.getByLabelText('Name this face')).toBeInTheDocument()

    // Hiding the boxes must not leave an orphaned panel for an invisible face.
    await user.click(screen.getByRole('button', { name: 'Hide faces' }))
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })

  it('keeps the technical detail collapsed on first render', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    const expander = screen.getByRole('button', { name: 'Technical details' })
    expect(expander).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()
    expect(screen.queryByText('ISO 200')).not.toBeInTheDocument()

    // One click brings the EXIF back.
    await user.click(expander)
    expect(expander).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('EOS R5')).toBeInTheDocument()
    expect(screen.getByText('ISO 200')).toBeInTheDocument()
  })

  it('stacks the media and panel columns full-width below the lg breakpoint', async () => {
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    // Both columns are `col-12` (full width, stacked) until `lg`, where they
    // split 7/5 side-by-side — so on phones and tablets the preview sits above
    // the metadata panel instead of squeezing into a narrow half-column.
    expect(container.querySelector('.col-12.col-lg-7')).not.toBeNull()
    expect(container.querySelector('.col-12.col-lg-5')).not.toBeNull()
  })

  it('plays a video with a range-streaming player instead of an image', async () => {
    fetchPhotoMock.mockResolvedValue(
      photo({ media_type: 'video', file_name: 'clip.mp4', file_mime: 'video/mp4', title: 'Clip' }),
    )
    const { container } = renderPage()

    await screen.findByRole('heading', { name: 'Clip' })
    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video?.getAttribute('src')).toContain('/photos/b/video')
    // No still <img> is rendered for the main preview of a video.
    expect(container.querySelector('img[alt="Clip"]')).toBeNull()
  })

  it('shows a live photo with a hold-to-play motion clip', async () => {
    fetchPhotoMock.mockResolvedValue(
      photo({ media_type: 'live', file_name: 'live.heic', title: 'Live' }),
    )
    const { container } = renderPage()

    await screen.findByRole('heading', { name: 'Live' })
    expect(screen.getByRole('button', { name: /Live/ })).toBeInTheDocument()
    const video = container.querySelector('video')
    expect(video?.getAttribute('src')).toContain('/photos/b/video')
  })

  it('offers prev/next that respect the list order and a Back link to the origin', async () => {
    renderPage(true, '/photos/b?sort=oldest&album=al_1')

    await screen.findByRole('heading', { name: 'Beach' })
    // The neighbour fetch uses the originating filter/sort + scope.
    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    expect(fetchPhotosMock.mock.calls[0][0]).toMatchObject({ sort: 'oldest', album: 'al_1' })

    const prev = await screen.findByRole('link', { name: 'Previous photo' })
    const next = await screen.findByRole('link', { name: 'Next photo' })
    expect(prev).toHaveAttribute('href', expect.stringContaining('/photos/a'))
    expect(next).toHaveAttribute('href', expect.stringContaining('/photos/c'))
    // Carries the originating query so order/scope survive navigation.
    expect(next.getAttribute('href')).toContain('sort=oldest')

    const back = screen.getByRole('link', { name: /Back/ })
    expect(back).toHaveAttribute('href', expect.stringContaining('/albums/al_1'))
  })

  it('edits metadata via the API and reflects the refreshed photo', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('button', { name: 'Edit' }))
    const titleInput = screen.getByLabelText('Title')
    await user.clear(titleInput)
    await user.type(titleInput, 'Sunset beach')

    updatePhotoMock.mockResolvedValue(photo({ title: 'Sunset beach' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updatePhotoMock).toHaveBeenCalled()
    })
    expect(updatePhotoMock.mock.calls[0][0]).toBe('b')
    expect(updatePhotoMock.mock.calls[0][1]).toMatchObject({ title: 'Sunset beach' })
    expect(await screen.findByRole('heading', { name: 'Sunset beach' })).toBeInTheDocument()
  })

  it('adds and removes album memberships inline', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    await waitFor(() => {
      expect(fetchAlbumsMock).toHaveBeenCalled()
    })

    // Add the photo to a non-member album via the autocomplete.
    await user.type(screen.getByRole('combobox', { name: 'Add to album' }), 'trips')
    await user.click(await screen.findByRole('option', { name: 'Trips' }))
    await waitFor(() => {
      expect(addAlbumPhotosMock).toHaveBeenCalledWith('al_2', ['b'])
    })
    expect(await screen.findByText('Trips')).toBeInTheDocument()

    // Remove an existing membership.
    await user.click(screen.getByRole('button', { name: 'Remove from album Holidays' }))
    await waitFor(() => {
      expect(removeAlbumPhotosMock).toHaveBeenCalledWith('al_1', ['b'])
    })
  })

  it('adds and removes label memberships inline', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    await waitFor(() => {
      expect(fetchLabelsMock).toHaveBeenCalled()
    })

    await user.type(screen.getByRole('combobox', { name: 'Add label' }), 'forest')
    await user.click(await screen.findByRole('option', { name: 'Forest' }))
    await waitFor(() => {
      expect(attachLabelMock).toHaveBeenCalledWith('lb_2', 'b')
    })

    await user.click(screen.getByRole('button', { name: 'Remove label Sunset' }))
    await waitFor(() => {
      expect(detachLabelMock).toHaveBeenCalledWith('lb_1', 'b')
    })
  })

  it('toggles the per-user favorite', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('button', { name: 'Add to favorites' }))
    await waitFor(() => {
      expect(favoritePhotoMock).toHaveBeenCalledWith('b', true)
    })
  })

  it('writes a non-destructive edit and reflects it in the preview', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('tab', { name: 'Edit' }))
    // Rotating updates the live edit preview immediately.
    await user.click(screen.getByRole('button', { name: 'Rotate right' }))
    const editPreview = screen.getByLabelText('Edit preview')
    expect(editPreview).toHaveStyle({ transform: 'rotate(90deg)' })

    saveEditMock.mockResolvedValue({ ...NEUTRAL, rotation: 90 })
    await user.click(screen.getByRole('button', { name: 'Save edits' }))

    await waitFor(() => {
      expect(saveEditMock).toHaveBeenCalled()
    })
    expect(saveEditMock.mock.calls[0][1]).toMatchObject({ rotation: 90 })
    // The main preview now reflects the saved edit.
    const main = screen.getByRole('img', { name: 'Beach' })
    await waitFor(() => {
      expect(main).toHaveStyle({ transform: 'rotate(90deg)' })
    })
  })

  it('pages to the next photo with the right arrow key', async () => {
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    // Neighbours must be resolved before the arrow can navigate.
    await screen.findByRole('link', { name: 'Next photo' })

    fireEvent.keyDown(document, { key: 'ArrowRight' })

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
    })
  })

  it('toggles the favorite with the f key', async () => {
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    fireEvent.keyDown(document, { key: 'f' })

    await waitFor(() => {
      expect(favoritePhotoMock).toHaveBeenCalledWith('b', true)
    })
  })

  it('returns to the source list with Escape', async () => {
    renderPage(true, '/photos/b?sort=oldest')
    await screen.findByRole('heading', { name: 'Beach' })

    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => {
      // The library lives at the root route; anchor the match so `/photos/b`
      // (which also contains a slash) cannot pass for it.
      expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
    })
  })

  it('shows a read-only page to viewers', async () => {
    fetchFacesMock.mockResolvedValue(facesResponse(1))
    renderPage(false)
    await screen.findByRole('heading', { name: 'Beach' })

    // No edit affordances.
    expect(screen.queryByRole('tab', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Remove from album Holidays' }),
    ).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Add to album')).not.toBeInTheDocument()
    // Viewers see the faces drawn on the photo, but cannot select one to name it,
    // and album lists are not fetched.
    expect(await screen.findByTestId('face-overlay')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeDisabled()
    expect(fetchAlbumsMock).not.toHaveBeenCalled()
    // Chips are still shown (read-only).
    const organize = screen.getByText('Holidays').closest('div')
    expect(within(organize as HTMLElement).getByText('Holidays')).toBeInTheDocument()
  })

  describe('fullscreen lightbox', () => {
    it('opens the fullscreen viewer when the main image is clicked', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))

      const dialog = screen.getByRole('dialog', { name: 'Fullscreen photo viewer' })
      expect(within(dialog).getByRole('img').getAttribute('src')).toContain(
        '/photos/b/thumb/fit_1920',
      )
    })

    it('closes on the close button, the backdrop and Esc', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // Close button.
      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))
      await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Close' }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

      // Backdrop click.
      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))
      fireEvent.click(screen.getByRole('dialog'))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

      // Escape closes the viewer without navigating away from the photo.
      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))
      fireEvent.keyDown(document, { key: 'Escape' })
      await waitFor(() => {
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      })
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')
    })

    it('pages prev/next through the list and stops at the ends', async () => {
      const user = userEvent.setup()
      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })

      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))
      const dialog = screen.getByRole('dialog')

      // Neighbours resolve → the next arrow appears; the viewer shows photo b.
      const next = await within(dialog).findByRole('button', { name: 'Next photo' })
      expect(within(dialog).getByRole('img').getAttribute('src')).toContain('/photos/b/thumb')

      // Step to the last photo; the next arrow disappears at the end.
      await user.click(next)
      await waitFor(() => {
        expect(within(dialog).getByRole('img').getAttribute('src')).toContain('/photos/c/thumb')
      })
      await waitFor(() => {
        expect(within(dialog).queryByRole('button', { name: 'Next photo' })).not.toBeInTheDocument()
      })

      // Step back to the first photo; the prev arrow disappears at the start.
      await user.click(within(dialog).getByRole('button', { name: 'Previous photo' }))
      // The next arrow reappearing signals the neighbours of b have resolved.
      await within(dialog).findByRole('button', { name: 'Next photo' })
      expect(within(dialog).getByRole('img').getAttribute('src')).toContain('/photos/b/thumb')

      await user.click(within(dialog).getByRole('button', { name: 'Previous photo' }))
      await waitFor(() => {
        expect(within(dialog).getByRole('img').getAttribute('src')).toContain('/photos/a/thumb')
      })
      expect(
        within(dialog).queryByRole('button', { name: 'Previous photo' }),
      ).not.toBeInTheDocument()
    })

    it('restores the detail URL to the last-viewed photo on close', async () => {
      const user = userEvent.setup()
      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })

      await user.click(screen.getByRole('button', { name: 'Open fullscreen' }))
      const dialog = screen.getByRole('dialog')
      const next = await within(dialog).findByRole('button', { name: 'Next photo' })
      await user.click(next)
      await waitFor(() => {
        expect(within(dialog).getByRole('img').getAttribute('src')).toContain('/photos/c/thumb')
      })

      await user.click(within(dialog).getByRole('button', { name: 'Close' }))
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
    })

    it('does not open the image lightbox for a video', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({
          media_type: 'video',
          file_name: 'clip.mp4',
          file_mime: 'video/mp4',
          title: 'Clip',
        }),
      )
      renderPage()
      await screen.findByRole('heading', { name: 'Clip' })

      // Videos keep their own native fullscreen; there is no image-lightbox trigger.
      expect(screen.queryByRole('button', { name: 'Open fullscreen' })).not.toBeInTheDocument()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})
