import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'
import { type AlbumCount, type LabelCount } from '../../services/organize'

import { FilterBar } from './FilterBar'

function renderBar(
  view: LibraryView,
  onChange: SetUrlState<LibraryView>,
  props: {
    showSearch?: boolean
    showSort?: boolean
    showDensity?: boolean
    facets?: LibraryFacets
    searchHref?: string
  } = {},
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <FilterBar view={view} onChange={onChange} total={0} {...props} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** An album the facet select offers, trimmed to the fields the bar reads. */
function album(uid: string, title: string, photoCount: number): AlbumCount {
  return {
    uid,
    slug: uid,
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** A label the facet select offers, trimmed to the fields the bar reads. */
function label(uid: string, name: string, photoCount: number): LabelCount {
  return {
    uid,
    slug: uid,
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** The three facet option lists, as `useLibraryFacets` would deliver them. */
const FACETS: LibraryFacets = {
  years: [
    { year: 2023, count: 12 },
    { year: 2021, count: 3 },
  ],
  albums: [album('al_1', 'Holidays', 12), album('al_2', 'Náměstí', 4)],
  labels: [label('lb_1', 'Beach', 7), label('lb_2', 'Portrait', 2)],
}

/** Opens the advanced-filter panel so its controls become reachable. */
async function openPanel(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Filters/ }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  // The density picker reads localStorage; keep every test on a clean slate.
  window.localStorage.removeItem('kukatko.grid.density')
})

describe('FilterBar header', () => {
  it('keeps the sort selector in the header without expanding', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.selectOptions(screen.getByLabelText('Sort'), 'rating')
    expect(onChange).toHaveBeenCalledWith({ sort: 'rating' })
  })

  it('hides the search and sort controls when asked', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { showSearch: false, showSort: false })
    expect(screen.queryByLabelText('Filter the library')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Sort')).not.toBeInTheDocument()
    // The filters toggle is still available.
    expect(screen.getByRole('button', { name: /Filters/ })).toBeInTheDocument()
  })

  it('shows the grid-density stepper resting on the responsive default', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    // Auto is the resting state: there is nothing to step below it and nothing to
    // reset, so only "more" is available.
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Automatic' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeEnabled()
  })

  it('persists the density per device instead of writing it to the URL view', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    expect(onChange).not.toHaveBeenCalled()
    // Auto steps into the pinned range at the minimum column count.
    expect(window.localStorage.getItem('kukatko.grid.density')).toBe('2')
  })

  it('hides the density picker when asked', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { showDensity: false })
    expect(screen.queryByLabelText('Tiles per row')).not.toBeInTheDocument()
  })

  it('points the quick filter at /search for real search, carrying the view', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'sunset' }, vi.fn(), { searchHref: '/search?q=sunset' })

    // The quick filter says what it does; the link says where real search lives.
    expect(screen.getByPlaceholderText('Filter by title and description…')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Full-text & semantic search/ })).toHaveAttribute(
      'href',
      '/search?q=sunset',
    )
  })

  it('omits the search link when the page does not offer one', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.queryByRole('link', { name: /Full-text/ })).not.toBeInTheDocument()
  })

  it('toggles the advanced panel open and closed', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    const toggle = screen.getByRole('button', { name: /Filters/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })
})

describe('FilterBar layout', () => {
  // The sort selector must share the search input's row so the two line up. The
  // search hint sits below that row — not inside it — so its extra height cannot
  // stretch the search column and push the selector down under the row's centre
  // alignment. Asserting on structure (not a fragile pixel measurement) keeps the
  // guard honest in jsdom, where layout has no geometry.
  it('keeps the sort selector in the search input row, with the hint outside it', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { searchHref: '/search' })

    const sort = screen.getByLabelText('Sort')
    const searchInput = screen.getByLabelText('Filter the library')
    const row = sort.parentElement
    expect(row).not.toBeNull()

    // Both controls belong to the same header row (the alignment group)...
    expect(row).toContainElement(searchInput)
    // ...but the helper hint is not a member of it, so it can't affect alignment.
    const hint = screen.getByRole('link', { name: /Full-text & semantic search/ })
    expect(row).not.toContainElement(hint)
  })

  it('keeps the sort selector in the search input row when no hint is shown', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    const sort = screen.getByLabelText('Sort')
    const searchInput = screen.getByLabelText('Filter the library')
    expect(sort.parentElement).toContainElement(searchInput)
    expect(screen.queryByRole('link', { name: /Full-text/ })).not.toBeInTheDocument()
  })
})

describe('FilterBar facets', () => {
  it('hides the facet row when the page supplies no options', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.queryByLabelText('Year')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Album')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument()
  })

  it('offers each year present in the catalog with its photo count', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    const year = screen.getByLabelText('Year')
    expect(within(year).getByRole('option', { name: 'Any year' })).toBeInTheDocument()
    expect(within(year).getByRole('option', { name: '2023 (12)' })).toBeInTheDocument()
    expect(within(year).getByRole('option', { name: '2021 (3)' })).toBeInTheDocument()
  })

  it('writes the selected year to the view state', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.selectOptions(screen.getByLabelText('Year'), '2023')
    expect(onChange).toHaveBeenCalledWith({ year: '2023' })
  })

  it('writes the album picked from the searchable select to the view state', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Album'))
    await user.click(screen.getByRole('option', { name: /Holidays/ }))
    expect(onChange).toHaveBeenCalledWith({ album: 'al_1' })
  })

  it('narrows the album options as the reader types, ignoring case and accents', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    await user.type(screen.getByLabelText('Album'), 'namesti')

    expect(screen.getByRole('option', { name: /Náměstí/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /Holidays/ })).not.toBeInTheDocument()
  })

  it('writes the label picked from the searchable select to the view state', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Label'))
    await user.click(screen.getByRole('option', { name: /Portrait/ }))
    expect(onChange).toHaveBeenCalledWith({ label: 'lb_2' })
  })

  it('clears a facet from inside its own select', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1' }, onChange, { facets: FACETS })

    // At rest the select shows the current choice, not a placeholder.
    expect(screen.getByLabelText('Album')).toHaveValue('Holidays')
    await user.click(screen.getByLabelText('Album'))
    await user.click(screen.getByRole('option', { name: 'Any album' }))
    expect(onChange).toHaveBeenCalledWith({ album: '' })
  })

  it('names an album/label chip by its title, not its uid', () => {
    renderBar({ ...LIBRARY_DEFAULTS, year: '2023', album: 'al_1', label: 'lb_1' }, vi.fn(), {
      facets: FACETS,
    })

    expect(screen.getByText('Year: 2023')).toBeInTheDocument()
    expect(screen.getByText('Album: Holidays')).toBeInTheDocument()
    expect(screen.getByText('Label: Beach')).toBeInTheDocument()
  })

  it('falls back to the raw uid when the facet options do not name it', () => {
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_gone' }, vi.fn(), { facets: FACETS })
    expect(screen.getByText('Album: al_gone')).toBeInTheDocument()
  })
})

describe('FilterBar advanced controls', () => {
  it('pushes the minimum-rating filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Rating'), '3')
    expect(onChange).toHaveBeenCalledWith({ min_rating: '3' })
  })

  it('pushes the flag filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Flag'), 'pick')
    expect(onChange).toHaveBeenCalledWith({ flag: 'pick' })
  })

  it('replaces history for live-typed free-text input', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.type(screen.getByLabelText('Camera'), 'C')
    expect(onChange).toHaveBeenCalledWith({ camera: 'C' }, { replace: true })
  })
})

describe('FilterBar active-filter chips', () => {
  it('renders a chip per active filter and badges the toggle count', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4', flag: 'pick' }, vi.fn())

    expect(screen.getByText('Rating: ≥ 4')).toBeInTheDocument()
    expect(screen.getByText('Flag: Picks')).toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: /Filters/ })
    expect(within(toggle).getByText('2')).toBeInTheDocument()
  })

  it('clears a single filter when its chip is dismissed', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, onChange)

    await user.click(screen.getByRole('button', { name: /Remove filter/ }))
    expect(onChange).toHaveBeenCalledWith({ min_rating: '' })
  })

  it('treats an active rating filter as a clearable filter', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, vi.fn())
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument()
  })

  it('does not show chips or clear-all when no filters are active', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Remove filter/ })).not.toBeInTheDocument()
  })

  it('resets every filter but keeps the sort on clear-all', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, sort: 'title', min_rating: '4', camera: 'Canon' }, onChange)

    await user.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(onChange).toHaveBeenCalledWith({ ...LIBRARY_DEFAULTS, sort: 'title' })
  })
})
