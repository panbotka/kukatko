import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
// stub them so this suite stays focused on the viewer's own behaviour. Their own
// behaviour is covered by their own suites. The face overlay is *not* stubbed:
// this suite asserts the viewer renders exactly one image of the photo, which only
// means something with the real overlay mounted.
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
    searchPhotos: vi.fn(),
    archivePhoto: vi.fn(),
    unarchivePhoto: vi.fn(),
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

const {
  fetchPhoto,
  fetchEdit,
  saveEdit,
  updatePhoto,
  favoritePhoto,
  fetchPhotos,
  searchPhotos,
  archivePhoto,
  unarchivePhoto,
} = await import('../services/photos')
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
const searchPhotosMock = vi.mocked(searchPhotos)
const archivePhotoMock = vi.mocked(archivePhoto)
const unarchivePhotoMock = vi.mocked(unarchivePhoto)
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

/** Surfaces the current location so keyboard-navigation tests can assert routes. */
function LocationProbe() {
  const location = useLocation()
  return (
    <>
      <span data-testid="pathname">{location.pathname}</span>
      <span data-testid="location">{`${location.pathname}${location.search}`}</span>
    </>
  )
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

/** Opens the metadata drawer (its contents are aria-hidden until it is). */
async function openInfo(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Info' }))
}

/** The root immersive-viewer element, for reading its data-* view flags. */
function viewer(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.kk-viewer')
  if (el === null) {
    throw new Error('viewer root not found')
  }
  return el
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  // The face-overlay toggle persists to localStorage; start every test from the
  // shipped default (overlay off — the photo is the content).
  window.localStorage.removeItem('kukatko.faces.overlay')
  fetchFacesMock.mockResolvedValue(facesResponse(0))
  assignFaceMock.mockResolvedValue(undefined)
  fetchPhotoMock.mockResolvedValue(photo())
  fetchEditMock.mockResolvedValue(NEUTRAL)
  fetchPhotosMock.mockResolvedValue(page(['a', 'b', 'c']))
  searchPhotosMock.mockResolvedValue(page(['a', 'b', 'c']))
  fetchAlbumsMock.mockResolvedValue([albumCount('al_1', 'Holidays'), albumCount('al_2', 'Trips')])
  fetchLabelsMock.mockResolvedValue([labelCount('lb_1', 'Sunset'), labelCount('lb_2', 'Forest')])
  addAlbumPhotosMock.mockResolvedValue(['b'])
  removeAlbumPhotosMock.mockResolvedValue([])
  attachLabelMock.mockResolvedValue(undefined)
  detachLabelMock.mockResolvedValue(undefined)
  favoritePhotoMock.mockResolvedValue(undefined)
  archivePhotoMock.mockResolvedValue(undefined)
  unarchivePhotoMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PhotoDetailPage — immersive viewer', () => {
  it('opens the photo full-bleed into a viewer, the image owning the screen', async () => {
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    // The whole point: one preview of the photo, full-bleed on the viewer stage,
    // never a second copy — and the default state is just the photo, no panel.
    const dialog = screen.getByRole('dialog', { name: 'Photo viewer' })
    expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
    expect(container.querySelectorAll('img')).toHaveLength(1)
    expect(within(dialog).getByRole('img', { name: 'Beach' }).getAttribute('src')).toContain(
      'fit_1920',
    )
    expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    // The similar-photos strip lives in the (mounted) drawer, keyed to this photo.
    expect(screen.getByTestId('similar')).toHaveAttribute('data-uid', 'b')
  })

  it('keeps the rating and personal-marking controls in the action bar', async () => {
    // These curation controls stay reachable in the chrome for keyboard and
    // screen-reader users; the favorite heart shares its toggle with the `f` key.
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    expect(screen.getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rate 5 of 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Eye' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Thumbs up' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Thumbs down' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
  })

  describe('archive control', () => {
    it('archives the open photo and swaps the control to Restore', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // An editor sees Archive; clicking it calls the archive service for this
      // photo and, on success, keeps the user on the page with the control now
      // offering Restore (the photo is in the trash).
      await user.click(screen.getByRole('button', { name: 'Archive' }))
      await waitFor(() => {
        expect(archivePhotoMock).toHaveBeenCalledWith('b')
      })
      expect(await screen.findByRole('button', { name: 'Restore' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    })

    it('hides the archive control from a viewer', async () => {
      // Archiving is an editor+ action; a viewer gets the read-only viewer.
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Restore' })).not.toBeInTheDocument()
    })

    it('shows Restore for an already-archived photo and unarchives on click', async () => {
      // Opened from the Trash page: the photo arrives archived, so the same
      // control leads with Restore and calls unarchive, swapping back to Archive.
      const user = userEvent.setup()
      fetchPhotoMock.mockResolvedValue(photo({ archived_at: '2026-01-01T00:00:00Z' }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Restore' }))
      await waitFor(() => {
        expect(unarchivePhotoMock).toHaveBeenCalledWith('b')
      })
      expect(await screen.findByRole('button', { name: 'Archive' })).toBeInTheDocument()
    })
  })

  describe('stage geometry', () => {
    it('stamps the figure with the photo’s display aspect ratio so the overlay lands true', async () => {
      // The figure is given the exact frame ratio inline, so its box IS the
      // rendered image (no letterbox for the percentage face boxes to drift into).
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const figure = container.querySelector<HTMLElement>('.kk-viewer__figure')
      expect(figure).not.toBeNull()
      expect(figure).toHaveAttribute('data-framed', 'true')
      expect(figure).toHaveStyle({ aspectRatio: '4000 / 3000' })
    })

    it('swaps the framed ratio for a quarter-turn EXIF orientation', async () => {
      // Orientations 5–8 rotate the image a quarter turn; the thumbnailer bakes
      // that in and markers live in that display space, so the figure must too.
      fetchPhotoMock.mockResolvedValue(
        photo({ file_width: 4000, file_height: 3000, file_orientation: 6 }),
      )
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(container.querySelector<HTMLElement>('.kk-viewer__figure')).toHaveStyle({
        aspectRatio: '3000 / 4000',
      })
    })
  })

  describe('metadata drawer', () => {
    it('is shut by default and slides in on demand, tracked in the URL', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // Shut: its contents are aria-hidden, so their controls are unreachable.
      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      expect(screen.queryByRole('button', { name: 'Technical details' })).not.toBeInTheDocument()

      await openInfo(user)
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      // Deep-linkable: the open state lives in a URL param so refresh/Back behave.
      expect(screen.getByTestId('location')).toHaveTextContent('info=1')
      expect(screen.getByRole('button', { name: 'Technical details' })).toBeInTheDocument()

      // Toggling again shuts it and drops the param.
      await openInfo(user)
      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      expect(screen.getByTestId('location')).not.toHaveTextContent('info=1')
    })

    it('toggles with the i key', async () => {
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 'i' })
      await waitFor(() => {
        expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      })
      fireEvent.keyDown(document, { key: 'i' })
      await waitFor(() => {
        expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      })
    })

    it('opens already showing when the URL asks for it (deep link / refresh)', async () => {
      const { container } = renderPage(true, '/photos/b?sort=oldest&info=1')
      await screen.findByRole('heading', { name: 'Beach' })

      expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      expect(screen.getByRole('button', { name: 'Technical details' })).toBeInTheDocument()
    })

    it('keeps the technical details collapsed until expanded', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      const expander = screen.getByRole('button', { name: 'Technical details' })
      expect(expander).toHaveAttribute('aria-expanded', 'false')
      expect(screen.queryByText('EOS R5')).not.toBeInTheDocument()

      await user.click(expander)
      expect(expander).toHaveAttribute('aria-expanded', 'true')
      expect(screen.getByText('EOS R5')).toBeInTheDocument()
      expect(screen.getByText('ISO 200')).toBeInTheDocument()
    })

    it('carries caption & place (with its map) ahead of the technical details', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      const caption = screen.getByText('Caption & place')
      const organize = screen.getByText('Organize')
      const technical = screen.getByRole('button', { name: 'Technical details' })
      expect(
        caption.compareDocumentPosition(organize) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy()
      expect(
        organize.compareDocumentPosition(technical) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy()
      // The location map is embedded in the caption & place block.
      expect(screen.getByTestId('map')).toBeInTheDocument()
    })
  })

  describe('faces', () => {
    it('hides the faces until asked, even on a photo full of them', async () => {
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      // The toggle proves the faces loaded — they are simply not drawn yet.
      await screen.findByRole('button', { name: 'Show faces' })

      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
      expect(screen.queryByText('Faces: 2')).not.toBeInTheDocument()
    })

    it('draws the boxes over the one preview and opens the naming panel with them', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      const { container } = renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))

      // Turning faces on opens the drawer alongside the boxes.
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'Unnamed face 2' })).toBeEnabled()
      // The panel lists the same faces.
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Select face #1' })).toBeInTheDocument()
      // Still exactly one *preview* — the boxes are drawn over it and the rows are
      // text. The people chips' crops are 24px images, so the invariant is counted
      // in full-size previews.
      const previews = [...container.querySelectorAll('img')].filter((img) =>
        img.getAttribute('src')?.includes('fit_1920'),
      )
      expect(previews).toHaveLength(1)
    })

    it('opens the faces panel on load when the stored preference asks for faces', async () => {
      // A remembered "show faces" must bring up the boxes AND their naming panel,
      // not leave the boxes over a shut drawer: the drawer opens itself on the
      // faces once they are known to be available, even without info in the URL.
      window.localStorage.setItem('kukatko.faces.overlay', 'true')
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      const { container } = renderPage(true, '/photos/b?sort=oldest')

      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      })
      expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()
      // Opening it pins the drawer state into the URL, so Back/refresh behave.
      expect(screen.getByTestId('location')).toHaveTextContent('info=1')
    })

    it('leaves the drawer shut when faces are preferred but the photo has none', async () => {
      // The preference cannot conjure a panel out of nothing: with no faces to
      // show, the drawer stays shut rather than opening on an empty face list.
      window.localStorage.setItem('kukatko.faces.overlay', 'true')
      fetchFacesMock.mockResolvedValue(facesResponse(0))
      const { container } = renderPage(true, '/photos/b?sort=oldest')

      await screen.findByRole('heading', { name: 'Beach' })
      // Give the faces fetch time to resolve before asserting the drawer is shut.
      await screen.findByRole('button', { name: 'Info' })
      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      expect(screen.getByTestId('location')).not.toHaveTextContent('info=1')
    })

    it('shows the faces on their own — activating them does not drag in the info panel', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))

      // The faces view: the boxes and their naming panel, and NOTHING of the
      // metadata ("Informace") — that belongs to the info view alone.
      expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()
      expect(screen.queryByText('Caption & place')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Technical details' })).not.toBeInTheDocument()
      expect(screen.queryByTestId('similar')).not.toBeInTheDocument()
    })

    it('the info button switches from the faces view to the metadata', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()

      // Info does not close on top of the faces — it swaps the drawer to the
      // metadata, dropping the boxes and the faces panel.
      await user.click(screen.getByRole('button', { name: 'Info' }))
      expect(screen.getByText('Caption & place')).toBeInTheDocument()
      expect(screen.queryByText('Faces: 2')).not.toBeInTheDocument()
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    })

    it('lights only the active view’s toggle — not the info button alongside faces', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))

      // Faces view: the faces toggle is pressed, the info toggle is NOT — the drawer
      // merely being open must not read as "info active".
      expect(screen.getByRole('button', { name: 'Hide faces' })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
      expect(screen.getByRole('button', { name: 'Info' })).toHaveAttribute('aria-pressed', 'false')

      // Switching to info flips it: info pressed, the faces toggle back to unpressed.
      await user.click(screen.getByRole('button', { name: 'Info' }))
      expect(screen.getByRole('button', { name: 'Info' })).toHaveAttribute('aria-pressed', 'true')
      expect(screen.getByRole('button', { name: 'Show faces' })).toHaveAttribute(
        'aria-pressed',
        'false',
      )
    })

    it('hiding the faces closes the drawer rather than revealing the metadata', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      const { container } = renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))
      // The faces view was open; turning them off shuts the drawer (data-panel), it
      // does not leave it open on the metadata — the info panel is a separate view.
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')

      await user.click(screen.getByRole('button', { name: 'Hide faces' }))
      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    })

    it('toggles the faces with the m key and remembers the choice', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(1))
      renderPage()
      await screen.findByRole('button', { name: 'Show faces' })

      fireEvent.keyDown(document, { key: 'm' })
      expect(await screen.findByTestId('face-overlay')).toBeInTheDocument()
      expect(window.localStorage.getItem('kukatko.faces.overlay')).toBe('true')

      fireEvent.keyDown(document, { key: 'm' })
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
      expect(window.localStorage.getItem('kukatko.faces.overlay')).toBe('false')

      // And the button does the same thing.
      await user.click(screen.getByRole('button', { name: 'Show faces' }))
      expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
    })

    it('closes the naming panel when the faces are hidden', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(1))
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))

      // The first unnamed face is selected for you, so the naming panel is up.
      expect(screen.getByLabelText('Name this face')).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Hide faces' }))
      expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
    })

    it('names a face reached from a person chip in the Organize block', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(1))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      // The chips answer "who is in this photo" with the faces still hidden; clicking
      // one brings up the faces at that face — the one place people are named.
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
      await user.click(await screen.findByRole('button', { name: 'Name unnamed face 1' }))
      expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
      expect(screen.getByLabelText('Name this face')).toBeInTheDocument()

      await user.type(screen.getByLabelText('Name'), 'Alice')
      await user.click(await screen.findByRole('option', { name: /Alice/ }))
      await waitFor(() => {
        expect(assignFaceMock).toHaveBeenCalled()
      })
    })
  })

  describe('media kinds', () => {
    it('plays a video with a range-streaming player instead of an image', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({
          media_type: 'video',
          file_name: 'clip.mp4',
          file_mime: 'video/mp4',
          title: 'Clip',
        }),
      )
      const { container } = renderPage()

      await screen.findByRole('heading', { name: 'Clip' })
      const video = container.querySelector('video')
      expect(video).not.toBeNull()
      expect(video?.getAttribute('src')).toContain('/photos/b/video')
      expect(container.querySelector('img[alt="Clip"]')).toBeNull()
    })

    it('shows a live photo with a hold-to-play motion clip', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({ media_type: 'live', file_name: 'live.heic', title: 'Live' }),
      )
      const { container } = renderPage()

      await screen.findByRole('heading', { name: 'Live' })
      expect(screen.getByRole('button', { name: /Live/ })).toBeInTheDocument()
      expect(container.querySelector('video')?.getAttribute('src')).toContain('/photos/b/video')
    })
  })

  describe('navigation', () => {
    it('offers prev/next that respect the list order and a close back to the origin', async () => {
      const user = userEvent.setup()
      renderPage(true, '/photos/b?sort=oldest&album=al_1')

      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(fetchPhotosMock).toHaveBeenCalled()
      })
      expect(fetchPhotosMock.mock.calls[0][0]).toMatchObject({ sort: 'oldest', album: 'al_1' })

      const prev = await screen.findByRole('link', { name: 'Previous photo' })
      const next = await screen.findByRole('link', { name: 'Next photo' })
      expect(prev).toHaveAttribute('href', expect.stringContaining('/photos/a'))
      expect(next).toHaveAttribute('href', expect.stringContaining('/photos/c'))
      expect(next.getAttribute('href')).toContain('sort=oldest')
      expect(next.getAttribute('href')).toContain('album=al_1')

      // Closing returns to the originating scoped list (album), carrying its state.
      await user.click(screen.getByRole('button', { name: 'Back to the list' }))
      await waitFor(() => {
        expect(screen.getByTestId('location')).toHaveTextContent('/albums/al_1')
      })
      expect(screen.getByTestId('location')).toHaveTextContent('sort=oldest')
    })

    it('pages prev/next within a subject when opened from a person gallery', async () => {
      // A subject-gallery tile links to /photos/b?person=su_a, so the viewer must
      // page GET /photos scoped to that person — this person's photos in the same
      // order the gallery shows — not the whole library.
      renderPage(true, '/photos/b?person=su_a')

      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(fetchPhotosMock).toHaveBeenCalled()
      })
      // The neighbours are paged from the person-scoped list, never the bare one.
      expect(fetchPhotosMock.mock.calls[0][0]).toMatchObject({ person: 'su_a' })

      const prev = await screen.findByRole('link', { name: 'Previous photo' })
      const next = await screen.findByRole('link', { name: 'Next photo' })
      expect(prev).toHaveAttribute('href', expect.stringContaining('/photos/a'))
      expect(next).toHaveAttribute('href', expect.stringContaining('/photos/c'))
      // The scope rides along so stepping keeps paging the subject set.
      expect(next.getAttribute('href')).toContain('person=su_a')
    })

    it('pages prev/next through the search and returns to it when opened from search', async () => {
      const user = userEvent.setup()
      renderPage(true, '/photos/b?q=beach&mode=semantic')

      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(searchPhotosMock).toHaveBeenCalled()
      })
      expect(fetchPhotosMock).not.toHaveBeenCalled()
      const [params, mode] = searchPhotosMock.mock.calls[0]
      expect(params.q).toBe('beach')
      expect(mode).toBe('semantic')

      const prev = await screen.findByRole('link', { name: 'Previous photo' })
      const next = await screen.findByRole('link', { name: 'Next photo' })
      expect(prev).toHaveAttribute('href', '/photos/a?q=beach&mode=semantic')
      expect(next).toHaveAttribute('href', '/photos/c?q=beach&mode=semantic')

      // Closing reconstructs the search URL (query + mode), not the library.
      await user.click(screen.getByRole('button', { name: 'Back to the list' }))
      await waitFor(() => {
        expect(screen.getByTestId('location')).toHaveTextContent('/search?q=beach&mode=semantic')
      })
    })

    it('carries the open drawer through prev/next so it stays open while paging', async () => {
      renderPage(true, '/photos/b?sort=oldest&info=1')
      await screen.findByRole('heading', { name: 'Beach' })

      const next = await screen.findByRole('link', { name: 'Next photo' })
      expect(next.getAttribute('href')).toContain('/photos/c')
      expect(next.getAttribute('href')).toContain('info=1')
    })

    it('pages to the next photo with the right arrow key', async () => {
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('link', { name: 'Next photo' })

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
    })

    it('keeps the current photo mounted while a neighbour loads, then swaps in place', async () => {
      let resolveNext!: (p: PhotoDetail) => void
      const pendingNext = new Promise<PhotoDetail>((resolve) => {
        resolveNext = resolve
      })
      fetchPhotoMock.mockImplementation((uid) =>
        uid === 'c' ? pendingNext : Promise.resolve(photo({ uid: 'b', title: 'Beach' })),
      )

      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })
      const beachImg = screen.getByRole('img', { name: 'Beach' })
      await screen.findByRole('link', { name: 'Next photo' })

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })

      // The previous photo's heading stays mounted — no full-screen loading flicker.
      expect(screen.getByRole('heading', { name: 'Beach' })).toBeInTheDocument()
      expect(screen.getByRole('img', { name: 'Beach' })).toBe(beachImg)

      resolveNext(photo({ uid: 'c', title: 'Cliff' }))
      expect(await screen.findByRole('heading', { name: 'Cliff' })).toBeInTheDocument()
      expect(screen.queryByRole('heading', { name: 'Beach' })).not.toBeInTheDocument()
    })

    it('cancels a superseded neighbour fetch so the latest target wins', async () => {
      const resolvers = new Map<string, (p: PhotoDetail) => void>()
      fetchPhotoMock.mockImplementation(
        (uid, signal) =>
          new Promise<PhotoDetail>((resolve, reject) => {
            resolvers.set(uid, resolve)
            signal?.addEventListener('abort', () => {
              reject(new DOMException('aborted', 'AbortError'))
            })
          }),
      )
      fetchPhotosMock.mockResolvedValue(page(['a', 'b', 'c', 'd']))

      renderPage(true, '/photos/b?sort=oldest')
      await waitFor(() => {
        expect(resolvers.has('b')).toBe(true)
      })
      resolvers.get('b')?.(photo({ uid: 'b', title: 'Beach' }))
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('link', { name: 'Next photo' })

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
      await screen.findByRole('link', { name: 'Next photo' })
      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/d')
      })
      expect(screen.getByRole('heading', { name: 'Beach' })).toBeInTheDocument()

      resolvers.get('c')?.(photo({ uid: 'c', title: 'Cliff' }))
      resolvers.get('d')?.(photo({ uid: 'd', title: 'Dune' }))

      expect(await screen.findByRole('heading', { name: 'Dune' })).toBeInTheDocument()
      expect(screen.queryByRole('heading', { name: 'Cliff' })).not.toBeInTheDocument()
    })

    it('closes to the source list with Escape', async () => {
      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 'Escape' })
      await waitFor(() => {
        // The library lives at the root route; anchor so `/photos/b` cannot pass.
        expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
      })
    })

    it('steps back out of the drawer before closing the viewer with Escape', async () => {
      renderPage(true, '/photos/b?sort=oldest&info=1')
      await screen.findByRole('heading', { name: 'Beach' })

      // First Escape shuts the drawer, staying on the photo.
      fireEvent.keyDown(document, { key: 'Escape' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')
      })
      expect(screen.getByTestId('location')).not.toHaveTextContent('info=1')

      // A second Escape leaves the viewer for the list.
      fireEvent.keyDown(document, { key: 'Escape' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent(/^\/$/)
      })
    })
  })

  describe('title', () => {
    it('never titles the viewer with the file name', async () => {
      fetchPhotoMock.mockResolvedValue(photo({ title: '', file_name: 'IMG_1234.jpg' }))
      renderPage()

      await screen.findByRole('heading', { level: 1 })
      expect(screen.queryByRole('heading', { name: 'IMG_1234.jpg' })).not.toBeInTheDocument()
      expect(screen.getByRole('heading', { level: 1 }).textContent).not.toContain('IMG_1234')
    })

    it('falls back to the capture date and place when the photo has no title', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({
          title: '',
          file_name: 'IMG_1234.jpg',
          taken_at: '2026-01-02T10:00:00Z',
          place: { country: 'Czechia', region: 'South Moravia', city: 'Brno', place_name: '' },
        }),
      )
      renderPage()

      const heading = await screen.findByRole('heading', { level: 1 })
      expect(heading.textContent).toContain('Brno')
      expect(heading.textContent).toContain(
        new Date('2026-01-02T10:00:00Z').toLocaleString('en', {
          year: 'numeric',
          month: 'numeric',
          day: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
        }),
      )
    })

    it('says a photo with no title, date or place is untitled rather than naming its file', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({ title: '', file_name: 'IMG_1234.jpg', taken_at: undefined }),
      )
      renderPage()

      expect(await screen.findByRole('heading', { name: 'Untitled' })).toBeInTheDocument()
    })
  })

  describe('curation & metadata', () => {
    it('edits metadata via the API and reflects the refreshed photo', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      await user.click(screen.getByRole('button', { name: 'Edit Title' }))
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
      // The chrome title (the <h1>) follows the refreshed photo.
      expect(await screen.findByRole('heading', { name: 'Sunset beach' })).toBeInTheDocument()
    })

    it('adds and removes album memberships inline', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)
      await waitFor(() => {
        expect(fetchAlbumsMock).toHaveBeenCalled()
      })

      await user.type(screen.getByRole('combobox', { name: 'Add to album' }), 'trips')
      await user.click(await screen.findByRole('option', { name: 'Trips' }))
      await waitFor(() => {
        expect(addAlbumPhotosMock).toHaveBeenCalledWith('al_2', ['b'])
      })
      expect(await screen.findByText('Trips')).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Remove from album Holidays' }))
      await waitFor(() => {
        expect(removeAlbumPhotosMock).toHaveBeenCalledWith('al_1', ['b'])
      })
    })

    it('adds and removes label memberships inline', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)
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

    it('toggles the per-user favorite with the button and the f key', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await user.click(screen.getByRole('button', { name: 'Add to favorites' }))
      await waitFor(() => {
        expect(favoritePhotoMock).toHaveBeenCalledWith('b', true)
      })

      favoritePhotoMock.mockClear()
      fireEvent.keyDown(document, { key: 'f' })
      await waitFor(() => {
        expect(favoritePhotoMock).toHaveBeenCalled()
      })
    })
  })

  describe('editing', () => {
    it('writes a non-destructive edit and previews it live on the one photo', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // The panel is not up until asked for, and the photo has the screen to itself.
      expect(screen.queryByRole('button', { name: 'Rotate right' })).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Edits' }))
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')

      const main = screen.getByRole('img', { name: 'Beach' })
      await user.click(screen.getByRole('button', { name: 'Rotate right' }))
      expect(main).toHaveStyle({ transform: 'rotate(90deg)' })
      // The page still carries exactly one copy of the photo.
      expect(container.querySelectorAll('img')).toHaveLength(1)

      saveEditMock.mockResolvedValue({ ...NEUTRAL, rotation: 90 })
      await user.click(screen.getByRole('button', { name: 'Save edits' }))
      await waitFor(() => {
        expect(saveEditMock).toHaveBeenCalled()
      })
      expect(saveEditMock.mock.calls[0][1]).toMatchObject({ rotation: 90 })
      await waitFor(() => {
        expect(screen.getByRole('img', { name: 'Beach' })).toHaveStyle({
          transform: 'rotate(90deg)',
        })
      })
    })

    it('composes adjustments made in one batch instead of dropping the earlier one', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await user.click(screen.getByRole('button', { name: 'Edits' }))

      const main = screen.getByRole('img', { name: 'Beach' })
      const brightness = screen.getByLabelText('Brightness')
      act(() => {
        fireEvent.click(screen.getByRole('button', { name: 'Rotate right' }))
        fireEvent.change(brightness, { target: { value: '0.5' } })
      })
      expect(main).toHaveStyle({ transform: 'rotate(90deg)', filter: 'brightness(1.5)' })

      saveEditMock.mockResolvedValue({ ...NEUTRAL, rotation: 90, brightness: 0.5 })
      await user.click(screen.getByRole('button', { name: 'Save edits' }))
      await waitFor(() => {
        expect(saveEditMock).toHaveBeenCalled()
      })
      expect(saveEditMock.mock.calls[0][1]).toMatchObject({ rotation: 90, brightness: 0.5 })
    })

    it('closes the edit panel from its header, discarding what was not saved', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await user.click(screen.getByRole('button', { name: 'Edits' }))
      await user.click(screen.getByRole('button', { name: 'Rotate right' }))
      expect(screen.getByRole('img', { name: 'Beach' })).toHaveStyle({ transform: 'rotate(90deg)' })

      await user.click(screen.getByRole('button', { name: 'Close the edits panel' }))
      expect(screen.queryByRole('button', { name: 'Rotate right' })).not.toBeInTheDocument()
      expect(saveEditMock).not.toHaveBeenCalled()
      expect(screen.getByRole('img', { name: 'Beach' })).not.toHaveStyle({
        transform: 'rotate(90deg)',
      })
    })

    it('shows the edits on their own — activating them does not drag in the info panel', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await user.click(screen.getByRole('button', { name: 'Edits' }))
      expect(screen.getByRole('button', { name: 'Rotate right' })).toBeInTheDocument()
      // The edit view carries none of the metadata sections.
      expect(screen.queryByText('Caption & place')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Technical details' })).not.toBeInTheDocument()
    })

    it('never lets the faces and the edits both lead the drawer', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()

      // Opening the edits takes the lead slot from the faces and drops the boxes.
      await user.click(screen.getByRole('button', { name: 'Edits' }))
      expect(screen.getByRole('button', { name: 'Rotate right' })).toBeInTheDocument()
      expect(screen.queryByText('Faces: 2')).not.toBeInTheDocument()
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()

      // And showing the faces again closes the edits, the same way round.
      await user.click(screen.getByRole('button', { name: 'Show faces' }))
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Rotate right' })).not.toBeInTheDocument()
    })

    it('stands the faces down while the preview is edited, and brings them back', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      // A photo stored rotated: the boxes are placed in percentages of the upright
      // image, so over this preview they would miss the faces. The face UI is
      // offered at all only while the preview is untouched.
      fetchEditMock.mockResolvedValue({ ...NEUTRAL, rotation: 90 })
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('button', { name: 'Edits' })

      expect(screen.queryByRole('button', { name: 'Show faces' })).not.toBeInTheDocument()
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
      fireEvent.keyDown(document, { key: 'm' })
      expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()

      saveEditMock.mockResolvedValue(NEUTRAL)
      await user.click(screen.getByRole('button', { name: 'Edits' }))
      await user.click(screen.getByRole('button', { name: 'Reset to original' }))
      expect(await screen.findByRole('button', { name: 'Show faces' })).toBeInTheDocument()
    })
  })

  it('shows a read-only viewer to viewers', async () => {
    const user = userEvent.setup()
    fetchFacesMock.mockResolvedValue(facesResponse(1))
    renderPage(false)
    await screen.findByRole('heading', { name: 'Beach' })

    // No edit affordances in the chrome, and the drawer offers none either.
    expect(screen.queryByRole('button', { name: 'Edits' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Info' }))
    expect(screen.queryByRole('button', { name: 'Edit Title' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Add to album' })).not.toBeInTheDocument()
    // Read-only album/label chips still link out.
    expect(screen.getByRole('link', { name: 'Holidays' })).toHaveAttribute('href', '/albums/al_1')
    expect(screen.getByRole('link', { name: 'Sunset' })).toHaveAttribute('href', '/labels/lb_1')
    expect(fetchAlbumsMock).not.toHaveBeenCalled()

    // A viewer may show the faces but cannot select one to name it.
    await user.click(screen.getByRole('button', { name: 'Show faces' }))
    expect(screen.getByTestId('face-overlay')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Select face #1' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })
})
