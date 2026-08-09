import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { LibraryCharts, LibraryStats } from '../services/system'

import { StatsPage } from './StatsPage'

// Mock the system service so both of the page's data sources are controlled.
vi.mock('../services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/system')>()
  return { ...actual, fetchLibraryStats: vi.fn(), fetchLibraryCharts: vi.fn() }
})

const { fetchLibraryStats, fetchLibraryCharts } = await import('../services/system')
const fetchMock = vi.mocked(fetchLibraryStats)
const chartsMock = vi.mocked(fetchLibraryCharts)

/** A full counts fixture, in the shape the backend derives them. */
function stats(overrides: Partial<LibraryStats> = {}): LibraryStats {
  return {
    photos: 20310,
    videos: 118,
    photos_live: 20301,
    photos_archived: 9,
    photos_with_embedding: 20092,
    photos_with_faces: 14567,
    photos_without_embedding: 218,
    photos_without_faces: 5743,
    photos_with_gps: 12186,
    embeddings: 20092,
    faces: 112806,
    faces_assigned: 84604,
    subjects: 42,
    subjects_person: 38,
    subjects_pet: 3,
    subjects_other: 1,
    markers: 900,
    markers_assigned: 750,
    markers_unassigned: 150,
    albums: 12,
    labels: 27,
    ...overrides,
  }
}

/**
 * Chart series in the shape the backend fills them: gap-filled years, a full
 * twelve-month window, every media bucket, and the running storage total.
 */
function charts(overrides: Partial<LibraryCharts> = {}): LibraryCharts {
  return {
    photos_by_year: [
      { year: 1905, photos: 12 },
      { year: 1906, photos: 0 },
      { year: 1907, photos: 40 },
    ],
    added_by_month: [
      { month: '2025-09', photos: 100 },
      { month: '2025-10', photos: 0 },
      { month: '2025-11', photos: 250 },
    ],
    top_cameras: [
      { camera: 'Canon EOS 5D', model: 'Canon EOS 5D', photos: 800 },
      { camera: 'Apple iPhone 13', model: 'iPhone 13', photos: 400 },
    ],
    storage_by_media: [
      { media: 'image', photos: 20, bytes: 4096 },
      { media: 'live', photos: 0, bytes: 0 },
      { media: 'video', photos: 2, bytes: 2048 },
      { media: 'raw', photos: 1, bytes: 1024 },
    ],
    storage_by_year: [
      { year: 2025, photos: 10, bytes: 4096, cumulative_bytes: 4096 },
      { year: 2026, photos: 13, bytes: 3072, cumulative_bytes: 7168 },
    ],
    ...overrides,
  }
}

// auth builds an AuthContext value for the given capability. The page itself is
// open to every role; only the action links behind the highlighted numbers are
// gated, so `canWrite` is the whole difference between a viewer and an editor.
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    isMaintainer: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter>
          <StatsPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(stats())
  chartsMock.mockReset()
  chartsMock.mockResolvedValue(charts())
})

describe('StatsPage', () => {
  it('renders the loaded counts grouped, with thousands separators', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Library statistics' }),
    ).toBeInTheDocument()

    // Headline numbers: grouped for the active locale, never raw JSON.
    expect(await screen.findByTestId('stat-headline-photos')).toHaveTextContent('20,310')
    expect(screen.getByTestId('stat-headline-content')).toHaveTextContent('20,092')
    expect(screen.getByTestId('stat-headline-faces')).toHaveTextContent('112,806')
    expect(screen.getByTestId('stat-headline-people')).toHaveTextContent('42')
    expect(screen.getByTestId('stat-headline-collections')).toHaveTextContent('12')

    // Every group renders under its own heading.
    for (const name of [
      'Photos',
      'Search by content',
      'Faces',
      'People and animals',
      'Albums and labels',
    ]) {
      expect(screen.getByRole('heading', { name })).toBeInTheDocument()
    }
  })

  it('says what an embedding buys the reader instead of naming it', async () => {
    renderPage()

    // The page is open to the whole family, so the pipeline's vocabulary stays
    // out of it: no card, row or headline mentions an embedding.
    expect(await screen.findByTestId('library-stats')).not.toHaveTextContent(/embedding/i)
    expect(screen.getByText('Photos ready to search by content')).toBeInTheDocument()
    expect(screen.getByText('Still to process')).toBeInTheDocument()
  })

  it('reports the derived coverage gaps, which is what the page is opened for', async () => {
    renderPage()

    expect(await screen.findByTestId('stat-content-pending')).toHaveTextContent('218')
    expect(screen.getByTestId('stat-faces-without-faces')).toHaveTextContent('5,743')
    expect(screen.getByTestId('stat-people-unnamed')).toHaveTextContent('150')
    // The photo breakdown carries the trash and the video count too.
    expect(screen.getByTestId('stat-photos-archived')).toHaveTextContent('9')
    expect(screen.getByTestId('stat-photos-videos')).toHaveTextContent('118')
  })

  it('turns the numbers that mean work into links to where that work happens', async () => {
    renderPage()

    const unnamed = within(await screen.findByTestId('stat-people-unnamed')).getByRole('link', {
      name: 'Name faces in the review game',
    })
    expect(unnamed).toHaveTextContent('150')
    expect(unnamed).toHaveAttribute('href', '/review')

    // The library's own "no faces" filter, so the destination is an ordinary
    // library view rather than a screen of its own.
    expect(
      within(screen.getByTestId('stat-faces-without-faces')).getByRole('link', {
        name: 'Show photos without a face in the library',
      }),
    ).toHaveAttribute('href', '/?q=faces%3A0')

    expect(
      within(screen.getByTestId('stat-photos-archived')).getByRole('link', {
        name: 'Open the trash',
      }),
    ).toHaveAttribute('href', '/trash')
  })

  it('never offers a viewer a link they would be turned away from', async () => {
    renderPage(false)

    // Both destinations write (naming a face, emptying the trash), so a viewer
    // reads the plain number instead of being sent to a forbidden page.
    expect(await screen.findByTestId('stat-people-unnamed')).toHaveTextContent('150')
    expect(within(screen.getByTestId('stat-people-unnamed')).queryByRole('link')).toBeNull()
    expect(within(screen.getByTestId('stat-photos-archived')).queryByRole('link')).toBeNull()
    // The library is open to every role, so that link survives.
    expect(
      within(screen.getByTestId('stat-faces-without-faces')).getByRole('link'),
    ).toHaveAttribute('href', '/?q=faces%3A0')
  })

  it('shows an error state instead of zeroes when the counts cannot be loaded', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Failed to load the library statistics.')).toBeInTheDocument()
    // Crucially, no grid of zeroes is rendered alongside the error.
    expect(screen.queryByTestId('library-stats')).not.toBeInTheDocument()
  })

  it('retries the fetch from the error state', async () => {
    const user = userEvent.setup()
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    const error = await screen.findByTestId('error-state')
    await user.click(within(error).getByRole('button', { name: /try again/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(await screen.findByTestId('stat-headline-photos')).toHaveTextContent('20,310')
  })

  it('draws the four charts with their key numbers in the accessible name', async () => {
    renderPage()

    expect(
      await screen.findByRole('group', {
        name: 'Photos by the year they were taken, 1905 to 1907. Photos in total: 52. The busiest year is 1907: 40.',
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', {
        name: 'Photos added to the library over the last 3 months. Photos in total: 350. The busiest month is Nov 2025: 250.',
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('group', {
        name: 'The most used cameras, 2 of them ranked. The most photos come from Canon EOS 5D: 800.',
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Library size by media type, 7.0 KB in total.' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', {
        name: 'Library growth by the year photos were added, 2025 to 2026. At the end of 2026 it held 7.0 KB.',
      }),
    ).toBeInTheDocument()
  })

  it('opens a year of the histogram in the library, through the period filter', async () => {
    renderPage()

    const bar = await screen.findByRole('link', { name: 'Show photos taken in 1907' })
    expect(bar).toHaveAttribute('href', '/?taken_after=1907-01-01&taken_before=1907-12-31')
    // An empty year leads nowhere: there would be nothing behind the link.
    expect(screen.queryByRole('link', { name: 'Show photos taken in 1906' })).toBeNull()
  })

  it('opens a camera in the library through its own filter', async () => {
    renderPage()

    expect(
      await screen.findByRole('link', { name: 'Show photos taken with Apple iPhone 13' }),
    ).toHaveAttribute('href', '/?camera=iPhone+13')
  })

  it('reports the coverage shares beside the counts', async () => {
    renderPage()

    // 12 186 of 20 310 photos know where they were taken.
    expect(await screen.findByTestId('coverage-gps')).toHaveTextContent('60%')
    expect(screen.getByTestId('coverage-content')).toHaveTextContent('98.9%')
    expect(screen.getByTestId('coverage-faces')).toHaveTextContent('75%')
  })

  it('keeps the counts readable when only the charts fail', async () => {
    chartsMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Failed to load the library charts.')).toBeInTheDocument()
    // The counts came from the other endpoint and are unaffected.
    expect(screen.getByTestId('stat-headline-photos')).toHaveTextContent('20,310')
    expect(screen.queryByTestId('library-charts')).not.toBeInTheDocument()
  })

  it('retries only the charts from their own error state', async () => {
    const user = userEvent.setup()
    chartsMock.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    const error = await screen.findByTestId('error-state')
    await user.click(within(error).getByRole('button', { name: /try again/i }))

    await waitFor(() => {
      expect(chartsMock).toHaveBeenCalledTimes(2)
    })
    expect(await screen.findByTestId('library-charts')).toBeInTheDocument()
    // The counts were never refetched: the two endpoints fail and retry apart.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('says so instead of drawing an empty frame when a series has nothing in it', async () => {
    chartsMock.mockResolvedValue(charts({ photos_by_year: [], top_cameras: [] }))
    renderPage()

    expect(await screen.findByText('No photo has a known capture date yet.')).toBeInTheDocument()
    expect(screen.getByText('No photo names a camera yet.')).toBeInTheDocument()
  })

  it('groups the numbers in the active language', async () => {
    await i18n.changeLanguage('cs')
    renderPage()

    // Czech groups thousands with a (narrow no-break) space, not a comma.
    const headline = await screen.findByTestId('stat-headline-photos')
    expect(headline.textContent.replace(/\s/gu, ' ')).toBe('20 310')
  })
})
