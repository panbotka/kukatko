import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { SearchPage } from './SearchPage'

// Stand-in for react-virtuoso's grid (jsdom has no layout): render every item.
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
  endReached?: () => void
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

// Keep the real helpers; only the network call is faked.
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, searchPhotos: vi.fn() }
})

// The cross-entity sections run their own global search; stub it to an empty
// result so this suite stays focused on the photo grid (see GlobalSearchSections
// tests for the sections themselves).
vi.mock('../services/search', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/search')>()
  return {
    ...actual,
    globalSearch: vi
      .fn()
      .mockResolvedValue({ query: '', albums: [], labels: [], people: [], photos: [] }),
  }
})

const { searchPhotos } = await import('../services/photos')
const searchMock = vi.mocked(searchPhotos)

const { globalSearch } = await import('../services/search')
const globalSearchMock = vi.mocked(globalSearch)

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
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[], extra: Partial<PhotoListResponse> = {}): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null, ...extra }
}

/** Surfaces the current URL query for navigation assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="search">{location.search}</span>
}

function renderSearch(initialEntry = '/search') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <SearchPage />
        <LocationProbe />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  // `restoreMocks: true` wipes the factory's resolved value after each test, so
  // re-establish it here; otherwise the cross-entity sections' debounced global
  // search resolves to `undefined` and leaks an unhandled rejection.
  globalSearchMock.mockReset()
  globalSearchMock.mockResolvedValue({ query: '', albums: [], labels: [], people: [], photos: [] })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('SearchPage', () => {
  it('shows the idle prompt and runs no search when the query is empty', () => {
    renderSearch()
    expect(screen.getByText('Enter a search term.')).toBeInTheDocument()
    expect(searchMock).not.toHaveBeenCalled()
  })

  it('reproduces the query and mode from a shared URL and searches with them', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach&mode=semantic')

    await screen.findByRole('link', { name: 'a.jpg' })

    // The input and mode selector reflect the URL.
    expect(screen.getByLabelText('Search term')).toHaveValue('beach')
    expect(screen.getByLabelText('Mode')).toHaveValue('semantic')

    // The fetch used the URL query and mode (params, mode, signal).
    const [params, mode] = searchMock.mock.calls[0]
    expect(params.q).toBe('beach')
    expect(mode).toBe('semantic')
  })

  it('changing the mode updates the URL and refetches', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(searchMock.mock.calls[0][1]).toBe('hybrid')

    await user.selectOptions(screen.getByLabelText('Mode'), 'fulltext')

    await waitFor(() => {
      const calls = searchMock.mock.calls
      expect(calls[calls.length - 1][1]).toBe('fulltext')
    })
    expect(screen.getByTestId('search')).toHaveTextContent('mode=fulltext')
  })

  it('debounces typed input before committing to the URL and searching', async () => {
    vi.useFakeTimers()
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search')

    fireEvent.change(screen.getByLabelText('Search term'), { target: { value: 'cat' } })

    // No request yet — the debounce has not elapsed.
    expect(searchMock).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400)
    })

    expect(searchMock).toHaveBeenCalledTimes(1)
    expect(searchMock.mock.calls[0][0].q).toBe('cat')
    expect(screen.getByTestId('search')).toHaveTextContent('q=cat')
  })

  it('shows a non-blocking notice when search degrades to full-text', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')], { mode: 'fulltext', degraded: true }))
    renderSearch('/search?q=beach&mode=semantic')

    expect(
      await screen.findByText(/semantic search is temporarily unavailable/i),
    ).toBeInTheDocument()
    // The results still render alongside the notice (non-blocking).
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })

  it('shows the empty state when nothing matches', async () => {
    searchMock.mockResolvedValue(page([]))
    renderSearch('/search?q=nothing')

    expect(await screen.findByText('Nothing found')).toBeInTheDocument()
  })

  it('shows an error with a retry that re-runs the search', async () => {
    searchMock.mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    expect(await screen.findByText('Search failed.')).toBeInTheDocument()

    searchMock.mockResolvedValueOnce(page([photo('a', 'a.jpg')]))
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })
})
