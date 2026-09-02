import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { albumOption, BATCH_ACTIONS } from '../test/batchBar'

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

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

// The box's own recent searches: stubbed so what the page decides to remember is
// observable, and so the dropdown never reaches for the network.
vi.mock('../services/searchHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/searchHistory')>()
  return {
    ...actual,
    fetchSearchHistory: vi.fn(),
    recordSearch: vi.fn(),
    clearSearchHistory: vi.fn(),
  }
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

const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

const { globalSearch } = await import('../services/search')
const globalSearchMock = vi.mocked(globalSearch)

const { fetchSearchHistory, recordSearch } = await import('../services/searchHistory')
const historyMock = vi.mocked(fetchSearchHistory)
const recordMock = vi.mocked(recordSearch)

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

/** Surfaces the current URL query for navigation assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="search">{location.search}</span>
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

/**
 * Renders the page. `semanticSearch` is the instance capability: it defaults to
 * available, because that is the state in which every mode behaves as picked;
 * the offline-sidecar behaviour has its own tests.
 */
function renderSearch(initialEntry = '/search', canWrite = true, semanticSearch = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider
        value={{ semantic_search: semanticSearch, known: true, passkeys: false }}
      >
        <AuthContext.Provider value={auth(canWrite)}>
          <MemoryRouter initialEntries={[initialEntry]}>
            <SearchPage />
            <LocationProbe />
          </MemoryRouter>
        </AuthContext.Provider>
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
}

/**
 * Reveals the mode switch, which lives behind the "Advanced" toggle for everyone
 * who has not asked for a mode of their own. A view already running a
 * non-default mode opens the panel by itself, so the toggle is only clicked when
 * the select is not on screen yet.
 */
async function openAdvanced(user: ReturnType<typeof userEvent.setup>) {
  if (screen.queryByLabelText('How to search') === null) {
    await user.click(screen.getByRole('button', { name: /advanced/i }))
  }
  return screen.getByLabelText('How to search')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
  // `restoreMocks: true` wipes the factory's resolved value after each test, so
  // re-establish it here; otherwise the cross-entity sections' debounced global
  // search resolves to `undefined` and leaks an unhandled rejection.
  globalSearchMock.mockReset()
  globalSearchMock.mockResolvedValue({ query: '', albums: [], labels: [], people: [], photos: [] })
  historyMock.mockReset()
  historyMock.mockResolvedValue([])
  recordMock.mockReset()
  recordMock.mockResolvedValue()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('SearchPage', () => {
  it('shows the idle prompt and runs no search when the query is empty', () => {
    renderSearch()
    expect(screen.getByText('Enter a search term.')).toBeInTheDocument()
    expect(searchMock).not.toHaveBeenCalled()
  })

  it('quotes the query in the browser tab, so a saved search is recognisable', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(document.title).toBe('Search “beach” · Kukátko')
  })

  it('falls back to the page name in the tab while nothing has been asked for', () => {
    renderSearch()

    expect(document.title).toBe('Search · Kukátko')
  })

  it('reproduces the query and mode from a shared URL and searches with them', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach&mode=semantic')

    await screen.findByRole('link', { name: 'a.jpg' })

    // The input reflects the URL, and a mode nobody would have picked by
    // accident unfolds its own switch rather than ranking differently in secret.
    expect(screen.getByLabelText('Search term')).toHaveValue('beach')
    expect(screen.getByLabelText('How to search')).toHaveValue('semantic')

    // The fetch used the URL query and mode (params, mode, signal).
    const [params, mode] = searchMock.mock.calls[0]
    expect(params.q).toBe('beach')
    expect(mode).toBe('semantic')
  })

  it('links each tile to the detail page carrying the search scope', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach&mode=semantic')

    // The tile's detail link carries the query and mode so Esc/Back returns to
    // the search (ranked results), not the library with `q` as a substring
    // filter, and prev/next pages the same ranked results.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?q=beach&mode=semantic')
  })

  it('changing the mode updates the URL and refetches', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(searchMock.mock.calls[0][1]).toBe('hybrid')

    await user.selectOptions(await openAdvanced(user), 'fulltext')

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
      await screen.findByText(/search by content is temporarily unavailable/i),
    ).toBeInTheDocument()
    // The results still render alongside the notice (non-blocking).
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })

  describe('with the embeddings sidecar offline', () => {
    it('searches as full-text right away instead of waiting for the sidecar timeout', async () => {
      searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
      renderSearch('/search?q=beach&mode=hybrid', true, false)

      await screen.findByRole('link', { name: 'a.jpg' })
      // The capability already says the sidecar is unreachable, so nothing that
      // could block on its timeout is sent — hybrid goes out as full-text.
      expect(searchMock.mock.calls.every(([, mode]) => mode === 'fulltext')).toBe(true)
    })

    it('says so beside the mode selector before any search runs', () => {
      renderSearch('/search', true, false)

      // No query has been typed yet: the notice comes from the capability flag,
      // not from a reply the page is still waiting for.
      expect(searchMock).not.toHaveBeenCalled()
      expect(screen.getByText(/search by content is temporarily unavailable/i)).toBeInTheDocument()
    })

    it('offers no notice while semantic search is available', () => {
      renderSearch('/search', true, true)

      expect(
        screen.queryByText(/search by content is temporarily unavailable/i),
      ).not.toBeInTheDocument()
    })

    it('disables the semantic option and explains why', async () => {
      const user = userEvent.setup()
      renderSearch('/search', true, false)

      const semantic = within(await openAdvanced(user)).getByRole('option', {
        name: 'By what is in the photo',
      })
      expect(semantic).toBeDisabled()
      expect(semantic).toHaveAttribute('title', expect.stringMatching(/unavailable/i))
    })

    it('keeps the picked mode in the URL so it applies again once the box is back', async () => {
      searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
      renderSearch('/search?q=beach&mode=semantic', true, false)

      await screen.findByRole('link', { name: 'a.jpg' })
      expect(screen.getByLabelText('How to search')).toHaveValue('semantic')
      expect(screen.getByTestId('search')).toHaveTextContent('mode=semantic')
    })
  })

  it('states no photo count until a query exists', () => {
    renderSearch('/search')

    // "Photos: 0" above "Enter a search term." reads as an empty library, when in
    // truth nothing has been searched for yet.
    expect(screen.getByText('Enter a search term.')).toBeInTheDocument()
    expect(screen.queryByText(/^photos:/i)).not.toBeInTheDocument()
  })

  it('states the photo count once a query has results', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach')

    expect(await screen.findByText('Photos: 1')).toBeInTheDocument()
  })

  it('hints at query-language tokens the server did not understand', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')], { unknown_tokens: ['color:red'] }))
    renderSearch('/search?q=color:red')

    expect(await screen.findByText(/i don't understand these filters/i)).toBeInTheDocument()
    expect(screen.getByText('color:red')).toBeInTheDocument()
    // The results still render alongside the hint (the token degraded to text).
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })

  it('repairs a mistyped filter key in the box and re-runs the search with it', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')], { unknown_tokens: ['osoba:Jarmila'] }))
    const user = userEvent.setup()
    renderSearch('/search?q=osoba:Jarmila')

    await user.click(await screen.findByRole('button', { name: 'Did you mean person:Jarmila?' }))

    // Both halves of the box move together: the URL the search runs on, and the
    // text the reader sees — otherwise the debounce commits the broken query back.
    expect(screen.getByTestId('search')).toHaveTextContent('q=person%3AJarmila')
    expect(screen.getByLabelText('Search term')).toHaveValue('person:Jarmila')
    await waitFor(() => {
      const calls = searchMock.mock.calls
      expect(calls[calls.length - 1][0].q).toBe('person:Jarmila')
    })
  })

  it('opens the query-language help listing filters and operators', async () => {
    const user = userEvent.setup()
    renderSearch()

    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    expect(await screen.findByText('Search query language')).toBeInTheDocument()
    // The example appears in the operators table and again in the filter list.
    expect(screen.getAllByText('label:cat|dog').length).toBeGreaterThan(0)
    expect(screen.getByText(/a space between filters means and/i)).toBeInTheDocument()
  })

  it('autocompletes filter keys in the query box', async () => {
    searchMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderSearch()

    const input = screen.getByLabelText('Search term')
    await user.type(input, 'ca')

    const option = await screen.findByRole('option', { name: 'camera:' })
    await user.click(option)

    expect(input).toHaveValue('camera:')
    // Accepting a key closes the dropdown until the user types again.
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('accepts a suggested key with the keyboard', async () => {
    searchMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderSearch()

    const input = screen.getByLabelText('Search term')
    await user.type(input, 'c')
    await screen.findByRole('listbox', { name: 'Filter suggestions' })

    // ArrowDown highlights camera:, a second one moves to city:, Enter accepts
    // the highlighted row (rather than submitting).
    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')

    expect(input).toHaveValue('city:')
  })

  it('searches for the typed phrase even when its tail prefixes a filter key', async () => {
    searchMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderSearch()

    const input = screen.getByLabelText('Search term')
    await user.type(input, 'svatba u')
    // `u` prefixes `uid`, so the key panel is up — untouched, so Enter is the
    // reader's own submit and neither the box nor the URL may be rewritten.
    await screen.findByRole('listbox', { name: 'Filter suggestions' })
    await user.keyboard('{Enter}')

    expect(input).toHaveValue('svatba u')
    await waitFor(() => {
      expect(screen.getByTestId('search')).toHaveTextContent('q=svatba+u')
    })
    expect(searchMock.mock.calls.map(([params]) => params.q)).not.toContain('svatba uid:')
    expect(searchMock.mock.calls.at(-1)?.[0].q).toBe('svatba u')
  })

  it('remembers nothing of a query that was only typed', async () => {
    vi.useFakeTimers()
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search')

    // Typed in bursts, with a pause long enough for each prefix to run as its own
    // search — which is exactly how `sva` used to end up in the history beside
    // `svatba`. Nothing here was submitted, so nothing is remembered.
    fireEvent.change(screen.getByLabelText('Search term'), { target: { value: 'sva' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000)
    })
    fireEvent.change(screen.getByLabelText('Search term'), { target: { value: 'svatba' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })

    expect(searchMock.mock.calls.map(([params]) => params.q)).toContain('sva')
    expect(recordMock).not.toHaveBeenCalled()
  })

  it('remembers a submitted query, exactly once', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search')

    const input = screen.getByLabelText('Search term')
    await user.type(input, 'svatba')
    expect(recordMock).not.toHaveBeenCalled()

    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(recordMock).toHaveBeenCalledTimes(1)
    })
    expect(recordMock.mock.calls[0][0]).toBe('svatba')

    // The search the debounce runs behind the submit is the same query; it must
    // not be counted a second time.
    await waitFor(() => {
      expect(screen.getByTestId('search')).toHaveTextContent('q=svatba')
    })
    expect(recordMock).toHaveBeenCalledTimes(1)
  })

  it('remembers a recent search picked from the box, moving it back to the front', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    historyMock.mockResolvedValue([
      { query: 'svatba 1974', searched_at: '2026-08-09T12:00:00Z' },
      { query: 'hory', searched_at: '2026-08-08T12:00:00Z' },
    ])
    const user = userEvent.setup()
    renderSearch('/search')

    // The page focuses the box on arrival, so the history is already offered.
    await user.click(await screen.findByRole('option', { name: 'svatba 1974' }))

    expect(screen.getByLabelText('Search term')).toHaveValue('svatba 1974')
    await waitFor(() => {
      expect(recordMock).toHaveBeenCalledTimes(1)
    })
    expect(recordMock.mock.calls[0][0]).toBe('svatba 1974')
  })

  it('offers a remembered search back for the prefix being typed', async () => {
    searchMock.mockResolvedValue(page([]))
    historyMock.mockResolvedValue([{ query: 'svatba 1974', searched_at: '2026-08-09T12:00:00Z' }])
    const user = userEvent.setup()
    renderSearch('/search')

    await user.type(screen.getByLabelText('Search term'), 's')

    // `s` also prefixes two filter keys; the reader's own search comes first.
    const options = await screen.findAllByRole('option')
    expect(options[0]).toHaveTextContent('svatba 1974')
  })

  it('shows the empty state when nothing matches', async () => {
    searchMock.mockResolvedValue(page([]))
    renderSearch('/search?q=nothing')

    expect(await screen.findByText('Nothing found')).toBeInTheDocument()
    // A dead end is never just a statement: the spelling is always worth a look.
    expect(screen.getByText(/check the spelling/i)).toBeInTheDocument()
  })

  it('clears the filters from the empty state, keeping the query that was asked', async () => {
    searchMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderSearch('/search?q=nothing&favorite=yes')

    // Scoped to the empty state: the filter bar carries a clear-all of its own,
    // saying the same thing in the same words, which is the point.
    const empty = within(await screen.findByTestId('empty-state'))
    await user.click(empty.getByRole('button', { name: 'Clear filters' }))

    const url = screen.getByTestId('search')
    expect(url).toHaveTextContent('q=nothing')
    expect(url).not.toHaveTextContent('favorite=yes')
  })

  it('offers describing the photo from the empty state and switches the search to it', async () => {
    searchMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderSearch('/search?q=nothing')

    await screen.findByText('Nothing found')
    await user.click(screen.getByRole('button', { name: 'Search by what is in the photo' }))

    expect(screen.getByTestId('search')).toHaveTextContent('mode=semantic')
  })

  it('offers no describing step while the box that reads photographs is down', async () => {
    searchMock.mockResolvedValue(page([]))
    renderSearch('/search?q=nothing', true, false)

    await screen.findByText('Nothing found')
    expect(
      screen.queryByRole('button', { name: 'Search by what is in the photo' }),
    ).not.toBeInTheDocument()
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

describe('SearchPage bulk edit', () => {
  it('keeps selection and bulk edit away from viewers', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderSearch('/search?q=beach', false)

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More edits' })).not.toBeInTheDocument()
  })

  it('offers a select checkmark on every result, with no selection mode to enter', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    // No "Select" step: the result is a link that already carries its checkmark,
    // exactly as on the library.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More edits' })).toBeEnabled()
  })

  it('raises the library’s full batch bar over the results, and only that one bar', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    const bars = screen.getAllByRole('toolbar', { name: 'Batch actions' })
    expect(bars).toHaveLength(1)
    const [bar] = bars
    for (const name of BATCH_ACTIONS) {
      expect(within(bar).getByRole('button', { name })).toBeInTheDocument()
    }

    // Select-all reaches the rest of the loaded results, as on the library.
    await user.click(within(bar).getByRole('button', { name: 'Select all' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('adds the picked results to an album straight from the bar, then re-runs the search', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    albumsMock.mockResolvedValue([albumOption('al_2', 'Trips')])
    labelsMock.mockResolvedValue([])
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    const searchesBefore = searchMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Add to album' }))
    await user.click(await screen.findByLabelText('Add to albums'))
    await user.click(await screen.findByRole('option', { name: /Trips/ }))
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a'], { add_to_albums: ['al_2'] })
    })
    await waitFor(() => {
      expect(searchMock.mock.calls.length).toBeGreaterThan(searchesBefore)
    })
  })

  it('bulk-edits exactly the picked photos, then re-runs the search', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select b.jpg' }))

    const searchesBefore = searchMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'More edits' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(searchMock.mock.calls.length).toBeGreaterThan(searchesBefore)
    })
  })

  it('drops the selection when the query changes, so no result of the old search stays picked', async () => {
    searchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderSearch('/search?q=beach')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    await user.selectOptions(await openAdvanced(user), 'fulltext')

    await waitFor(() => {
      expect(screen.queryByText('1 selected')).not.toBeInTheDocument()
    })
    // The search's own actions are handed back the header.
    expect(await screen.findByRole('button', { name: 'Save view' })).toBeInTheDocument()
  })
})
