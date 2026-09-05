import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { type Location, MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import { MorphContext, type MorphState } from '../components/morph/MorphContext'
import { NARROW_VIEWPORT_QUERY } from '../hooks/useIsNarrowViewport'
import { DEFAULT_IDLE_MS } from '../hooks/useAutoHideChrome'
import { FIRST_RUN_HOLD_MS } from '../hooks/useViewerChrome'
import { COARSE_POINTER_QUERY } from '../lib/mapGestures'
import i18n from '../i18n'
import { clearBlurPlaceholderCache } from '../lib/blurPlaceholder'
import { readGridScroll, writeGridScroll } from '../lib/gridScroll'
import { stageRenditionName } from '../lib/rendition'
import { type AlbumCount, type LabelCount } from '../services/organize'
import { type FacesResponse } from '../services/people'
import {
  GRID_PREVIEW_SIZE,
  type PhotoDetail,
  type PhotoEdit,
  type PhotoListResponse,
} from '../services/photos'
import { STUB_CANVAS_DATA_URL, stubBlurCanvas } from '../test/canvas'
import { declarations, readCss, ruleBody } from '../test/css'
import { frameRatio, loadImageAs } from '../test/imageFrame'

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
    ratePhoto: vi.fn(),
    fetchPhotos: vi.fn(),
    searchPhotos: vi.fn(),
    archivePhoto: vi.fn(),
    unarchivePhoto: vi.fn(),
    hidePhoto: vi.fn(),
    unhidePhoto: vi.fn(),
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

vi.mock('../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/comments')>()
  return { ...actual, fetchComments: vi.fn(), createComment: vi.fn() }
})

const {
  fetchPhoto,
  fetchEdit,
  saveEdit,
  updatePhoto,
  favoritePhoto,
  ratePhoto,
  fetchPhotos,
  searchPhotos,
  archivePhoto,
  unarchivePhoto,
  hidePhoto,
  unhidePhoto,
} = await import('../services/photos')
const { fetchAlbums, fetchLabels, addAlbumPhotos, removeAlbumPhotos, attachLabel, detachLabel } =
  await import('../services/organize')
const { fetchFaces, assignFace } = await import('../services/people')
const fetchFacesMock = vi.mocked(fetchFaces)
const assignFaceMock = vi.mocked(assignFace)
const { fetchComments, createComment } = await import('../services/comments')
const fetchCommentsMock = vi.mocked(fetchComments)
const createCommentMock = vi.mocked(createComment)

const fetchPhotoMock = vi.mocked(fetchPhoto)
const fetchEditMock = vi.mocked(fetchEdit)
const saveEditMock = vi.mocked(saveEdit)
const updatePhotoMock = vi.mocked(updatePhoto)
const favoritePhotoMock = vi.mocked(favoritePhoto)
const ratePhotoMock = vi.mocked(ratePhoto)
const fetchPhotosMock = vi.mocked(fetchPhotos)
const searchPhotosMock = vi.mocked(searchPhotos)
const archivePhotoMock = vi.mocked(archivePhoto)
const unarchivePhotoMock = vi.mocked(unarchivePhoto)
const hidePhotoMock = vi.mocked(hidePhoto)
const unhidePhotoMock = vi.mocked(unhidePhoto)
const fetchAlbumsMock = vi.mocked(fetchAlbums)
const fetchLabelsMock = vi.mocked(fetchLabels)
const addAlbumPhotosMock = vi.mocked(addAlbumPhotos)
const removeAlbumPhotosMock = vi.mocked(removeAlbumPhotos)
const attachLabelMock = vi.mocked(attachLabel)
const detachLabelMock = vi.mocked(detachLabel)

const NEUTRAL: PhotoEdit = { rotation: 0, brightness: 0, contrast: 0 }

/**
 * The rendition the viewer stage resolves to for the 4000 × 3000 fixture in
 * jsdom's 1024 × 768 window at DPR 1. The stage picks its size from the box it
 * paints into (see `lib/rendition`), so these tests derive it the same way
 * rather than pinning a literal that a viewport change would silently invalidate.
 */
const STAGE_SIZE = stageRenditionName(
  { width: window.innerWidth, height: window.innerHeight },
  { width: 4000, height: 3000 },
)

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
    review_enabled: true,
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

/**
 * Renders the detail page. `semanticSearch` is the instance capability, which
 * decides the mode prev/next pages the originating search with; it defaults to
 * available so a `mode=semantic` URL is followed as written.
 */
function renderPage(
  canWrite = true,
  entry: string | Partial<Location> = '/photos/b?sort=oldest',
  semanticSearch = true,
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider
        value={{ semantic_search: semanticSearch, known: true, passkeys: false }}
      >
        <AuthContext.Provider value={auth(canWrite)}>
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/photos/:uid" element={<PhotoDetailPage />} />
            </Routes>
            <LocationProbe />
          </MemoryRouter>
        </AuthContext.Provider>
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
}

/** Opens the metadata drawer (its contents are aria-hidden until it is). */
async function openInfo(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Info' }))
}

/**
 * Opens the library-operations overflow menu (archive / hide). Its items are not
 * rendered at all while it is shut, which is the point: no glyph, no hidden twin
 * — the operations are read as text or not offered.
 */
async function openLibrary(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Library actions' }))
  await screen.findByText('Library')
}

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer for the narrow
 * viewport query alone, so only `useIsNarrowViewport` changes its mind — every
 * other media query (reduced motion, coarse pointer) keeps the shared setup's
 * non-matching default. Reset to desktop after each test.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow && query === NARROW_VIEWPORT_QUERY,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** The top action bar, which on a phone keeps only the view toggles. */
function actionBar(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.kk-viewer__actions')
  if (el === null) {
    throw new Error('action bar not found')
  }
  return el
}

/** The root immersive-viewer element, for reading its data-* view flags. */
function viewer(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.kk-viewer')
  if (el === null) {
    throw new Error('viewer root not found')
  }
  return el
}

/** The figure the face overlay's percentages are measured against. */
function stageFigure(): HTMLElement | null {
  return document.querySelector<HTMLElement>('.kk-viewer__figure')
}

/** The metadata drawer, whose contents stay mounted while it is shut. */
function drawer(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.kk-viewer__panel')
  if (el === null) {
    throw new Error('drawer not found')
  }
  return el
}

/** What a browser lets the keyboard reach by the elements' own native rules. */
const FOCUSABLE = 'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])'

/**
 * The drawer's controls the keyboard can still walk into. jsdom implements neither
 * `inert` nor layout, so this reads the same DOM condition a browser acts on: an
 * `inert` ancestor takes a whole subtree out of the tab order.
 */
function tabbableInDrawer(container: HTMLElement): HTMLElement[] {
  return [...drawer(container).querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
    (el) => el.closest('[inert]') === null && !el.hasAttribute('disabled'),
  )
}

/**
 * Reports the stage preview as loaded at the given natural size — jsdom fetches
 * nothing, so this is the only way the viewer learns the shape of what it renders.
 * Until it happens no face box is drawn, so any test about the boxes has to call
 * it. The default is the shape of `photo()`'s own row, i.e. a correct row.
 */
function loadPreview(width = 4000, height = 3000): void {
  // Never the progressive stand-in beneath it: that one is a smaller rendition
  // of the same photograph, and it is the FULL-size image whose arrival the
  // viewer measures its frame from.
  const img = document.querySelector<HTMLImageElement>(
    'img.kk-viewer__image:not(.kk-viewer__image--under)',
  )
  if (img === null) {
    throw new Error('stage preview not found')
  }
  loadImageAs(img, width, height)
}

/**
 * A stand-in for `HTMLImageElement` recording what the viewer's preloader asks
 * the browser for. jsdom fetches nothing and implements no `decode()`, so a
 * warmed image is only ever *requested* here, and a test that wants one to become
 * ready says so by hand through {@link StubImage.finish}.
 */
class StubImage {
  /** Every address a preloader has started a load for, in order. */
  static readonly requested: string[] = []
  private static readonly live = new Map<string, StubImage>()

  decoding = 'auto'
  onload: (() => void) | null = null
  onerror: (() => void) | null = null

  private url = ''

  /** Assigning a source is what starts a load, so that is where it is recorded. */
  get src(): string {
    return this.url
  }

  set src(url: string) {
    this.url = url
    if (url !== '') {
      StubImage.requested.push(url)
      StubImage.live.set(url, this)
    }
  }

  removeAttribute(name: string): void {
    if (name === 'src') {
      StubImage.live.delete(this.url)
      this.url = ''
    }
  }

  /** Reports the warmed image at `url` as loaded, as the network would. */
  static finish(url: string): void {
    StubImage.live.get(url)?.onload?.()
  }

  static reset(): void {
    StubImage.requested.length = 0
    StubImage.live.clear()
  }
}

/** Every address the viewer's preloader has asked the browser for. */
function preloaded(): string[] {
  return [...StubImage.requested]
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  StubImage.reset()
  vi.stubGlobal('Image', StubImage)
  // Every test but the phone ones runs at the desktop breakpoint, where the
  // curation controls ride the top action bar.
  mockViewport(false)
  // The face-overlay toggle persists to localStorage; start every test from the
  // shipped default (overlay off — the photo is the content).
  window.localStorage.removeItem('kukatko.faces.overlay')
  fetchFacesMock.mockResolvedValue(facesResponse(0))
  assignFaceMock.mockResolvedValue(undefined)
  fetchCommentsMock.mockResolvedValue([])
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
  ratePhotoMock.mockResolvedValue(undefined)
  archivePhotoMock.mockResolvedValue(undefined)
  unarchivePhotoMock.mockResolvedValue(undefined)
  hidePhotoMock.mockResolvedValue(undefined)
  unhidePhotoMock.mockResolvedValue(undefined)
})

afterEach(() => {
  // `restoreMocks` puts spies back but leaves stubbed globals in place.
  vi.unstubAllGlobals()
})

describe('PhotoDetailPage — immersive viewer', () => {
  it('names the browser tab after the photo, and hands it back on the way out', async () => {
    // Opening a photo in a second tab is ordinary here, and browser history is
    // how "the photo I saw last week" is found again — both read this title.
    const { unmount } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    expect(document.title).toBe('Beach · Kukátko')

    // Leaving must not strand the photo's name over whatever comes next.
    unmount()
    expect(document.title).toBe('Kukátko')
  })

  it('falls back to the photo facts in the tab when it carries no title', async () => {
    // A photo nobody named is called by when and where it was taken — the very
    // same name the viewer's heading shows.
    fetchPhotoMock.mockResolvedValue(photo({ title: '', description: '' }))
    renderPage()

    const heading = await screen.findByRole('heading', { level: 1 })
    expect(document.title).toBe(`${heading.textContent} · Kukátko`)
    expect(document.title).not.toBe('Kukátko')
  })

  it('says a purged photo is gone rather than failing blankly', async () => {
    // The audit log outlives what it audits: an entry recording a purge links to
    // the photo it purged, so a 404 here is the normal case, not a failure.
    const { ApiError } = await import('../services/auth')
    fetchPhotoMock.mockRejectedValue(new ApiError(404, 'photo not found'))
    renderPage()

    expect(await screen.findByText('This photo no longer exists.')).toBeInTheDocument()
    expect(screen.getByText(/It was most likely purged from the trash/)).toBeInTheDocument()
    expect(screen.queryByText('Could not load this photo.')).toBeNull()
  })

  it('still reports a failed load as a failure, with a way back', async () => {
    fetchPhotoMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Could not load this photo.')).toBeInTheDocument()
  })

  it('opens the photo full-bleed into a viewer, the image owning the screen', async () => {
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    // The whole point: one preview of the photo, full-bleed on the viewer stage,
    // never a second copy — and the default state is just the photo, no panel.
    const dialog = screen.getByRole('dialog', { name: 'Photo viewer' })
    expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
    // One full-size preview. The second `<img>` is the progressive stand-in — a
    // smaller rendition painted underneath while that preview is on the wire —
    // which goes as soon as the real one lands.
    expect(container.querySelectorAll('img')).toHaveLength(2)
    loadPreview()
    expect(container.querySelectorAll('img')).toHaveLength(1)
    expect(within(dialog).getByRole('img', { name: 'Beach' }).getAttribute('src')).toContain(
      STAGE_SIZE,
    )
    expect(screen.queryByTestId('face-overlay')).not.toBeInTheDocument()
    // The similar-photos strip lives in the (mounted) drawer, keyed to this photo.
    expect(screen.getByTestId('similar')).toHaveAttribute('data-uid', 'b')
  })

  it('keeps the rating and personal-marking controls in the action bar', async () => {
    // These curation controls stay reachable in the chrome for keyboard and
    // screen-reader users; the favorite heart shares its toggle with the `f` key.
    // With a mouse the top edge costs nothing to reach, so the desktop layout
    // keeps them there — and carries no bottom dock at all.
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    const bar = actionBar(container)
    expect(within(bar).getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Rate 5 of 5' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Look at later' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Pick' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Reject' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Photo actions' })).not.toBeInTheDocument()
  })

  /**
   * The bar used to be fourteen identical round glyphs in one row, which says
   * nothing about which of them change the library and which only change the
   * view. These tests pin the three kinds apart — the assessment of the
   * photograph, the operations on the library, the toggles of the view — plus the
   * handful of acts that are never folded into any of them.
   */
  describe('the bar is grouped by what its controls do', () => {
    it('gathers the stars and the review marks into one named assessment group', async () => {
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const assess = screen.getByRole('group', { name: 'Photo assessment' })
      expect(within(assess).getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
      expect(within(assess).getByRole('button', { name: 'Rate 5 of 5' })).toBeInTheDocument()
      expect(within(assess).getByRole('button', { name: 'Look at later' })).toBeInTheDocument()
      expect(within(assess).getByRole('button', { name: 'Pick' })).toBeInTheDocument()
      expect(within(assess).getByRole('button', { name: 'Reject' })).toBeInTheDocument()
      // What the group is NOT: neither the view toggles nor the library
      // operations belong to an opinion about the photograph.
      expect(within(assess).queryByRole('button', { name: 'Info' })).not.toBeInTheDocument()
      expect(
        within(assess).queryByRole('button', { name: 'Library actions' }),
      ).not.toBeInTheDocument()
    })

    it('gathers faces, edits and info into one named group of view toggles', async () => {
      fetchFacesMock.mockResolvedValue(facesResponse(1))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const views = screen.getByRole('group', { name: 'Photo views' })
      expect(await within(views).findByRole('button', { name: 'Show faces' })).toBeInTheDocument()
      expect(within(views).getByRole('button', { name: 'Edits' })).toBeInTheDocument()
      expect(within(views).getByRole('button', { name: 'Info' })).toBeInTheDocument()
      expect(within(views).queryByRole('button', { name: 'Rate 1 of 5' })).not.toBeInTheDocument()
    })

    it('keeps favoriting out of every group, one click away', async () => {
      // Favoriting, opening the info drawer and stepping to the next photograph
      // are what a reader does on nearly every photo, so none of them is ever
      // folded into a menu — and the heart belongs to no group: it is not an
      // assessment of the photograph, it is a shortlist.
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const heart = within(actionBar(container)).getByRole('button', { name: 'Add to favorites' })
      expect(heart.closest('.kk-viewer__group')).toBeNull()
      expect(screen.getByRole('button', { name: 'Info' })).toBeInTheDocument()
      expect(await screen.findByRole('link', { name: 'Next photo' })).toBeInTheDocument()

      await user.click(heart)
      await waitFor(() => {
        expect(favoritePhotoMock).toHaveBeenCalledWith('b', true)
      })
    })

    it('spaces the groups apart more widely than the controls inside one', () => {
      // jsdom computes no layout, so the grouping's visual half is read off the
      // stylesheet: if the two gaps were equal there would be no grouping to see,
      // only a `role` a sighted reader never hears.
      const css = readCss('src/components/photo/viewer.css')
      const bar = declarations(ruleBody(css, /\.kk-viewer__actions\s*(?=\{)/, /gap:/) ?? '')
      const group = declarations(ruleBody(css, /\.kk-viewer__group\s*(?=\{)/) ?? '')
      expect(bar.get('gap')).toBe('var(--kk-space-3)')
      expect(group.get('gap')).toBe('var(--kk-space-2)')
      // And a hairline in front of every group but the first, for the reader who
      // cannot tell 12px from 8px over a busy photograph.
      const rule = ruleBody(css, /\.kk-viewer__actions > \* \+ \.kk-viewer__group::before\s*(?=\{)/)
      expect(declarations(rule ?? '').get('width')).toBe('1px')
    })
  })

  /**
   * Archive and hide are the only two controls that change WHAT IS IN the
   * library, and they are the two no glyph can say. They live behind one overflow
   * control and are read as sentences.
   */
  describe('library operations behind the overflow menu', () => {
    it('offers them as words, not as glyphs', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // Shut, the menu renders nothing at all: no second, hidden copy of the two
      // controls for a query — or a screen reader — to find.
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Hide from the library' }),
      ).not.toBeInTheDocument()

      const toggle = within(actionBar(container)).getByRole('button', { name: 'Library actions' })
      expect(toggle).toHaveAttribute('title', 'Library actions')

      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Hide from the library' })).toHaveTextContent(
        'Hide from the library',
      )
      expect(screen.getByRole('button', { name: 'Archive' })).toHaveTextContent('Archive')
    })

    it('is not offered at all to a viewer', async () => {
      // An overflow control that opens an empty menu is worse than no control.
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Library actions' })).not.toBeInTheDocument()
    })

    it('pins the chrome while it is open', async () => {
      // The trigger is chrome, and chrome fades when the viewer goes idle. A menu
      // whose own trigger melts away under the reader's hand cannot be closed by
      // the control that opened it — so the idle timer stands down while it is up.
      vi.useFakeTimers({ shouldAdvanceTime: true })
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      try {
        const { container } = renderPage()
        await screen.findByRole('heading', { name: 'Beach' })

        // The control: shut, the chrome melts away on idle exactly as before.
        act(() => {
          vi.advanceTimersByTime(10_000)
        })
        expect(viewer(container)).toHaveAttribute('data-chrome', 'hidden')

        await openLibrary(user)
        act(() => {
          vi.advanceTimersByTime(10_000)
        })
        expect(viewer(container)).toHaveAttribute('data-chrome', 'visible')
      } finally {
        vi.useRealTimers()
      }
    })
  })

  describe('archive control', () => {
    it('archives the open photo and swaps the item to Restore', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // An editor finds Archive in the library menu; clicking it calls the
      // archive service for this photo and, on success, keeps the user on the
      // page with the menu now offering Restore (the photo is in the trash).
      // The item says the act in words, and the tooltip repeats it.
      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Archive' })).toHaveAttribute('title', 'Archive')

      await user.click(screen.getByRole('button', { name: 'Archive' }))
      await waitFor(() => {
        expect(archivePhotoMock).toHaveBeenCalledWith('b')
      })
      await openLibrary(user)
      const restore = await screen.findByRole('button', { name: 'Restore' })
      expect(restore).toHaveAttribute('title', 'Restore')
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    })

    it('hides the archive control from a viewer', async () => {
      // Archiving is an editor+ action; a viewer gets the read-only viewer, and
      // with it no library menu at all.
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Library actions' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Restore' })).not.toBeInTheDocument()
    })

    it('shows Restore for an already-archived photo and unarchives on click', async () => {
      // Opened from the Trash page: the photo arrives archived, so the same item
      // leads with Restore and calls unarchive, swapping back to Archive.
      const user = userEvent.setup()
      fetchPhotoMock.mockResolvedValue(photo({ archived_at: '2026-01-01T00:00:00Z' }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Restore' }))
      await waitFor(() => {
        expect(unarchivePhotoMock).toHaveBeenCalledWith('b')
      })
      await openLibrary(user)
      expect(await screen.findByRole('button', { name: 'Archive' })).toBeInTheDocument()
    })
  })

  describe('hide-from-library control', () => {
    it('hides the open photo and swaps the item to Show', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // The item sits next to archive, but it is a different act: the photo is
      // not deleted, so the page stays put and the menu simply offers the way
      // back.
      await openLibrary(user)
      await user.click(screen.getByRole('button', { name: 'Hide from the library' }))
      await waitFor(() => {
        expect(hidePhotoMock).toHaveBeenCalledWith('b')
      })
      await openLibrary(user)
      expect(await screen.findByRole('button', { name: 'Show in the library' })).toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Hide from the library' }),
      ).not.toBeInTheDocument()
    })

    it('shows the way back in the item title, so the flag can be undone', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // A flag you cannot list is a flag you cannot undo, so the item that sets
      // it also names the search that finds it again.
      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Hide from the library' })).toHaveAttribute(
        'title',
        expect.stringContaining('hidden:yes'),
      )
    })

    it('leads with Show for an already-hidden photo and unhides on click', async () => {
      const user = userEvent.setup()
      fetchPhotoMock.mockResolvedValue(photo({ hidden_from_library: true }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      expect(
        screen.queryByRole('button', { name: 'Hide from the library' }),
      ).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Show in the library' }))
      await waitFor(() => {
        expect(unhidePhotoMock).toHaveBeenCalledWith('b')
      })
      await openLibrary(user)
      expect(
        await screen.findByRole('button', { name: 'Hide from the library' }),
      ).toBeInTheDocument()
    })

    it('hides the control from a viewer', async () => {
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Library actions' })).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Hide from the library' }),
      ).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Show in the library' })).not.toBeInTheDocument()
    })
  })

  /**
   * The `s` key ("skrýt") is the eye button's own act on a keyboard, for walking
   * a series and dropping the dross without aiming a mouse at the same 1rem
   * target every time. It runs the very same handler, so these tests pin what is
   * particular to the key: that it toggles, that it stays on the photo (the
   * second press is the undo — the toast has no action button), that a viewer
   * gets nothing, and that it keeps out of the way of typing and of dialogs.
   */
  describe('the `s` shortcut — hide and unhide', () => {
    it('hides the open photo and swaps the menu item to Show', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 's' })
      await waitFor(() => {
        expect(hidePhotoMock).toHaveBeenCalledWith('b')
      })
      // The key and the menu are one state: what the keyboard did shows in the
      // badge at once, and in the menu's wording the next time it is opened.
      expect(await screen.findByText('Hidden from the library')).toBeInTheDocument()
      await openLibrary(user)
      expect(await screen.findByRole('button', { name: 'Show in the library' })).toBeInTheDocument()
    })

    it('stays on the photo and brings it back on a second press', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 's' })
      // The badge is the proof the state is legible while you stand on it, so a
      // stray press is obvious rather than silent.
      expect(await screen.findByText('Hidden from the library')).toBeInTheDocument()
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')

      fireEvent.keyDown(document, { key: 's' })
      await waitFor(() => {
        expect(unhidePhotoMock).toHaveBeenCalledWith('b')
      })
      await openLibrary(user)
      expect(
        await screen.findByRole('button', { name: 'Hide from the library' }),
      ).toBeInTheDocument()
      expect(screen.queryByText('Hidden from the library')).toBeNull()
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')
    })

    it('does nothing at all for a viewer', async () => {
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 's' })
      await act(async () => {
        await Promise.resolve()
      })
      expect(hidePhotoMock).not.toHaveBeenCalled()
      expect(screen.queryByText('Hidden from the library')).toBeNull()
    })

    it('is ignored while a text field has the keystroke', async () => {
      // The hook's own guard, asserted through the page: `s` is a letter, and
      // "skrýt" must never happen because someone typed a caption.
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      const field = screen.getAllByRole('textbox')[0]
      fireEvent.keyDown(field, { key: 's' })
      await act(async () => {
        await Promise.resolve()
      })
      expect(hidePhotoMock).not.toHaveBeenCalled()
    })

    it('is ignored while a modal with a form is open', async () => {
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      const modal = document.createElement('div')
      modal.className = 'modal show'
      modal.innerHTML = '<input aria-label="modal-field" />'
      document.body.append(modal)
      try {
        fireEvent.keyDown(document, { key: 's' })
        await act(async () => {
          await Promise.resolve()
        })
        expect(hidePhotoMock).not.toHaveBeenCalled()
      } finally {
        modal.remove()
      }
    })
  })

  /**
   * "Held back from the library" — hidden, or archived into the trash — is a
   * state the viewer has to SHOW, not just announce. It used to ride two icon
   * toggles whose glyphs named the state and whose `active` marking tinted it
   * red; an eye and a struck-through eye read alike at 1rem, and the archive box
   * names a flag no glyph has. The state is now written twice in words — as the
   * badge beside the title, which every reader (a viewer included) sees without
   * opening anything, and as the act the library menu offers, which can only be
   * "Hide from the library" for a photo that is in it.
   */
  describe('withheld state — hidden from the library, archived', () => {
    it('offers the way back for a hidden photo, and names it', async () => {
      const user = userEvent.setup()
      fetchPhotoMock.mockResolvedValue(photo({ hidden_from_library: true }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      const on = screen.getByRole('button', { name: 'Show in the library' })
      expect(on).toHaveAttribute('title', 'Bring the photo back into the library')
      expect(screen.queryByRole('button', { name: 'Hide from the library' })).toBeNull()
    })

    it('offers the hiding for a photo that is in the library', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      const off = screen.getByRole('button', { name: 'Hide from the library' })
      expect(off).toHaveAttribute('title', expect.stringContaining('hidden:yes'))
      expect(screen.queryByRole('button', { name: 'Show in the library' })).toBeNull()
    })

    it('follows the photo as it is hidden and brought back', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      await user.click(screen.getByRole('button', { name: 'Hide from the library' }))
      expect(await screen.findByText('Hidden from the library')).toBeInTheDocument()

      await openLibrary(user)
      await user.click(screen.getByRole('button', { name: 'Show in the library' }))
      await waitFor(() => {
        expect(screen.queryByText('Hidden from the library')).toBeNull()
      })
    })

    it('offers Restore once the photo is in the trash', async () => {
      // Two items of the same kind behave the same way, or the wording teaches
      // nothing.
      const user = userEvent.setup()
      fetchPhotoMock.mockResolvedValue(photo({ archived_at: '2026-01-01T00:00:00Z' }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Archive' })).toBeNull()
    })

    it('says "hidden from the library" in words beside the title', async () => {
      // Colour alone fails a colour-blind reader and every forced-colours mode;
      // the badge carries the same state as text, and puts it somewhere other
      // than on the one small button.
      fetchPhotoMock.mockResolvedValue(photo({ hidden_from_library: true }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const badge = screen.getByText('Hidden from the library')
      expect(badge.closest('.kk-viewer__flags')).not.toBeNull()
    })

    it('says "in the trash" in words for an archived photo', async () => {
      fetchPhotoMock.mockResolvedValue(photo({ archived_at: '2026-01-01T00:00:00Z' }))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const badge = screen.getByText('In the trash')
      expect(badge.closest('.kk-viewer__flags')).not.toBeNull()
    })

    it('badges nothing for a photo that is simply in the library', async () => {
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(container.querySelector('.kk-viewer__flags')).toBeNull()
      expect(screen.queryByText('Hidden from the library')).toBeNull()
    })

    it('badges a hidden photo for a viewer, who cannot toggle it', async () => {
      // The state is worth knowing even where the controls are not offered.
      fetchPhotoMock.mockResolvedValue(photo({ hidden_from_library: true }))
      renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      expect(screen.queryByRole('button', { name: 'Show in the library' })).toBeNull()
      expect(screen.getByText('Hidden from the library')).toBeInTheDocument()
    })

    it('appears the moment the photo is hidden and goes when it comes back', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      expect(container.querySelector('.kk-viewer__flags')).toBeNull()

      await openLibrary(user)
      await user.click(screen.getByRole('button', { name: 'Hide from the library' }))
      expect(await screen.findByText('Hidden from the library')).toBeInTheDocument()

      await openLibrary(user)
      await user.click(screen.getByRole('button', { name: 'Show in the library' }))
      await waitFor(() => {
        expect(container.querySelector('.kk-viewer__flags')).toBeNull()
      })
    })
  })

  describe('phone layout — the thumb-reachable curation dock', () => {
    beforeEach(() => {
      mockViewport(true)
    })

    it('moves the everyday curation loop out of the top bar into a bottom dock', async () => {
      // The top corner is the hardest place to reach one-handed on a tall phone,
      // so on a phone the stars, the review marks and the heart live in a strip
      // along the bottom edge instead. They are MOVED, not duplicated:
      // `getByRole` throws on a second copy, so each of these assertions also
      // pins that the desktop bar did not keep a hidden twin.
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const dock = screen.getByRole('group', { name: 'Photo actions' })
      expect(within(dock).getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Rate 5 of 5' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Look at later' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Pick' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Reject' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
      // The very same grouping as the desktop bar's, which is what keeps the two
      // layouts one design instead of two.
      expect(within(dock).getByRole('group', { name: 'Photo assessment' })).toBeInTheDocument()

      const bar = actionBar(container)
      expect(within(bar).queryByRole('button', { name: 'Rate 1 of 5' })).not.toBeInTheDocument()
      expect(within(bar).queryByRole('button', { name: 'Pick' })).not.toBeInTheDocument()
      expect(
        within(bar).queryByRole('button', { name: 'Add to favorites' }),
      ).not.toBeInTheDocument()
    })

    it('leaves the library operations in the top bar, as words', async () => {
      // Archive and hide are occasional, and they are the two that need a
      // sentence rather than a glyph — so they stay in the bar's overflow menu
      // rather than taking two of the dock's finger-sized slots. That is what
      // makes the dock a single row.
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const dock = screen.getByRole('group', { name: 'Photo actions' })
      expect(within(dock).queryByRole('button', { name: 'Library actions' })).toBeNull()
      expect(
        within(actionBar(container)).getByRole('button', { name: 'Library actions' }),
      ).toBeInTheDocument()

      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Hide from the library' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument()
    })

    it('leaves the occasional toggles, the close and the arrows where they were', async () => {
      // Only the everyday loop moves. Faces / Edits / Info are occasional, so they
      // stay in the top cluster, and the two affordances that already worked
      // one-handed — the persistent ✕ and the ‹/› arrows — are untouched.
      fetchFacesMock.mockResolvedValue(facesResponse(1))
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const bar = actionBar(container)
      expect(await within(bar).findByRole('button', { name: 'Show faces' })).toBeInTheDocument()
      expect(within(bar).getByRole('button', { name: 'Edits' })).toBeInTheDocument()
      expect(within(bar).getByRole('button', { name: 'Info' })).toBeInTheDocument()

      expect(screen.getByRole('button', { name: 'Back to the list' })).toBeInTheDocument()
      expect(await screen.findByRole('link', { name: 'Previous photo' })).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Next photo' })).toBeInTheDocument()

      // The viewer is nothing but icons, so each of them answers a hovering
      // mouse with the same sentence its accessible name carries.
      for (const name of ['Show faces', 'Edits', 'Info']) {
        expect(within(bar).getByRole('button', { name })).toHaveAttribute('title', name)
      }
      expect(screen.getByRole('button', { name: 'Back to the list' })).toHaveAttribute(
        'title',
        'Back to the list',
      )
      expect(screen.getByRole('link', { name: 'Previous photo' })).toHaveAttribute(
        'title',
        'Previous photo',
      )
      expect(screen.getByRole('link', { name: 'Next photo' })).toHaveAttribute(
        'title',
        'Next photo',
      )

      const dock = screen.getByRole('group', { name: 'Photo actions' })
      expect(within(dock).queryByRole('button', { name: 'Info' })).not.toBeInTheDocument()
    })

    it('keeps the dock controls wired to the very same actions', async () => {
      // The point of the relocation is reach, not a second implementation: the
      // dock's stars, marks, heart and archive drive the same optimistic hooks
      // and the same endpoints the top bar drove.
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      const dock = screen.getByRole('group', { name: 'Photo actions' })

      await user.click(within(dock).getByRole('button', { name: 'Rate 4 of 5' }))
      await waitFor(() => {
        expect(ratePhotoMock).toHaveBeenCalledWith('b', { rating: 4 })
      })
      // Optimistic: the stars fill in place, in the dock.
      expect(within(dock).getByRole('button', { name: 'Rate 4 of 5' })).toHaveAttribute(
        'aria-pressed',
        'true',
      )

      await user.click(within(dock).getByRole('button', { name: 'Reject' }))
      await waitFor(() => {
        expect(ratePhotoMock).toHaveBeenCalledWith('b', { flag: 'reject' })
      })

      await user.click(within(dock).getByRole('button', { name: 'Add to favorites' }))
      await waitFor(() => {
        expect(favoritePhotoMock).toHaveBeenCalledWith('b', true)
      })
      expect(
        await within(dock).findByRole('button', { name: 'Remove from favorites' }),
      ).toBeInTheDocument()
    })

    it('shares its state with the rating hotkeys and the `f` key', async () => {
      // The dock is the same controlled tree the keys already drove, so a hotkey
      // has to light it up — otherwise the two would be separate states.
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      const dock = screen.getByRole('group', { name: 'Photo actions' })

      fireEvent.keyDown(document, { key: '3' })
      await waitFor(() => {
        expect(ratePhotoMock).toHaveBeenCalledWith('b', { rating: 3 })
      })
      expect(within(dock).getByRole('button', { name: 'Rate 3 of 5' })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
      expect(within(dock).getByRole('button', { name: 'Rate 4 of 5' })).toHaveAttribute(
        'aria-pressed',
        'false',
      )

      fireEvent.keyDown(document, { key: 'f' })
      expect(
        await within(dock).findByRole('button', { name: 'Remove from favorites' }),
      ).toBeInTheDocument()
    })

    it('gives a viewer the same dock, and no library menu at all', async () => {
      // Rating, marking and favoriting are personal actions every signed-in user
      // gets, so a viewer's dock is the whole dock; what a viewer does not get is
      // the bar's library menu, which would otherwise open on nothing.
      const { container } = renderPage(false)
      await screen.findByRole('heading', { name: 'Beach' })

      const dock = screen.getByRole('group', { name: 'Photo actions' })
      expect(within(dock).getByRole('button', { name: 'Rate 1 of 5' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Look at later' })).toBeInTheDocument()
      expect(within(dock).getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
      expect(
        within(actionBar(container)).queryByRole('button', { name: 'Library actions' }),
      ).toBeNull()
      expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    })

    it('does not disturb the info drawer', async () => {
      // The drawer still opens from the top bar's Info, still carries its own
      // close, and the dock does not leak a duplicate control into it.
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
      await openInfo(user)
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      const panel = screen.getByRole('complementary', { name: 'Info' })
      await user.click(within(panel).getByRole('button', { name: 'Close info' }))
      expect(viewer(container)).toHaveAttribute('data-panel', 'closed')
    })
  })

  /**
   * jsdom evaluates no media queries and computes no layout, so the dock's
   * placement — bottom edge, safe-area inset, fading with the chrome, standing
   * down under the drawer, finger-sized targets — is pinned by reading the
   * stylesheet itself. These are the properties the relocation is FOR: a strip
   * that a home indicator overlaps, that never fades, or whose buttons stay
   * mouse-sized would satisfy the DOM tests above and still miss the point.
   */
  describe('curation dock stylesheet', () => {
    const css = readCss('src/components/photo/viewer.css')
    const dock = declarations(ruleBody(css, /\.kk-viewer__dock\s*(?=\{)/) ?? '')

    it('pins the dock to the bottom edge above the stage', () => {
      expect(dock.get('position')).toBe('absolute')
      expect(dock.get('bottom')).toBe('0')
      expect(dock.get('left')).toBe('0')
      expect(dock.get('right')).toBe('0')
      // The same layer the top bar and the arrows sit on, so it is over the photo
      // but under the drawer (4) and the persistent close (5).
      expect(dock.get('z-index')).toBe('3')
    })

    it('carries the home-indicator inset itself', () => {
      // Without this the bottom row of controls sits under the iOS home bar,
      // which is exactly the region a thumb-reachable strip must avoid.
      expect(dock.get('padding')).toContain('env(safe-area-inset-bottom, 0px)')
      // A landscape notch eats the sides too.
      expect(dock.get('padding')).toContain('env(safe-area-inset-left, 0px)')
      expect(dock.get('padding')).toContain('env(safe-area-inset-right, 0px)')
    })

    it('keeps its groups on one row, and wrapping only as a last resort', () => {
      // It used to take two rows: eleven finger-sized targets do not fit a 360px
      // line. The two library glyphs have left for the bar's menu, and the star
      // scale gives up its width floor (never its height) below — which is what
      // brings the rest onto one line. `wrap` survives only as the safety valve
      // for something narrower than any ordinary phone.
      expect(dock.get('flex-wrap')).toBe('wrap')
      // The groups are told apart by the same wider gap the top bar uses.
      expect(dock.get('gap')).toBe('var(--kk-space-1) var(--kk-space-3)')
    })

    it('fades away with the rest of the chrome', () => {
      // The photo owns the screen the moment you stop touching it — a permanent
      // bar would trade a reachable strip for a cropped photograph.
      const hidden = declarations(
        ruleBody(css, /\.kk-viewer\[data-chrome='hidden'\] \.kk-viewer__dock\s*(?=\{)/) ?? '',
      )
      expect(hidden.get('opacity')).toBe('0')
      // Faded-out controls must not still swallow taps meant for the photo.
      expect(hidden.get('pointer-events')).toBe('none')
      expect(dock.get('transition')).toContain('opacity')
    })

    it('stands down while the info drawer is open', () => {
      // At this width the drawer covers the screen; a strip peeking out from
      // under it would be unreachable noise.
      const panelOpen = declarations(
        ruleBody(css, /\.kk-viewer\[data-panel='open'\] \.kk-viewer__dock\s*(?=\{)/) ?? '',
      )
      expect(panelOpen.get('opacity')).toBe('0')
      expect(panelOpen.get('pointer-events')).toBe('none')
    })

    it('lifts its shared controls to the 44px finger floor', () => {
      // The stars/flags/heart are sized for a mouse where they are also used
      // (grid tiles, the desktop bar); the dock is the one place that has to
      // enlarge them.
      const buttons = declarations(ruleBody(css, /\.kk-viewer__dock \.btn\s*(?=\{)/) ?? '')
      expect(buttons.get('min-width')).toBe('2.75rem')
      expect(buttons.get('min-height')).toBe('2.75rem')
    })

    it('lets the star scale — and only it — give up that width to hold one row', () => {
      // Nine targets at 44px wide want 396px and a phone offers 344. The stars
      // absorb the difference because they are the one control here that is a
      // *scale*: five contiguous targets, where a mis-tap lands on a neighbour
      // and the next tap undoes it. They stretch back to the full 44px wherever
      // the row has the room (measured in Chromium: 28px at 360, 35 at 393, 42
      // at 430) and never go below 24px, WCAG 2.2's minimum target; the discrete
      // toggles beside them keep both floors, so the browser has nowhere else to
      // take the space from.
      const scale = declarations(
        ruleBody(css, /\.kk-viewer__dock \.kk-viewer__stars\s*(?=\{)/) ?? '',
      )
      expect(scale.get('flex')).toBe('1 1 0')
      expect(scale.get('max-width')).toBe('13.75rem')
      const stars = declarations(
        ruleBody(css, /\.kk-viewer__dock \.kk-viewer__stars \.btn\s*(?=\{)/) ?? '',
      )
      expect(stars.get('flex')).toBe('1 1 0')
      expect(stars.get('min-width')).toBe('1.5rem')
      // Only the width yields: the 44px height floor is the dock's own and is
      // never overridden here.
      expect(stars.get('min-height')).toBeUndefined()
      const group = declarations(
        ruleBody(css, /\.kk-viewer__dock \.kk-viewer__group\s*(?=\{)/) ?? '',
      )
      expect(group.get('min-width')).toBe('0')
    })
  })

  /**
   * The drawer's phone shape. On a 393 × 852 phone the side drawer measured the
   * whole viewport: an opaque panel plus a 55 % black scrim over a photo that was
   * still mounted, still loaded, and not visible by a single pixel — which makes
   * the faces panel meaningless, because its rows are numbered to match the boxes
   * drawn ON that photo. Below `md` it is a bottom sheet instead.
   *
   * jsdom evaluates no media queries and computes no layout, so the geometry is
   * pinned by reading `viewer.css` itself; the DOM tests below cover what a query
   * can see. (The layout itself was checked in a real browser at 393 × 852 — see
   * the commit — because neither of these can see a pixel.)
   */
  describe('phone bottom sheet', () => {
    const css = readCss('src/components/photo/viewer.css')
    // The `max-width: 767.98px` block — the phone half of the drawer's two shapes.
    // Picked by content, so it survives another narrow block being added later.
    const sheet =
      ruleBody(css, /@media \(max-width: 767\.98px\)\s*(?=\{)/, /\.kk-viewer__panel\s*\{/) ?? ''

    it('re-anchors the drawer to the bottom edge', () => {
      const panel = declarations(ruleBody(sheet, /\.kk-viewer__panel\s*(?=\{)/) ?? '')
      expect(panel.get('top')).toBe('auto')
      expect(panel.get('bottom')).toBe('0')
      expect(panel.get('left')).toBe('0')
      expect(panel.get('right')).toBe('0')
      expect(panel.get('height')).toBe('var(--kk-viewer-sheet-h)')
      // It rises from below now instead of sliding in from the right…
      expect(panel.get('transform')).toBe('translateY(100%)')
      // …and comes to rest at the bottom edge when open.
      const open = declarations(ruleBody(sheet, /\.kk-viewer__panel\.is-open\s*(?=\{)/) ?? '')
      expect(open.get('transform')).toBe('translateY(0)')
    })

    it('takes under half the screen, so the photo above it stays the larger thing', () => {
      const root = declarations(ruleBody(css, /\.kk-viewer\s*(?=\{)/) ?? '')
      // `dvh`, not `vh`: a phone browser's collapsing address bar changes the real
      // viewport, and a sheet measured against the larger one overshoots the screen.
      const share = /^(\d+(?:\.\d+)?)dvh$/.exec(root.get('--kk-viewer-sheet-h') ?? '')
      expect(share).not.toBeNull()
      expect(Number(share?.[1])).toBeGreaterThanOrEqual(40)
      expect(Number(share?.[1])).toBeLessThanOrEqual(50)
    })

    it('gives the stage back exactly the sheet’s height', () => {
      // The one thing that keeps the photo on screen: the stage yields the sheet's
      // height rather than the sheet covering it. Same variable on both sides, so
      // a photo half-under the sheet is not expressible.
      const stage = declarations(
        ruleBody(sheet, /\.kk-viewer\[data-panel='open'\] \.kk-viewer__stage\s*(?=\{)/) ?? '',
      )
      expect(stage.get('bottom')).toBe('var(--kk-viewer-sheet-h)')
      expect(css).toContain('bottom var(--kk-duration-base)')
    })

    it('lifts a lead panel’s own height cap inside the sheet', () => {
      // Two nested scroll regions in a ~370px window is a touch trap; in the sheet
      // the faces/edits list scrolls with the sheet.
      const list = declarations(ruleBody(sheet, /\.kk-viewer__panel-scroll\s*(?=\{)/) ?? '')
      expect(list.get('max-height')).toBe('none')
    })

    it('drops the full-screen scrim entirely', () => {
      // The 55 % black wash was half the bug. A sheet needs the photo above it both
      // VISIBLE and still taking its own touch gestures, so the scrim is gone
      // rather than merely shrunk — panning a photo through a dimming tap target
      // is not panning it.
      expect(css).not.toContain('kk-viewer__panel-scrim')
      expect(readCss('src/styles/tokens.css')).not.toContain('--kk-viewer-panel-scrim')
    })

    it('leaves the photo mounted, unscrimmed and gesture-bearing while the sheet is open', async () => {
      mockViewport(true)
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      await openInfo(user)
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')
      expect(container.querySelector('.kk-viewer__panel-scrim')).toBeNull()
      // The figure still carries the swipe/pinch surface marker, so the photo above
      // the sheet is not merely a picture of a photo.
      expect(container.querySelector('.kk-viewer__figure')).toHaveAttribute('data-swipe-surface')
    })

    it('gives the two controls different glyphs', async () => {
      // The other half of the bug: two visually identical round crosses ended up
      // side by side at the same height — the left one closing the whole photo, the
      // right one merely the panel. An arrow leaves the photo, a cross closes what
      // is over it.
      mockViewport(true)
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      const back = screen.getByRole('button', { name: 'Back to the list' })
      expect(back.querySelector('i')).toHaveClass('bi-arrow-left')
      const panel = screen.getByRole('complementary', { name: 'Info' })
      expect(
        within(panel).getByRole('button', { name: 'Close info' }).querySelector('i'),
      ).toHaveClass('bi-x-lg')
    })
  })

  /**
   * What the idle timer is allowed to take. On a desktop any mouse move brings the
   * chrome back; a phone has no such gesture, so an idle screen showing one
   * photograph and one unlabelled glyph left a reader with no name for what they
   * were looking at and no hint that anything else was there. The way back and the
   * title now dim instead of leaving.
   */
  describe('idle chrome stylesheet', () => {
    const css = readCss('src/components/photo/viewer.css')
    const idle = (part: string) =>
      declarations(
        ruleBody(css, new RegExp(`\\.kk-viewer\\[data-chrome='hidden'\\] \\.${part}\\s*(?=\\{)`)) ??
          '',
      )

    it('keeps the photo’s title, dimmed rather than removed', () => {
      const opacity = Number(idle('kk-viewer__heading').get('opacity'))
      expect(opacity).toBeGreaterThan(0)
      expect(opacity).toBeLessThan(1)
    })

    it('never fades the persistent back control at all', () => {
      // It lives outside the chrome by construction; assert no idle rule reaches
      // it, so a later refactor cannot quietly fold the one way out into the fade.
      expect(
        ruleBody(css, /\.kk-viewer\[data-chrome='hidden'\] \.kk-viewer__back\s*(?=\{)/),
      ).toBeUndefined()
    })

    it('still takes the controls and the arrows away', () => {
      // "Everything else may still hide" — the point is an immersive photo, not a
      // permanent bar.
      expect(idle('kk-viewer__actions').get('opacity')).toBe('0')
      expect(idle('kk-viewer__nav').get('opacity')).toBe('0')
      expect(idle('kk-viewer__dock').get('opacity')).toBe('0')
    })

    it('softens the darkening wash instead of dropping it', () => {
      // The title that stays has to stay LEGIBLE over an unknown photograph, so the
      // wash behind it thins rather than vanishing. It rides a pseudo-element for
      // exactly this: a gradient cannot be transitioned, an opacity can.
      const wash = declarations(
        ruleBody(css, /\.kk-viewer\[data-chrome='hidden'\] \.kk-viewer__chrome::before\s*(?=\{)/) ??
          '',
      )
      const opacity = Number(wash.get('opacity'))
      expect(opacity).toBeGreaterThan(0)
      expect(opacity).toBeLessThan(1)
    })
  })

  describe('stage sizing before the image arrives', () => {
    it('states the frame as the image’s own intrinsic size, so nothing moves on arrival', async () => {
      // An `<img>` with a loading `src` and no dimensions is a box the width of
      // its alt text, and the figure has nothing else in flow to size it: without
      // this the stage collapsed to a sliver while the photograph was on the wire,
      // taking the blur and the smaller rendition that fill it down with it.
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      const image = screen.getByRole('img', { name: 'Beach' })
      expect(image).toHaveAttribute('width', '4000')
      expect(image).toHaveAttribute('height', '3000')

      // Once the image has landed the measurement replaces the row's estimate —
      // the same proportions, so the box does not move.
      loadPreview(1920, 1440)
      expect(image).toHaveAttribute('width', '1920')
      expect(image).toHaveAttribute('height', '1440')
    })
  })

  describe('progressive stand-in stylesheet', () => {
    const css = readCss('src/components/photo/viewer.css')

    it('stacks the blur, then the smaller rendition, then the photograph', () => {
      // The order has to be stated rather than inferred: both stand-ins are
      // absolutely positioned, and a positioned element paints ABOVE the in-flow
      // image it is meant to sit under — the image escapes that only while it
      // carries a zoom transform, which it does not with the faces or edit view up.
      const image = declarations(
        ruleBody(css, /\.kk-viewer__figure > \.kk-viewer__image\s*(?=\{)/) ?? '',
      )
      const under = declarations(
        ruleBody(css, /\.kk-viewer__figure > \.kk-viewer__image--under\s*(?=\{)/) ?? '',
      )
      expect(Number(image.get('z-index'))).toBeGreaterThan(Number(under.get('z-index')))
      expect(Number(under.get('z-index'))).toBeGreaterThan(0)
    })

    it('lays the stand-in over exactly the box the photograph will fill', () => {
      // The framed figure already carries the photograph's proportions, so filling
      // it edge to edge is what makes the sharp image land without moving a pixel.
      const under = declarations(
        ruleBody(css, /\.kk-viewer__figure > \.kk-viewer__image--under\s*(?=\{)/) ?? '',
      )
      expect(under.get('position')).toBe('absolute')
      expect(under.get('inset')).toBe('0')
      expect(under.get('width')).toBe('100%')
      expect(under.get('height')).toBe('100%')
      // It is scenery, never a target: the gesture surface underneath must keep
      // receiving the touches that pinch and swipe the photograph.
      expect(under.get('pointer-events')).toBe('none')
    })
  })

  describe('stage geometry', () => {
    it('stamps the figure with the photo’s display aspect ratio so the overlay lands true', async () => {
      // The figure is given the exact frame ratio inline, so its box IS the
      // rendered image (no letterbox for the percentage face boxes to drift into).
      // Before the image has loaded that ratio is the catalogue row's estimate,
      // which is what keeps the stage from resizing when the photo arrives.
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(stageFigure()).not.toBeNull()
      expect(stageFigure()).toHaveAttribute('data-framed', 'true')
      expect(stageFigure()).toHaveStyle({ aspectRatio: '4000 / 3000' })
    })

    it('swaps the estimated ratio for a quarter-turn EXIF orientation', async () => {
      // Orientations 5–8 rotate the image a quarter turn; the thumbnailer bakes
      // that in and markers live in that display space, so the estimate must too.
      fetchPhotoMock.mockResolvedValue(
        photo({ file_width: 4000, file_height: 3000, file_orientation: 6 }),
      )
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(stageFigure()).toHaveStyle({ aspectRatio: '3000 / 4000' })
    })

    it('hands the figure over to the loaded image’s own dimensions', async () => {
      // `naturalWidth`/`naturalHeight` are post-orientation, so once the preview is
      // there the browser has already told us the truth — and the row, whatever it
      // says, stops mattering.
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      expect(frameRatio(stageFigure())).toBeCloseTo(4000 / 3000)

      loadPreview(1920, 1440)
      expect(stageFigure()).toHaveStyle({ aspectRatio: '1920 / 1440' })
      // A correct row and its image agree, so nothing moved: same frame, same
      // letterboxing, same zoom behaviour as before.
      expect(frameRatio(stageFigure())).toBeCloseTo(4000 / 3000)
    })

    it('frames a transposed row from the image, not from the row', async () => {
      // Production photo `phqale6fftf3a3v5tn17vtfd3d`: stored 3000x4000 with EXIF
      // orientation 6 — dimensions that were already oriented and then got rotated
      // a second time — while the file, and the preview, are portrait. Framed from
      // that row the figure is 1.78x too wide and one of the three faces lands off
      // the right edge of the image entirely.
      fetchPhotoMock.mockResolvedValue(
        photo({ file_width: 3000, file_height: 4000, file_orientation: 6 }),
      )
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      expect(frameRatio(stageFigure())).toBeCloseTo(4000 / 3000)

      loadPreview(1440, 1920)
      // The frame a correct row (4000x3000 + orientation 6) would have produced, so
      // the boxes land exactly where they land on a repaired row.
      expect(frameRatio(stageFigure())).toBeCloseTo(3000 / 4000)
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

    it('answers ? with the shortcuts overlay, navbar or no navbar', async () => {
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // The viewer is rendered outside the Layout, so it carries its own
      // triggerless copy of the help — the screen with the most keys of all
      // would otherwise be the one screen that cannot list them.
      fireEvent.keyDown(document, { key: '?' })
      expect(await screen.findByText('Previous / next photo')).toBeInTheDocument()
      expect(screen.getByText('Set the star rating (0 clears it)')).toBeInTheDocument()
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

  /**
   * What the library has computed about this photo is a visible block of the
   * drawer, not a row inside the collapsed technical card: "why does this photo
   * not come up in search?" deserves an answer without an extra click.
   */
  describe('processing block', () => {
    it('lists the steps without expanding the technical card', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({
          processing: [
            { step: 'metadata', state: 'done', at: '2026-08-17T10:00:00Z' },
            { step: 'image_embed', state: 'queued' },
            { step: 'places', state: 'skipped' },
          ],
        }),
      )
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      expect(screen.getByText('Metadata from the file')).toBeInTheDocument()
      expect(screen.getByText('Waiting in the queue')).toBeInTheDocument()
      // Still collapsed: the block stands on its own.
      expect(screen.getByRole('button', { name: 'Technical details' })).toHaveAttribute(
        'aria-expanded',
        'false',
      )
    })

    it('renders nothing when the instance reports no processing', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      expect(screen.queryByText('Metadata from the file')).not.toBeInTheDocument()
    })

    it('offers no run button to a non-maintainer', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({ processing: [{ step: 'image_embed', state: 'queued' }] }),
      )
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      expect(screen.getByText('Waiting in the queue')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /run now/i })).not.toBeInTheDocument()
    })
  })

  /**
   * The shut drawer and the keyboard. It slides out rather than unmounting, so all
   * of its controls stay laid out — and a laid-out control is a tabbable one.
   * Measured on production at 1440px: Tab left the visible chrome and landed on
   * "Close info" INSIDE the shut panel, the browser scrolled the photograph 416px
   * off screen to reveal it, and kept it there for 17 more stops. Two defects in
   * one, because the panel is `aria-hidden` the whole time it holds that focus —
   * a WCAG 4.1.2 violation, as a screen reader announces nothing from there.
   */
  describe('the shut drawer and the keyboard', () => {
    it('keeps every control of the shut drawer out of the tab order', async () => {
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // Shut, but fully mounted: there ARE controls in there — that is the whole
      // problem, and an assertion about "no reachable control" would otherwise pass
      // on an empty panel and prove nothing.
      expect(drawer(container).querySelectorAll(FOCUSABLE).length).toBeGreaterThan(0)
      expect(drawer(container)).toHaveAttribute('inert')
      expect(tabbableInDrawer(container)).toHaveLength(0)
    })

    it('gives the panel back to the keyboard when it opens', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      expect(drawer(container)).not.toHaveAttribute('inert')
      expect(tabbableInDrawer(container).length).toBeGreaterThan(0)
      expect(screen.getByRole('button', { name: 'Close info' })).toBeInTheDocument()
    })

    it('hands focus back to the info toggle when closed from inside itself', async () => {
      const user = userEvent.setup()
      const { container } = renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      // The ✕ lives in the drawer, so shutting it takes the focused control away
      // with it. Focus must land on the toggle that brings the panel back, not on
      // <body>, where the next Tab would restart at the top of the page.
      await user.click(screen.getByRole('button', { name: 'Close info' }))
      expect(drawer(container)).toHaveAttribute('inert')
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Info' })).toHaveFocus()
      })
    })

    it('leaves the keyboard alone when the drawer is shut from the chrome', async () => {
      const user = userEvent.setup()
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await openInfo(user)

      // Closing from the top bar: focus is already on a visible control, so nothing
      // is moved out from under the reader.
      await openInfo(user)
      expect(screen.getByRole('button', { name: 'Info' })).toHaveFocus()
    })

    it('takes the faces view — the same drawer — out of the tab order too', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      const { container } = renderPage()
      await user.click(await screen.findByRole('button', { name: 'Show faces' }))
      expect(drawer(container)).not.toHaveAttribute('inert')

      // Faces are a VIEW of this one drawer, not a panel of their own, so closing
      // them shuts the same element — and the same fix covers them.
      await user.click(screen.getByRole('button', { name: 'Close the faces panel' }))
      expect(drawer(container)).toHaveAttribute('inert')
      expect(tabbableInDrawer(container)).toHaveLength(0)
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Show faces' })).toHaveFocus()
      })
    })

    it('hides the shut drawer in CSS too, without cutting its slide-out', () => {
      // `inert` is the fix; `visibility: hidden` is the same rule for a browser too
      // old for it — and the one hiding switch that can be delayed past the slide,
      // so the drawer is still seen leaving.
      const css = readCss('src/components/photo/viewer.css')
      const panel = declarations(ruleBody(css, /\.kk-viewer__panel\s*(?=\{)/) ?? '')
      expect(panel.get('visibility')).toBe('hidden')
      expect(panel.get('transition')).toContain('visibility 0s linear var(--kk-duration-base)')

      const open = declarations(ruleBody(css, /\.kk-viewer__panel\.is-open\s*(?=\{)/) ?? '')
      expect(open.get('visibility')).toBe('visible')
      // No delay on the way in: it must be visible from the first frame.
      expect(open.get('transition')).toContain('visibility 0s')
      expect(open.get('transition')).not.toContain('visibility 0s linear')
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
      // The boxes wait for the preview to report the frame they are percentages of.
      expect(screen.queryByRole('button', { name: 'Unnamed face 1' })).not.toBeInTheDocument()
      loadPreview()
      expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeEnabled()
      expect(screen.getByRole('button', { name: 'Unnamed face 2' })).toBeEnabled()
      // The panel lists the same faces.
      expect(screen.getByText('Faces: 2')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Select face #1: No name' })).toBeInTheDocument()
      // Still exactly one *preview* — the boxes are drawn over it, and the crops in
      // the panel rows and the people chips are small `fit_*` cut-outs, so the
      // invariant is counted in full-size previews.
      const previews = [...container.querySelectorAll('img')].filter((img) =>
        img.getAttribute('src')?.includes(STAGE_SIZE),
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

    it('leaves a video with a view group of one, and the library menu intact', async () => {
      // Neither the edit panel nor the face boxes apply to a video, so the view
      // group shrinks to the info toggle alone rather than leaving an empty
      // group — and the library operations, which apply to any medium, are
      // unaffected.
      const user = userEvent.setup()
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

      const views = screen.getByRole('group', { name: 'Photo views' })
      expect(within(views).getByRole('button', { name: 'Info' })).toBeInTheDocument()
      expect(within(views).queryByRole('button', { name: 'Edits' })).toBeNull()
      expect(within(views).queryByRole('button', { name: 'Show faces' })).toBeNull()

      await openLibrary(user)
      expect(screen.getByRole('button', { name: 'Hide from the library' })).toBeInTheDocument()
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

      // Closing returns to the originating scoped list (album), carrying its
      // state — measured against the album page's own defaults, which rest at
      // oldest-first, so that order needs no param and a reversal would get one.
      await user.click(screen.getByRole('button', { name: 'Back to the list' }))
      await waitFor(() => {
        expect(screen.getByTestId('location')).toHaveTextContent('/albums/al_1')
      })
      expect(screen.getByTestId('location')).not.toHaveTextContent('sort=')
    })

    it('carries a reversed album order back to the album on close', async () => {
      const user = userEvent.setup()
      renderPage(true, '/photos/b?sort=newest&album=al_1')

      await screen.findByRole('heading', { name: 'Beach' })
      await user.click(screen.getByRole('button', { name: 'Back to the list' }))
      await waitFor(() => {
        expect(screen.getByTestId('location')).toHaveTextContent('/albums/al_1')
      })
      expect(screen.getByTestId('location')).toHaveTextContent('sort=newest')
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

    it('pages prev/next as full-text while the embeddings box is offline', async () => {
      renderPage(true, '/photos/b?q=beach&mode=semantic', false)

      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(searchPhotosMock).toHaveBeenCalled()
      })
      // The grid this photo was opened from ran full-text too, so following it is
      // both the right order and the only one that answers without waiting out
      // the offline sidecar's timeout.
      expect(searchPhotosMock.mock.calls[0][1]).toBe('fulltext')
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

    it('tells the list which photograph the reader paged to', async () => {
      window.sessionStorage.clear()
      // The library was left scrolled, so there is a position for a photograph to
      // sit in; paging replaces the history entry, so only this keeps the way back
      // pointed at what is actually on stage.
      writeGridScroll('/?sort=oldest', { count: 0, scrollY: 4000 })

      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(readGridScroll('/?sort=oldest')?.uid).toBe('b')
      })
      await screen.findByRole('link', { name: 'Next photo' })

      fireEvent.keyDown(document, { key: 'ArrowRight' })

      await waitFor(() => {
        expect(readGridScroll('/?sort=oldest')?.uid).toBe('c')
      })
    })

    it('pages on an arrow pressed before the list order has arrived', async () => {
      // A photo opened directly — a shared link, a refresh, Back/Forward — has to
      // find its place in the list first, and that walk takes a moment. An arrow
      // pressed in that window used to be dropped, so the keys read as dead until
      // the user did something else that happened to take long enough.
      let resolveList!: (res: PhotoListResponse) => void
      fetchPhotosMock.mockReturnValue(
        new Promise<PhotoListResponse>((resolve) => {
          resolveList = resolve
        }),
      )

      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })
      // Still resolving: there is not even an on-screen arrow to click yet.
      expect(screen.queryByRole('link', { name: 'Next photo' })).not.toBeInTheDocument()

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')

      resolveList(page(['a', 'b', 'c']))
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
    })

    it('pages back on a left arrow pressed before the list order has arrived', async () => {
      let resolveList!: (res: PhotoListResponse) => void
      fetchPhotosMock.mockReturnValue(
        new Promise<PhotoListResponse>((resolve) => {
          resolveList = resolve
        }),
      )

      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 'ArrowLeft' })
      resolveList(page(['a', 'b', 'c']))
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/a')
      })
    })

    it('drops a remembered arrow when the answer lands while a text field has focus', async () => {
      // The wait is long enough for the user to give up on the key and start
      // writing instead; a photo swapping out from under a caret is exactly what
      // the shortcut guard exists to prevent, so it is re-checked on arrival.
      let resolveList!: (res: PhotoListResponse) => void
      fetchPhotosMock.mockReturnValue(
        new Promise<PhotoListResponse>((resolve) => {
          resolveList = resolve
        }),
      )

      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })

      fireEvent.keyDown(document, { key: 'ArrowRight' })

      // Stands in for the comment composer / caption field: the guard is one
      // predicate over `document.activeElement`, and this focuses a real one
      // without dragging the whole drawer into a keyboard test.
      const composer = document.createElement('textarea')
      document.body.append(composer)
      composer.focus()

      resolveList(page(['a', 'b', 'c']))
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'Next photo' })).toBeInTheDocument()
      })
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/b')
      composer.remove()
    })

    it('leaves a right arrow at the end of the list a no-op, now and later', async () => {
      // "There is no next photo" must not be remembered as "we do not know yet",
      // or the last photo would jump the moment anything else refreshed.
      fetchPhotosMock.mockResolvedValue(page(['a', 'b', 'c']))
      renderPage(true, '/photos/c?sort=oldest')
      await screen.findByRole('link', { name: 'Previous photo' })
      expect(screen.queryByRole('link', { name: 'Next photo' })).not.toBeInTheDocument()

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
    })

    it('re-points the arrows at once when paging, without re-walking the list', async () => {
      // The neighbour pair belongs to one photo. While a fresh walk was in flight
      // the previous photo's pair stayed on screen, so "next" pointed at the photo
      // already open and a second press went nowhere.
      fetchPhotosMock.mockResolvedValue(page(['a', 'b', 'c', 'd']))
      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('link', { name: 'Next photo' })
      const walked = fetchPhotosMock.mock.calls.length

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/c')
      })
      expect(screen.getByRole('link', { name: 'Next photo' }).getAttribute('href')).toContain(
        '/photos/d',
      )
      expect(fetchPhotosMock.mock.calls.length).toBe(walked)

      fireEvent.keyDown(document, { key: 'ArrowRight' })
      await waitFor(() => {
        expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/d')
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

    it('warms the photo on stage and its two neighbours, but never a video', async () => {
      // Paging is the most repeated gesture in the app, so the neighbours' bytes
      // are on the machine before the arrow is pressed. A video neighbour is left
      // alone: its stage streams the file itself, and pulling a clip down for a
      // photo nobody may step to is the one preload that costs more than it saves.
      const videos = page(['a', 'b', 'c'])
      videos.photos[2] = { ...videos.photos[2], media_type: 'video' }
      fetchPhotosMock.mockResolvedValue(videos)

      renderPage(true, '/photos/b?sort=oldest')
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('link', { name: 'Next photo' })

      await waitFor(() => {
        expect(preloaded()).toContain(`/api/v1/photos/a/thumb/${STAGE_SIZE}`)
      })
      expect(preloaded()).toContain(`/api/v1/photos/b/thumb/${STAGE_SIZE}`)
      expect(preloaded().some((url) => url.includes('/photos/c/thumb/'))).toBe(false)
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

    /**
     * On a phone the bar's fixed-size controls leave the heading about 96 px, and
     * the label used to truncate as one run of text — losing the year, which is
     * the end of the date in both locales, and with it the only part a reader
     * cannot reconstruct. The bar now sheds the CLOCK instead, which it can only
     * do if the clock is an element of its own inside the date.
     *
     * jsdom evaluates no container query, so what is asserted here is the shape
     * that makes the shortening possible: with the time element taken out, what
     * remains still reads as a whole date, year included.
     */
    it('keeps the year when the header sheds the time on a narrow bar', async () => {
      fetchPhotoMock.mockResolvedValue(
        photo({ title: '', file_name: 'IMG_1234.jpg', taken_at: '2026-01-02T10:00:00Z' }),
      )
      renderPage()

      const heading = await screen.findByRole('heading', { level: 1 })
      const date = heading.querySelector('.kk-viewer__title-date')
      if (date === null) {
        throw new Error('the heading renders no capture date')
      }
      const time = date.querySelector('.kk-viewer__title-time')
      if (time === null) {
        throw new Error('the capture date renders no separable time')
      }

      // It really is a shortening of the app's own label, not a second formatting
      // of the same instant.
      const full = date.textContent
      expect(full).toBe(
        new Date('2026-01-02T10:00:00Z').toLocaleString('en', {
          year: 'numeric',
          month: 'numeric',
          day: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
        }),
      )

      // What the phone is left with once the clock is dropped.
      const shortened = full.replace(time.textContent, '')
      expect(shortened).toContain('2026')
      expect(shortened).toBe(
        new Date('2026-01-02T10:00:00Z').toLocaleDateString('en', {
          year: 'numeric',
          month: 'numeric',
          day: 'numeric',
        }),
      )
      expect(shortened).not.toMatch(/\d{1,2}:\d\d/)
    })

    it('lets the place give way rather than the date it follows', async () => {
      // The two are separate elements for one reason: only the place may be
      // ellipsised. If they ever fold back into one run of text, the year goes
      // first again.
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
      const date = heading.querySelector('.kk-viewer__title-date')
      const place = heading.querySelector('.kk-viewer__title-muted')
      expect(date?.textContent).toContain('2026')
      expect(date?.textContent).not.toContain('Brno')
      expect(place?.textContent).toContain('Brno')
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
      // The full-size preview has landed, so the progressive stand-in is gone and
      // the photo is on the stage exactly once.
      loadPreview()

      // The panel is not up until asked for, and the photo has the screen to itself.
      expect(screen.queryByRole('button', { name: 'Rotate right' })).not.toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Edits' }))
      expect(viewer(container)).toHaveAttribute('data-panel', 'open')

      const main = screen.getByRole('img', { name: 'Beach' })
      await user.click(screen.getByRole('button', { name: 'Rotate right' }))
      expect(main).toHaveStyle({ transform: 'rotate(90deg)' })
      // The page still carries exactly one copy of the photo (the progressive
      // stand-in went the moment the preview loaded).
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

    it('keeps the faces on a rotated photo — the boxes follow the turn', async () => {
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      // A photo saved rotated: FaceOverlay maps its boxes through the rotation, so
      // turning a photo upright no longer costs the reader their face rectangles.
      fetchEditMock.mockResolvedValue({ ...NEUTRAL, rotation: 90 })
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      expect(await screen.findByRole('button', { name: 'Show faces' })).toBeInTheDocument()
      fireEvent.keyDown(document, { key: 'm' })
      expect(await screen.findByTestId('face-overlay')).toBeInTheDocument()
    })

    it('stands the faces down for a crop, and brings them back when it is off', async () => {
      const user = userEvent.setup()
      fetchFacesMock.mockResolvedValue(facesResponse(2))
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await screen.findByRole('button', { name: 'Edits' })

      // A crop leaves a frame the boxes were never measured against, so every
      // rectangle would be off its face: the face UI stands down while it is on.
      await user.click(screen.getByRole('button', { name: 'Edits' }))
      await user.click(screen.getByRole('checkbox', { name: 'Crop' }))
      expect(screen.queryByRole('button', { name: 'Show faces' })).not.toBeInTheDocument()

      await user.click(screen.getByRole('checkbox', { name: 'Crop' }))
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
    loadPreview()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: 'Select face #1: No name' }),
    ).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })

  describe('progressive display', () => {
    /** The smaller rendition painted under the full-size image, if any. */
    function underImage(): HTMLImageElement | null {
      return document.querySelector<HTMLImageElement>('img.kk-viewer__image--under')
    }

    it('paints the grid rendition under the photo and drops it when the real one lands', async () => {
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })

      // Under the full-size image, filling the same framed box, and decorative:
      // the image above it owns the alt text.
      const under = underImage()
      expect(under).not.toBeNull()
      expect(under?.getAttribute('src')).toContain(`/thumb/${GRID_PREVIEW_SIZE}`)
      expect(under).toHaveAttribute('aria-hidden', 'true')
      expect(under?.parentElement).toBe(stageFigure())
      expect(under?.compareDocumentPosition(screen.getByRole('img', { name: 'Beach' }))).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      )

      loadPreview()
      expect(underImage()).toBeNull()
    })

    it('opens in the very image the grid painted when the grid hands its address over', async () => {
      // The address is carried across the route change because it cannot be
      // rebuilt: a rendition URL is signed per response, so the one the viewer
      // would compute is a different string and therefore a second download.
      renderPage(true, {
        pathname: '/photos/b',
        search: '?sort=oldest',
        state: { uid: 'b', previewUrl: 'https://media.example/b-720.jpg?sig=abc' },
      })
      await screen.findByRole('heading', { name: 'Beach' })

      expect(underImage()).toHaveAttribute('src', 'https://media.example/b-720.jpg?sig=abc')
    })

    it('ignores a handoff left behind by another photo', async () => {
      // History state outlives the navigation that made it: Back/Forward, and the
      // viewer's own `replace` paging, can present the previous photo's address.
      renderPage(true, {
        pathname: '/photos/b',
        search: '?sort=oldest',
        state: { uid: 'a', previewUrl: 'https://media.example/a-720.jpg' },
      })
      await screen.findByRole('heading', { name: 'Beach' })

      expect(underImage()?.getAttribute('src')).toContain(`/photos/b/thumb/${GRID_PREVIEW_SIZE}`)
    })

    it('spends nothing on a smaller rendition of a photo already warmed', async () => {
      // Stepping to a preloaded neighbour swaps instantly, so a stand-in would be
      // bytes spent on a frame nobody ever sees.
      renderPage()
      await screen.findByRole('heading', { name: 'Beach' })
      await waitFor(() => {
        expect(preloaded()).toContain(`/api/v1/photos/b/thumb/${STAGE_SIZE}`)
      })

      act(() => {
        StubImage.finish(`/api/v1/photos/b/thumb/${STAGE_SIZE}`)
      })
      await waitFor(() => {
        expect(underImage()).toBeNull()
      })
    })
  })
})

describe('PhotoDetailPage comment badge', () => {
  it('badges the info toggle with the thread size, in the name as well as the disc', async () => {
    fetchPhotoMock.mockResolvedValue(photo({ comment_count: 3 }))
    renderPage()

    const toggle = await screen.findByRole('button', { name: 'Info – 3 comments' })
    expect(within(toggle).getByText('3')).toBeInTheDocument()
  })

  it('leaves the toggle unbadged when nobody has said anything', async () => {
    renderPage()

    const toggle = await screen.findByRole('button', { name: 'Info' })
    expect(within(toggle).queryByText('0')).not.toBeInTheDocument()
  })

  it('caps the disc at 99+ while the name keeps the real number', async () => {
    fetchPhotoMock.mockResolvedValue(photo({ comment_count: 120 }))
    renderPage()

    const toggle = await screen.findByRole('button', { name: 'Info – 120 comments' })
    expect(within(toggle).getByText('99+')).toBeInTheDocument()
  })

  it('re-badges after a comment is posted, without reloading the photo', async () => {
    const user = userEvent.setup()
    fetchPhotoMock.mockResolvedValue(photo({ comment_count: 1 }))
    fetchCommentsMock.mockResolvedValue([
      {
        uid: 'cm_1',
        photo_uid: 'b',
        author_uid: 'u1',
        author_name: 'U',
        body: 'The barn is still standing here.',
        created_at: '2026-01-02T10:00:00Z',
      },
    ])
    createCommentMock.mockResolvedValue({
      uid: 'cm_2',
      photo_uid: 'b',
      author_uid: 'u1',
      author_name: 'U',
      body: 'And that is the well.',
      created_at: '2026-01-02T11:00:00Z',
    })
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Info – 1 comment' }))
    await user.type(await screen.findByLabelText('New comment'), 'And that is the well.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    // The count comes from the open thread, not from a second GET of the photo.
    expect(await screen.findByRole('button', { name: 'Info – 2 comments' })).toBeInTheDocument()
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
  })
})

describe('PhotoDetailPage — the blurred stand-in', () => {
  /** A real BlurHash of a real photograph (the woltapp reference string). */
  const hash = 'LEHV6nWB2yk8pyo0adR*.7kCMdnj'

  beforeEach(() => {
    clearBlurPlaceholderCache()
  })

  it("paints the photo's blur across the stage until the image lands", async () => {
    stubBlurCanvas()
    fetchPhotoMock.mockResolvedValue(photo({ blurhash: hash }))
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    const blur = document.querySelector('.kk-media-blur')
    expect(blur).toBeInTheDocument()
    expect(blur).toHaveStyle({ backgroundImage: `url("${STUB_CANVAS_DATA_URL}")` })
    // Inside the framed figure — the box already sized to the photo's shape — so
    // the blur stands exactly where the image will be and nothing moves.
    expect(blur?.parentElement).toBe(stageFigure())

    // Once the image is there it owns the stage; the blur goes before any zoom or
    // rotation can move the image off it.
    loadPreview()
    await waitFor(() => {
      expect(document.querySelector('.kk-media-blur')).not.toBeInTheDocument()
    })
  })

  it('opens on the neutral stage for a photo whose placeholder was never computed', async () => {
    stubBlurCanvas()
    renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    expect(document.querySelector('.kk-media-blur')).not.toBeInTheDocument()
  })
})

describe('the grid → viewer morph', () => {
  /**
   * Mounts the viewer with the view-transition plumbing already saying "this
   * photo is the one being morphed", which is what the provider does with
   * `flushSync` before it asks the browser for a transition. jsdom implements
   * none of the API, so what is asserted here is the wiring — which element
   * carries the mark, and that both of the close button's ways out are routed
   * through the morph — and never the animation.
   *
   * `from` decides how the viewer was reached: opened from a grid inside the
   * same history (Back can pop) or straight from a link (it cannot, and the list
   * URL has to be pushed instead).
   */
  function renderMorphing(overrides: Partial<MorphState> = {}, from: 'grid' | 'link' = 'link') {
    const morph = vi.fn((_id: string, navigate: () => void) => {
      navigate()
    })
    const value: MorphState = { enabled: true, morphId: 'b', morph, ...overrides }
    const entries = from === 'grid' ? ['/', '/photos/b?sort=oldest'] : ['/photos/b?sort=oldest']
    render(
      <I18nextProvider i18n={i18n}>
        <CapabilitiesContext.Provider
          value={{ semantic_search: true, known: true, passkeys: false }}
        >
          <AuthContext.Provider value={auth(true)}>
            <MorphContext.Provider value={value}>
              <MemoryRouter initialEntries={entries}>
                <Routes>
                  <Route path="/photos/:uid" element={<PhotoDetailPage />} />
                  {/* The list the viewer returns to, whichever query it
                      reconstructs. */}
                  <Route path="*" element={<div>grid</div>} />
                </Routes>
                <LocationProbe />
              </MemoryRouter>
            </MorphContext.Provider>
          </AuthContext.Provider>
        </CapabilitiesContext.Provider>
      </I18nextProvider>,
    )
    return { morph }
  }

  it('marks the photograph on stage as the half the tile grows into', async () => {
    renderMorphing()
    await screen.findByRole('heading', { name: 'Beach' })

    // The figure, not the stage around it: the tile has to grow into the
    // photograph, not into the letterbox the photograph sits in.
    expect(stageFigure()).toHaveAttribute('data-kk-morph')
  })

  it('marks nothing while another photo is the one being morphed', async () => {
    renderMorphing({ morphId: 'a' })
    await screen.findByRole('heading', { name: 'Beach' })

    expect(stageFigure()).not.toHaveAttribute('data-kk-morph')
  })

  it('closes back into the grid through the morph, popping history', async () => {
    const user = userEvent.setup()
    const { morph } = renderMorphing({}, 'grid')
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('button', { name: 'Back to the list' }))

    expect(morph).toHaveBeenCalledTimes(1)
    expect(morph.mock.calls[0][0]).toBe('b')
    // Back still pops — the morph wraps the navigation, it does not replace it.
    expect(screen.getByTestId('location')).toHaveTextContent('/')
  })

  it('closes through the morph for a photo opened from a bare link too', async () => {
    const user = userEvent.setup()
    const { morph } = renderMorphing()
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('button', { name: 'Back to the list' }))

    // Nothing to pop back to, so the reconstructed list URL is pushed instead —
    // still through the same single entry point.
    expect(morph).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('location')).toHaveTextContent('/?sort=oldest')
  })

  it('navigates exactly as before when the plumbing is the inert default', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <CapabilitiesContext.Provider
          value={{ semantic_search: true, known: true, passkeys: false }}
        >
          <AuthContext.Provider value={auth(true)}>
            <MemoryRouter initialEntries={['/', '/photos/b?sort=oldest']}>
              <Routes>
                <Route path="/" element={<div>grid</div>} />
                <Route path="/photos/:uid" element={<PhotoDetailPage />} />
              </Routes>
              <LocationProbe />
            </MemoryRouter>
          </AuthContext.Provider>
        </CapabilitiesContext.Provider>
      </I18nextProvider>,
    )
    await screen.findByRole('heading', { name: 'Beach' })

    await user.click(screen.getByRole('button', { name: 'Back to the list' }))

    // No provider at all — the fallback every browser without the API takes.
    // Back still works, which is the hard rule the morph must not touch.
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/')
    })
    expect(stageFigure()).toBeNull()
  })
})

describe('the first frame while the photo is still on the wire', () => {
  it('opens on the very image the grid painted when one was handed over', async () => {
    // A promise that never settles: the assertion is about what the viewer shows
    // *before* the photo's row arrives.
    fetchPhotoMock.mockImplementation(() => new Promise<PhotoDetail>(() => undefined))
    renderPage(true, {
      pathname: '/photos/b',
      search: '?sort=oldest',
      state: { uid: 'b', previewUrl: 'https://media.example/b-720.jpg?sig=abc' },
    })

    const first = await waitFor(() => {
      const img = document.querySelector<HTMLImageElement>('img.kk-viewer__image')
      expect(img).not.toBeNull()
      return img
    })
    expect(first).toHaveAttribute('src', 'https://media.example/b-720.jpg?sig=abc')
    // Decorative: the photo's name is not known yet, and the wait is announced
    // beside it instead.
    expect(first).toHaveAttribute('alt', '')
    expect(screen.getByRole('status')).toHaveTextContent('Loading photo…')
  })

  it('falls back to the spinner for a photo opened from a bare link', async () => {
    fetchPhotoMock.mockImplementation(() => new Promise<PhotoDetail>(() => undefined))
    renderPage(true, '/photos/b?sort=oldest')

    expect(await screen.findByRole('status')).toBeInTheDocument()
    expect(document.querySelector('img.kk-viewer__image')).toBeNull()
  })

  it('offers no toolbar at all until the row lands', async () => {
    // A photo opened straight from a URL has no rating, no flags and no hidden
    // state to show yet, so the grouped bar is not rendered half-built: the way
    // back is the only control on screen, and none of the groups — least of all
    // the library menu, which would act on nothing — is offered early.
    fetchPhotoMock.mockImplementation(() => new Promise<PhotoDetail>(() => undefined))
    const { container } = renderPage(true, '/photos/b?sort=oldest')

    expect(await screen.findByRole('status')).toBeInTheDocument()
    expect(container.querySelector('.kk-viewer__actions')).toBeNull()
    expect(screen.queryByRole('group', { name: 'Photo assessment' })).toBeNull()
    expect(screen.queryByRole('group', { name: 'Photo views' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Library actions' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Back to the list' })).toBeInTheDocument()
  })
})

/**
 * The one-time hint. A viewer whose controls have just faded looks broken to
 * someone who has never been told that a tap brings them back — and a phone
 * offers no pointer move to stumble on the answer with. These pin the two halves
 * of the answer: the chrome is held up longer the first time, and when it does go
 * it says how to get it back — once per device, never on the fiftieth photo.
 */
describe('the first-run chrome hint on a phone', () => {
  const HINT = 'Tap the photo to bring the controls back'

  /** A finger-pointed phone: both the narrow width and the coarse pointer. */
  function mockPhone(): void {
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: query === NARROW_VIEWPORT_QUERY || query === COARSE_POINTER_QUERY,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  }

  /** Runs the clock to the first fade: the first-run hold, then the idle. */
  function fadeChrome(): void {
    act(() => {
      vi.advanceTimersByTime(FIRST_RUN_HOLD_MS)
    })
    act(() => {
      vi.advanceTimersByTime(DEFAULT_IDLE_MS)
    })
  }

  beforeEach(() => {
    window.localStorage.removeItem('kukatko.viewer.chromeHintSeen')
    // `shouldAdvanceTime` keeps the page's own async work (fetches, findBy*)
    // running while the idle timers are driven by hand.
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockPhone()
  })

  afterEach(() => {
    vi.useRealTimers()
    window.localStorage.removeItem('kukatko.viewer.chromeHintSeen')
  })

  it('holds the chrome up past the ordinary idle, then explains its going', async () => {
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    // The moment the bar would normally have gone: still there, and silent —
    // there is nothing to explain while the controls are on screen.
    act(() => {
      vi.advanceTimersByTime(DEFAULT_IDLE_MS)
    })
    expect(viewer(container)).toHaveAttribute('data-chrome', 'visible')
    expect(screen.queryByText(HINT)).toBeNull()

    fadeChrome()
    expect(viewer(container)).toHaveAttribute('data-chrome', 'hidden')
    expect(screen.getByText(HINT)).toBeInTheDocument()
  })

  it('never says it twice on the same device', async () => {
    const first = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    fadeChrome()
    expect(screen.getByText(HINT)).toBeInTheDocument()
    first.unmount()

    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })
    // No hold either: the second photo fades on the ordinary idle.
    act(() => {
      vi.advanceTimersByTime(DEFAULT_IDLE_MS)
    })
    expect(viewer(container)).toHaveAttribute('data-chrome', 'hidden')
    expect(screen.queryByText(HINT)).toBeNull()
  })

  it('says nothing at all where there is a mouse', async () => {
    mockViewport(false)
    const { container } = renderPage()
    await screen.findByRole('heading', { name: 'Beach' })

    act(() => {
      vi.advanceTimersByTime(DEFAULT_IDLE_MS)
    })
    expect(viewer(container)).toHaveAttribute('data-chrome', 'hidden')
    expect(screen.queryByText(HINT)).toBeNull()
  })
})

/**
 * jsdom computes no layout, so where the hint lands — clear of the bottom edge
 * and of the home indicator, and above all not in the way of the very tap it is
 * asking for — is pinned by reading the stylesheet.
 */
describe('the chrome hint stylesheet', () => {
  const css = readCss('src/components/photo/viewer.css')
  const hint = declarations(ruleBody(css, /\.kk-viewer__hint\s*(?=\{)/) ?? '')

  it('never swallows the tap it asks for', () => {
    expect(hint.get('pointer-events')).toBe('none')
  })

  it('sits above the bottom edge, clear of the home indicator', () => {
    expect(hint.get('position')).toBe('absolute')
    const bottom = hint.get('bottom') ?? ''
    // Well up from the edge — where the dock will reappear — and carrying the
    // inset itself, exactly as the dock does (`viewport-fit=cover` is real).
    expect(bottom).toMatch(/^calc\(/)
    expect(bottom).toContain('env(safe-area-inset-bottom, 0px)')
    // Above the dock's layer for the moment their fades cross, below the
    // persistent back control (5).
    expect(Number(hint.get('z-index'))).toBeGreaterThan(3)
  })
})
