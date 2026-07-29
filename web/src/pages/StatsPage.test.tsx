import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import type { LibraryStats } from '../services/system'

import { StatsPage } from './StatsPage'

// Mock the system service so the page's single data source is controlled.
vi.mock('../services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/system')>()
  return { ...actual, fetchLibraryStats: vi.fn() }
})

const { fetchLibraryStats } = await import('../services/system')
const fetchMock = vi.mocked(fetchLibraryStats)

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
    embeddings: 20092,
    faces: 112806,
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

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <StatsPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(stats())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('StatsPage', () => {
  it('renders the loaded counts grouped, with thousands separators', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Library statistics' }),
    ).toBeInTheDocument()

    // Headline numbers: grouped for the active locale, never raw JSON.
    expect(await screen.findByTestId('stat-headline-photos')).toHaveTextContent('20,310')
    expect(screen.getByTestId('stat-headline-embeddings')).toHaveTextContent('20,092')
    expect(screen.getByTestId('stat-headline-faces')).toHaveTextContent('112,806')
    expect(screen.getByTestId('stat-headline-people')).toHaveTextContent('42')
    expect(screen.getByTestId('stat-headline-collections')).toHaveTextContent('12')

    // Every group renders under its own heading.
    for (const name of [
      'Photos',
      'Embeddings',
      'Faces',
      'People and animals',
      'Albums and labels',
    ]) {
      expect(screen.getByRole('heading', { name })).toBeInTheDocument()
    }
  })

  it('reports the derived coverage gaps, which is what the page is opened for', async () => {
    renderPage()

    expect(await screen.findByTestId('stat-embeddings-without-embedding')).toHaveTextContent('218')
    expect(screen.getByTestId('stat-faces-without-faces')).toHaveTextContent('5,743')
    expect(screen.getByTestId('stat-people-unnamed')).toHaveTextContent('150')
    // The photo breakdown carries the trash and the video count too.
    expect(screen.getByTestId('stat-photos-archived')).toHaveTextContent('9')
    expect(screen.getByTestId('stat-photos-videos')).toHaveTextContent('118')
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

  it('groups the numbers in the active language', async () => {
    await i18n.changeLanguage('cs')
    renderPage()

    // Czech groups thousands with a (narrow no-break) space, not a comma.
    const headline = await screen.findByTestId('stat-headline-photos')
    expect(headline.textContent.replace(/\s/gu, ' ')).toBe('20 310')
  })
})
