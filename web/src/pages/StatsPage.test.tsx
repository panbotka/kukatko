import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
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

  it('groups the numbers in the active language', async () => {
    await i18n.changeLanguage('cs')
    renderPage()

    // Czech groups thousands with a (narrow no-break) space, not a comma.
    const headline = await screen.findByTestId('stat-headline-photos')
    expect(headline.textContent.replace(/\s/gu, ' ')).toBe('20 310')
  })
})
