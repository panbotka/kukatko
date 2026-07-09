import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type GlobalSearchResult } from '../services/search'

import { NavbarSearch } from './NavbarSearch'

// Only the network call is faked; the hook's debounce and abort logic run for real.
vi.mock('../services/search', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/search')>()
  return { ...actual, globalSearch: vi.fn() }
})

const { globalSearch } = await import('../services/search')
const searchMock = vi.mocked(globalSearch)

const RESULT: GlobalSearchResult = {
  query: 'beach',
  albums: [{ uid: 'al1', title: 'Beach trip', cover: 'ph9', photo_count: 12 }],
  labels: [{ uid: 'lb1', name: 'beachy', photo_count: 40 }],
  people: [{ uid: 'su1', name: 'Beatrice', cover: 'ph3' }],
  photos: [
    {
      uid: 'ph1',
      file_hash: 'ph1',
      file_name: 'wave.jpg',
      file_size: 1,
      file_mime: 'image/jpeg',
      file_width: 1,
      file_height: 1,
      taken_at_source: 'exif',
      thumb_url: '/api/v1/photos/ph1/thumb/tile_500',
      download_url: '/api/v1/photos/ph1/download?original=true',
      title: '',
      description: '',
      camera_make: '',
      camera_model: '',
      lens_model: '',
      private: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ],
}

/** Surfaces the current URL (path + query) for navigation assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="loc">{`${location.pathname}${location.search}`}</span>
}

function renderNavbar() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/library']}>
        <NavbarSearch />
        <LocationProbe />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('NavbarSearch', () => {
  it('shows grouped quick results as the user types', async () => {
    searchMock.mockResolvedValue(RESULT)
    const user = userEvent.setup()
    renderNavbar()

    await user.type(screen.getByLabelText('Search term'), 'beach')

    // Group headers appear once matches arrive (after the internal debounce).
    expect(await screen.findByText('Albums')).toBeInTheDocument()
    expect(screen.getByText('Labels')).toBeInTheDocument()
    expect(screen.getByText('People')).toBeInTheDocument()
    expect(screen.getByText('Photos')).toBeInTheDocument()

    // And the individual entity rows.
    expect(screen.getByText('Beach trip')).toBeInTheDocument()
    expect(screen.getByText('beachy')).toBeInTheDocument()
    expect(screen.getByText('Beatrice')).toBeInTheDocument()
    expect(screen.getByText('wave.jpg')).toBeInTheDocument()
    expect(searchMock).toHaveBeenCalledWith('beach', expect.anything())
  })

  it('navigates to the album page when an album result is clicked', async () => {
    searchMock.mockResolvedValue(RESULT)
    const user = userEvent.setup()
    renderNavbar()

    await user.type(screen.getByLabelText('Search term'), 'beach')
    const albumRow = await screen.findByRole('option', { name: /Beach trip/ })

    await user.click(albumRow)

    expect(screen.getByTestId('loc')).toHaveTextContent('/albums/al1')
  })

  it('pressing Enter with no highlighted item goes to the full search page', async () => {
    searchMock.mockResolvedValue(RESULT)
    const user = userEvent.setup()
    renderNavbar()

    const input = screen.getByLabelText('Search term')
    await user.type(input, 'beach')
    await screen.findByText('Albums')

    await user.type(input, '{Enter}')

    await waitFor(() => {
      expect(screen.getByTestId('loc')).toHaveTextContent('/search?q=beach')
    })
  })

  it('shows a "nothing found" line when there are no matches', async () => {
    searchMock.mockResolvedValue({ query: 'zzz', albums: [], labels: [], people: [], photos: [] })
    const user = userEvent.setup()
    renderNavbar()

    await user.type(screen.getByLabelText('Search term'), 'zzz')

    expect(await screen.findByText('Nothing found')).toBeInTheDocument()
  })
})
