import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type Photo } from '../../services/photos'
import { type GlobalSearchResult } from '../../services/search'

import { SearchCommand } from './SearchCommand'

vi.mock('../../services/search', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/search')>()
  return { ...actual, globalSearch: vi.fn() }
})

const { globalSearch } = await import('../../services/search')
const searchMock = vi.mocked(globalSearch)

/** Builds a minimal Photo for the palette's Photos group. */
function photo(overrides: Partial<Photo> = {}): Photo {
  return {
    uid: 'ph1',
    file_hash: 'h',
    file_name: 'sunset.jpg',
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 1920,
    file_height: 1080,
    taken_at_source: 'exif',
    taken_at: '2024-06-01T10:00:00Z',
    thumb_url: '/api/v1/photos/ph1/thumb/tile_100',
    download_url: '/api/v1/photos/ph1/download?original=true',
    title: 'Sunset beach',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

const RESULT: GlobalSearchResult = {
  query: 'beach',
  albums: [{ uid: 'al1', title: 'Beach trip', cover: 'ph9', photo_count: 12 }],
  labels: [{ uid: 'lb1', name: 'beachy', photo_count: 40 }],
  people: [{ uid: 'su1', name: 'Beatrice', cover: 'ph3' }],
  photos: [photo()],
}

/** Renders the current location so navigation from the palette is observable. */
function LocationDisplay() {
  const location = useLocation()
  return <div data-testid="loc">{`${location.pathname}${location.search}`}</div>
}

function renderCommand() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/']}>
        <SearchCommand />
        <input aria-label="unrelated field" />
        <LocationDisplay />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  searchMock.mockResolvedValue(RESULT)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SearchCommand', () => {
  it('renders a field-shaped trigger showing the keyboard hint', () => {
    renderCommand()
    const trigger = screen.getByRole('button', { name: 'Search' })
    expect(trigger).toBeInTheDocument()
    // The shortcut is advertised in the field itself.
    expect(trigger).toHaveTextContent('/')
    // The palette is closed until asked for.
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })

  it('opens the palette with the / shortcut and closes it with Escape', async () => {
    renderCommand()
    fireEvent.keyDown(document.body, { key: '/' })
    const input = await screen.findByRole('combobox')
    expect(input).toBeInTheDocument()

    fireEvent.keyDown(input, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })
  })

  it('toggles the palette with Cmd/Ctrl-K, even from a focused field', async () => {
    renderCommand()
    const other = screen.getByRole('textbox', { name: 'unrelated field' })
    other.focus()

    fireEvent.keyDown(other, { key: 'k', ctrlKey: true })
    expect(await screen.findByRole('combobox')).toBeInTheDocument()

    fireEvent.keyDown(document.body, { key: 'k', metaKey: true })
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })
  })

  it('does not open on / while the user is typing in another field', () => {
    renderCommand()
    const other = screen.getByRole('textbox', { name: 'unrelated field' })
    other.focus()
    fireEvent.keyDown(other, { key: '/' })
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })

  it('shows grouped results for a query', async () => {
    const user = userEvent.setup()
    renderCommand()
    await user.click(screen.getByRole('button', { name: 'Search' }))
    await user.type(await screen.findByRole('combobox'), 'beach')

    // Every group the endpoint returns is rendered under its heading…
    expect(await screen.findByRole('option', { name: /Sunset beach/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Beatrice/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Beach trip/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /beachy/ })).toBeInTheDocument()
    for (const heading of ['Photos', 'People', 'Albums', 'Labels']) {
      expect(screen.getByText(heading)).toBeInTheDocument()
    }
  })

  it('opens the highlighted entity on Enter after arrowing down', async () => {
    const user = userEvent.setup()
    renderCommand()
    await user.click(screen.getByRole('button', { name: 'Search' }))
    const input = await screen.findByRole('combobox')
    await user.type(input, 'beach')
    await screen.findByRole('option', { name: /Sunset beach/ })

    // The cursor starts on the "search everything" row; one step down lands on the
    // first result (the photo), and Enter opens it.
    await user.keyboard('{ArrowDown}{Enter}')
    expect(screen.getByTestId('loc')).toHaveTextContent('/photos/ph1')
  })

  it('runs a full search when Enter is pressed without moving the cursor', async () => {
    const user = userEvent.setup()
    renderCommand()
    await user.click(screen.getByRole('button', { name: 'Search' }))
    const input = await screen.findByRole('combobox')
    await user.type(input, 'beach')
    await screen.findByRole('option', { name: /Sunset beach/ })

    await user.keyboard('{Enter}')
    expect(screen.getByTestId('loc')).toHaveTextContent('/search?q=beach')
  })

  it('navigates to a result when it is clicked', async () => {
    const user = userEvent.setup()
    renderCommand()
    await user.click(screen.getByRole('button', { name: 'Search' }))
    await user.type(await screen.findByRole('combobox'), 'beach')

    await user.click(await screen.findByRole('option', { name: /Beach trip/ }))
    expect(screen.getByTestId('loc')).toHaveTextContent('/albums/al1')
  })
})
