import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'
import { SLIDESHOW_DEFAULTS, writeSettings } from '../lib/slideshowSettings'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { SlideshowPage } from './SlideshowPage'

// Keep the real helpers; only the network calls are faked.
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn(), searchPhotos: vi.fn() }
})

const { fetchPhotos, searchPhotos } = await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)
const searchMock = vi.mocked(searchPhotos)

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

function page(photos: Photo[], extra: Partial<PhotoListResponse> = {}): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null, ...extra }
}

/**
 * Renders the slideshow. `semanticSearch` is the instance capability; it
 * defaults to available so a replayed search runs in the mode the URL asks for.
 */
function renderPage(initialEntry = '/slideshow', semanticSearch = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider value={{ semantic_search: semanticSearch }}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <SlideshowPage />
        </MemoryRouter>
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
  searchMock.mockReset()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('SlideshowPage', () => {
  it('scopes the fetch to the album / label and filters from the URL', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/slideshow?album=al1&sort=oldest')

    await screen.findByRole('img')
    const params = fetchMock.mock.calls[0][0]
    expect(params.album).toBe('al1')
    expect(params.sort).toBe('oldest')
  })

  it('renders the slideshow stage when photos load', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage('/slideshow?label=lb1')

    await screen.findByRole('img')
    expect(screen.getByText('slide 1 of 2')).toBeInTheDocument()
  })

  it('counts the remaining time against the server total, not the loaded page', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], { total: 40 }))
    const user = userEvent.setup()
    renderPage('/slideshow')

    await screen.findByRole('img')
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    // 40 photos at the default 5 s: 39 still to come → 3 min 15 s, shown beside
    // the speed control rather than in the caption.
    expect(screen.getByText('3 min 15 s left')).toBeInTheDocument()
  })

  it('replays the search — not a library listing — when the URL carries a mode', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/slideshow?q=beach&mode=semantic')

    await screen.findByRole('img')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(searchMock.mock.calls[0][0].q).toBe('beach')
    expect(searchMock.mock.calls[0][1]).toBe('semantic')
  })

  it('replays a semantic search as full-text while the embeddings box is offline', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/slideshow?q=beach&mode=semantic', false)

    await screen.findByRole('img')
    // Still the search endpoint (the library would play a different set), but in
    // the mode that can actually be answered — a slideshow that starts half a
    // minute late reads as broken.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(searchMock.mock.calls[0][1]).toBe('fulltext')
  })

  it('shows a graceful empty state for an empty set', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage('/slideshow?album=al1')

    expect(await screen.findByText('No photos to play')).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it('shows an error state with retry when loading fails', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    renderPage('/slideshow?album=al1')

    expect(await screen.findByText('Could not load photos.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('says what it is waiting for, and leaves a way out, while photos load', async () => {
    // The route is mounted outside the app shell: with no navbar and no Back
    // link, a slow first page used to be a black screen with a spinner, no
    // words and nothing to press.
    let resolve = (): void => undefined
    fetchMock.mockReturnValue(
      new Promise<PhotoListResponse>((r) => {
        resolve = () => {
          r(page([photo('a', 'a.jpg')]))
        }
      }),
    )
    renderPage('/slideshow?album=al1')

    expect(screen.getByText('Loading photos…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()

    resolve()
    await screen.findByRole('img')
  })

  it('offers the same way out of the empty and the failed show', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage('/slideshow?album=al1')

    await screen.findByText('No photos to play')
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument()
  })

  it('persists the chosen effect to localStorage from the settings panel', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage('/slideshow?album=al1')

    await screen.findByRole('img')
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.selectOptions(screen.getByLabelText('Transition'), 'slide')

    await waitFor(() => {
      const stored = JSON.parse(
        window.localStorage.getItem('kukatko.slideshow.settings') ?? '{}',
      ) as { effect?: string }
      expect(stored.effect).toBe('slide')
    })
  })

  it('asks the server for a random order, under one seed, when shuffle is on', async () => {
    writeSettings({ ...SLIDESHOW_DEFAULTS, shuffle: true })
    fetchMock.mockResolvedValue(
      page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], { total: 40, next_offset: 2 }),
    )
    renderPage('/slideshow?album=al1')

    await screen.findByRole('img')
    // Shuffling only what has loaded would over-represent the first page; the
    // whole set is only reachable through the server's own random order.
    await waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThan(1)
    })
    const seeds = new Set(fetchMock.mock.calls.map(([params]) => params.seed))
    expect(fetchMock.mock.calls[0][0].sort).toBe('random')
    expect(fetchMock.mock.calls[0][0].seed).toBeTruthy()
    // One seed for the life of the show: pages of two different permutations
    // would overlap and drop whatever fell between them.
    expect(seeds.size).toBe(1)
  })

  it('plays the view order, with no seed, when shuffle is off', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/slideshow?sort=oldest')

    await screen.findByRole('img')
    expect(fetchMock.mock.calls[0][0].sort).toBe('oldest')
    expect(fetchMock.mock.calls[0][0].seed).toBeUndefined()
  })

  it('keeps the photo on screen when shuffle is turned on mid-show', async () => {
    const ordered = [photo('a', 'a.jpg'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]
    // The reshuffled list comes back in another order, leading with a photo the
    // reader has already seen.
    const shuffledOrder = [photo('c', 'c.jpg'), photo('a', 'a.jpg'), photo('b', 'b.jpg')]
    fetchMock.mockImplementation((params) =>
      Promise.resolve(page(params.sort === 'random' ? shuffledOrder : ordered)),
    )
    const user = userEvent.setup()
    renderPage('/slideshow')

    await screen.findByRole('img')
    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(screen.getByRole('img')).toHaveAttribute('src', expect.stringContaining('/photos/b/'))

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('checkbox', { name: 'Shuffle' }))

    // Resumed, not restarted: the same photo, at the same position in the show.
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([params]) => params.sort === 'random')).toBe(true)
    })
    expect(screen.getByRole('img')).toHaveAttribute('src', expect.stringContaining('/photos/b/'))
    expect(screen.getByText('slide 2 of 3')).toBeInTheDocument()

    // And what is still to come is the new order minus what has been seen: c,
    // never a and b again before it.
    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(screen.getByRole('img')).toHaveAttribute('src', expect.stringContaining('/photos/c/'))
  })
})
