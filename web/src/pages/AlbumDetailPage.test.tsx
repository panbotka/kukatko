import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Album } from '../services/organize'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { AlbumDetailPage } from './AlbumDetailPage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout).
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ data, itemContent }: MockGridProps) => (
    <div data-testid="grid">
      {data.map((item, index) => (
        <div key={item.uid}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbum: vi.fn(),
    deleteAlbum: vi.fn(),
    removeAlbumPhotos: vi.fn(),
    updateAlbum: vi.fn(),
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
  }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbum, deleteAlbum, removeAlbumPhotos, fetchAlbums, fetchLabels } =
  await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchAlbumMock = vi.mocked(fetchAlbum)
const deleteAlbumMock = vi.mocked(deleteAlbum)
const removeMock = vi.mocked(removeAlbumPhotos)
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

function album(): Album {
  return {
    uid: 'al_1',
    slug: 'holidays',
    title: 'Holidays',
    description: '',
    type: 'album',
    private: false,
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
        <MemoryRouter initialEntries={['/albums/al_1']}>
          <Routes>
            <Route path="/albums/:uid" element={<AlbumDetailPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchAlbumMock.mockReset()
  deleteAlbumMock.mockReset()
  removeMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  removeMock.mockResolvedValue([])
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AlbumDetailPage', () => {
  it('scopes the photo grid to the album from the URL', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Holidays' })).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    expect(fetchPhotosMock.mock.calls[0][0].album).toBe('al_1')
  })

  it('lets a long unbroken album title wrap inside the header', async () => {
    // A title with no spaces used to hold the header open past a phone's
    // viewport: `.kk-page-title` now breaks anywhere, and its group is allowed
    // to shrink so the break can happen.
    const long = 'Dovolena'.repeat(20)
    fetchAlbumMock.mockResolvedValue({ ...album(), title: long })
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    const heading = await screen.findByRole('heading', { name: long })
    expect(heading).toHaveClass('kk-page-title')
    expect(heading.parentElement).toHaveClass('kk-min-w-0')
  })

  it('offers a back link that names the album list it returns to', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // An arrow alone said nothing; the label names the destination, and the
    // arrow itself stays decorative so the link's accessible name is the text.
    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
    expect(back.querySelector('.bi-arrow-left')).toHaveAttribute('aria-hidden', 'true')
  })

  it('names the album list in the back link of the error state too', async () => {
    fetchAlbumMock.mockRejectedValue(new Error('boom'))
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage()

    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
  })

  it('links each tile to the detail page carrying the album scope', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // The tile's detail link carries ?album so pressing Esc/Back on the photo
    // (and prev/next) returns to this album, not the whole library.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?album=al_1')
  })

  it('renders no sort selector and no manual reordering controls', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    // An album is always chronological: the shared filter bar hides its sort
    // selector here (other photo lists keep theirs).
    expect(screen.queryByRole('combobox', { name: 'Sort' })).not.toBeInTheDocument()
    // Manual ordering is gone: no reorder mode, no per-tile drag handles.
    expect(screen.queryByRole('button', { name: 'Reorder' })).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /^Move .+ (earlier|later)$/ }),
    ).not.toBeInTheDocument()
  })

  it('hides mutation controls from viewers', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More edits' })).not.toBeInTheDocument()
  })

  it('deletes the album through the styled confirm dialog, not a native prompt', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    deleteAlbumMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // The row control opens the dialog; nothing is deleted until it is confirmed.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Delete the album "Holidays"/)).toBeInTheDocument()
    expect(deleteAlbumMock).not.toHaveBeenCalled()

    // The confirm button carries the action itself, never "OK".
    await user.click(within(dialog).getByRole('button', { name: 'Delete album' }))
    await waitFor(() => {
      expect(deleteAlbumMock).toHaveBeenCalledWith('al_1')
    })
  })

  it('closes the confirm dialog without deleting when cancelled', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(deleteAlbumMock).not.toHaveBeenCalled()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    // No "Select" step: the tile is a link that already carries its checkmark,
    // exactly as on the library, and the selection bar is still out of the way.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    // Picking raises the selection bar with the album's selection actions.
    expect(screen.getByRole('button', { name: 'Set as cover' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove from album' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More edits' })).toBeEnabled()
  })

  it('raises the library’s full batch bar, with the album’s own actions merged in', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    // One toolbar, not two: the album's actions live on the shared bar next to
    // the full batch vocabulary the library offers.
    const bars = screen.getAllByRole('toolbar', { name: 'Batch actions' })
    expect(bars).toHaveLength(1)
    const [bar] = bars
    for (const name of [
      'Clear selection',
      'Select all',
      'Add to album',
      'Labels',
      'Favorite',
      'Archive',
      'Download ZIP',
      'Stack selected',
      'More edits',
      'Set as cover',
      'Remove from album',
    ]) {
      expect(within(bar).getByRole('button', { name })).toBeInTheDocument()
    }

    // Select-all picks up the rest of the loaded grid, so the count follows.
    await user.click(within(bar).getByRole('button', { name: 'Select all' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()

    // A cover is a single photo: two picked leaves that one action inapplicable.
    expect(within(bar).getByRole('button', { name: 'Set as cover' })).toBeDisabled()
  })

  it('adds the selection to another album straight from the bar, then reloads', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    albumsMock.mockResolvedValue([
      {
        uid: 'al_2',
        slug: 'trips',
        title: 'Trips',
        description: '',
        type: 'album',
        private: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        photo_count: 0,
      },
    ])
    labelsMock.mockResolvedValue([])
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Add to album' }))
    await user.click(await screen.findByLabelText('Add to albums'))
    await user.click(await screen.findByRole('option', { name: /Trips/ }))
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    // Exactly the picked photo, and the album's own grid refetches afterwards.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a'], { add_to_albums: ['al_2'] })
    })
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('bulk-edits exactly the selected photos, then reloads the grid', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(
      page([photo('a', 'a.jpg'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]),
    )
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 2, updated: 2, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Select c.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'More edits' }))
    const dialog = await screen.findByRole('dialog')
    await user.selectOptions(within(dialog).getByLabelText('Favorite'), 'true')
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }))

    // The two picked photos, not the three the album scope matches.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'c'], { set_favorite: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('keeps every header action inline on a wide screen', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // Desktop is unchanged by the phone collapse: no overflow toggle, every
    // action directly on the header row.
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
  })

  it('drops the selection when the selected photos are removed from the album', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Remove from album' }))

    await waitFor(() => {
      expect(removeMock).toHaveBeenCalledWith('al_1', ['a'])
    })
    // The selection is dropped, so no removed UID lingers in it — and with it
    // the bar, handing the header back to the album's own actions.
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })
})

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; a phone-width test
 * overrides it so the header takes its collapsed branch.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/**
 * The header's overflow menu, once opened. react-bootstrap mounts it on the
 * first open and then only toggles the `show` class; jsdom loads no Bootstrap
 * CSS, so a closed menu is that missing class, not a missing node.
 */
function overflowMenu(): HTMLElement {
  const menu = document.querySelector<HTMLElement>('.dropdown-menu')
  if (menu === null) {
    throw new Error('the overflow menu has not been rendered')
  }
  return menu
}

describe('AlbumDetailPage on a narrow (phone) screen', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('keeps the slideshow inline and folds the rest into an overflow menu', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // One compact row: the primary action plus the "…" toggle, instead of five
    // buttons wrapping into two or three rows beside the title.
    expect(screen.getByRole('link', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Download ZIP' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })

    // Every folded action is reachable from the menu, and the destructive one
    // sits behind a divider in danger styling rather than next to "Edit".
    const menu = overflowMenu()
    expect(within(menu).getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(within(menu).getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    const remove = within(menu).getByRole('button', { name: 'Delete' })
    expect(remove).toHaveClass('btn-outline-danger')
    const divider = menu.querySelector('.dropdown-divider')
    expect(divider).not.toBeNull()
    expect(divider?.compareDocumentPosition(remove)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('deletes the album from the overflow menu, still through the confirm dialog', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    deleteAlbumMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.click(within(overflowMenu()).getByRole('button', { name: 'Delete' }))

    // Collapsing the action changed nothing about it: the dialog still guards
    // the deletion, and the menu steps out of the way behind it.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Delete the album "Holidays"/)).toBeInTheDocument()
    expect(deleteAlbumMock).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(overflowMenu()).not.toHaveClass('show')
    })

    await user.click(within(dialog).getByRole('button', { name: 'Delete album' }))
    await waitFor(() => {
      expect(deleteAlbumMock).toHaveBeenCalledWith('al_1')
    })
  })

  it('edits the album from the overflow menu', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.click(within(overflowMenu()).getByRole('button', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Title')).toHaveValue('Holidays')
  })

  it('offers a viewer only the actions their role allows', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })

    // The menu is a different place to put the actions, not a way around RBAC:
    // a viewer's overflow holds the download alone.
    const menu = overflowMenu()
    expect(within(menu).getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(within(menu).queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(within(menu).queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(menu.querySelector('.dropdown-divider')).toBeNull()
  })
})
