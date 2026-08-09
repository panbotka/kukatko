import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type TileGridLayout } from '../components/TileGrid'
import i18n from '../i18n'
import { type SubjectCount, type SubjectType } from '../services/people'

import { PeoplePage } from './PeoplePage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout, so the real
// one measures zero and mounts nothing). It renders every subject through the
// real `List` component, which keeps the grid's own column template assertable.
interface MockGridProps {
  data: SubjectCount[]
  context: TileGridLayout
  components: { List: ComponentType<{ context: TileGridLayout; children: ReactNode }> }
  itemContent: (index: number, item: SubjectCount) => ReactNode
  computeItemKey: (index: number, item: SubjectCount) => string
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ components, context, data, itemContent, computeItemKey }: MockGridProps) => (
    <components.List context={context}>
      {data.map((item, index) => (
        <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
      ))}
    </components.List>
  ),
}))

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

const { fetchSubjects } = await import('../services/people')
const fetchMock = vi.mocked(fetchSubjects)

function subject(
  name: string,
  { type = 'person', photoCount = 1 }: { type?: SubjectType; photoCount?: number } = {},
): SubjectCount {
  return {
    uid: `su_${name.toLowerCase()}`,
    slug: name.toLowerCase(),
    name,
    type,
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photoCount * 2,
    photo_count: photoCount,
  }
}

/** A library like the real one: mostly people, a dog, and something else. */
function library(): SubjectCount[] {
  return [
    subject('Anna', { photoCount: 12 }),
    subject('Němcová', { photoCount: 40 }),
    subject('Bedřich', { photoCount: 3 }),
    subject('Rex', { type: 'pet', photoCount: 7 }),
    subject('Chalupa', { type: 'other', photoCount: 5 }),
  ]
}

/** Builds a minimal auth context value with the given write capability. */
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

/** Surfaces the current URL query so the view state can be asserted. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="search">{location.search}</span>
}

function renderPage(canWrite = true, entry = '/people') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <PeoplePage />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The names of the tiles the grid is currently showing, in the order it shows them. */
function shownNames(): string[] {
  return screen
    .getAllByRole('link')
    .map((link) => link.getAttribute('href') ?? '')
    .filter((href) => href.startsWith('/people/su_'))
    .map((href) => href.replace('/people/su_', ''))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(library())
})

describe('PeoplePage', () => {
  it('opens on everybody, alphabetically', async () => {
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })
    expect(shownNames()).toEqual(['anna', 'bedřich', 'chalupa', 'němcová', 'rex'])
  })

  it('narrows the grid by name and remembers the search in the URL', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    // Folded, exactly like the library's "Osoba" facet: no diacritics needed.
    await user.type(screen.getByRole('searchbox', { name: 'Search people' }), 'nemcova')

    await waitFor(() => {
      expect(shownNames()).toEqual(['němcová'])
    })
    expect(screen.getByTestId('search')).toHaveTextContent('q=nemcova')
  })

  it('sorts by photo count when asked', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort' }), 'count')

    await waitFor(() => {
      expect(shownNames()).toEqual(['němcová', 'anna', 'rex', 'chalupa', 'bedřich'])
    })
    expect(screen.getByTestId('search')).toHaveTextContent('sort=count')
  })

  it('splits the animals out of the people', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    await user.selectOptions(screen.getByRole('combobox', { name: 'Kind' }), 'pet')

    await waitFor(() => {
      expect(shownNames()).toEqual(['rex'])
    })
  })

  it('offers each kind with the count it holds under the current search', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    const kind = screen.getByRole('combobox', { name: 'Kind' })
    expect(kind).toHaveTextContent('Everyone (5)')
    expect(kind).toHaveTextContent('People (3)')

    await user.type(screen.getByRole('searchbox', { name: 'Search people' }), 'e')
    await waitFor(() => {
      expect(kind).toHaveTextContent('Everyone (3)')
    })
    expect(kind).toHaveTextContent('Animals (1)')
  })

  it('restores the whole view from the URL, so a link carries it', async () => {
    renderPage(true, '/people?q=e&sort=count&type=person')
    await screen.findByRole('link', { name: 'Němcová' })
    expect(shownNames()).toEqual(['němcová', 'bedřich'])
  })

  it('says nothing matched rather than showing an empty grid', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    await user.type(screen.getByRole('searchbox', { name: 'Search people' }), 'zzz')

    expect(await screen.findByText('Nobody matches here')).toBeInTheDocument()
    expect(screen.getByText('Try another name, or another kind.')).toBeInTheDocument()
  })

  it('blames the kind, not a search term that is not there', async () => {
    // Only the search can drop somebody library-wide; a kind that happens to be
    // empty must not send the reader hunting for a phantom query.
    fetchMock.mockResolvedValue([subject('Anna')])
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('link', { name: 'Anna' })

    await user.selectOptions(screen.getByRole('combobox', { name: 'Kind' }), 'pet')

    expect(await screen.findByText('Try another kind.')).toBeInTheDocument()
  })

  it('keeps the filter bar away from a library with nobody in it', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No people yet')).toBeInTheDocument()
    expect(screen.queryByRole('searchbox', { name: 'Search people' })).not.toBeInTheDocument()
  })

  it('offers the cluster queue to an editor', async () => {
    renderPage(true)
    expect(await screen.findByRole('link', { name: 'Review face groups' })).toBeInTheDocument()
  })

  it('keeps the cluster queue from a viewer, who cannot name anybody', async () => {
    renderPage(false)
    await screen.findByRole('link', { name: 'Anna' })
    expect(screen.queryByRole('link', { name: 'Review face groups' })).not.toBeInTheDocument()
  })

  it('offers a retry when the list cannot be loaded', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    renderPage()

    const retry = await screen.findByRole('button', { name: 'Try again' })
    fetchMock.mockResolvedValue(library())
    await user.click(retry)

    expect(await screen.findByRole('link', { name: 'Anna' })).toBeInTheDocument()
  })
})
