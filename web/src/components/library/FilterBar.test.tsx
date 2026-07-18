import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'
import { type AlbumCount, type LabelCount } from '../../services/organize'
import { type SubjectCount } from '../../services/people'

import { FilterBar } from './FilterBar'

function renderBar(
  view: LibraryView,
  onChange: SetUrlState<LibraryView>,
  props: {
    showSearch?: boolean
    showSort?: boolean
    showDensity?: boolean
    facets?: LibraryFacets
    showFavorite?: boolean
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

/** A subject the person facet offers, trimmed to the fields the bar reads. */
function subject(uid: string, name: string, markerCount: number): SubjectCount {
  return {
    uid,
    slug: uid,
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: markerCount,
  }
}

/** The four facet option lists, as `useLibraryFacets` would deliver them. */
const FACETS: LibraryFacets = {
  years: [
    { year: 2023, count: 12 },
    { year: 2021, count: 3 },
  ],
  albums: [album('al_1', 'Holidays', 12), album('al_2', 'Náměstí', 4)],
  labels: [label('lb_1', 'Beach', 7), label('lb_2', 'Portrait', 2)],
  subjects: [subject('su_1', 'Alice', 9), subject('su_2', 'Bob', 5)],
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

  it('shows the grid-density stepper with fewer/more steps and no auto control', () => {
    // A mid-range count leaves both steps live; there is no auto/reset control.
    window.localStorage.setItem('kukatko.grid.density', '4')
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Automatic' })).not.toBeInTheDocument()
  })

  it('persists the density per device instead of writing it to the URL view', async () => {
    window.localStorage.setItem('kukatko.grid.density', '3')
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    expect(onChange).not.toHaveBeenCalled()
    // The stepper pins one more column, persisted per device (never to the URL).
    expect(window.localStorage.getItem('kukatko.grid.density')).toBe('4')
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
    expect(screen.queryByLabelText('Person')).not.toBeInTheDocument()
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

  it('adds a second album to the selection instead of replacing the first', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1' }, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Album'))
    await user.click(screen.getByRole('option', { name: /Náměstí/ }))
    expect(onChange).toHaveBeenCalledWith({ album: 'al_1,al_2' })
  })

  it('drops the already-selected albums from the picker options', async () => {
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1' }, vi.fn(), { facets: FACETS })

    await user.click(screen.getByLabelText('Album'))
    // The chosen album is a chip below, not offered again in the picker.
    expect(screen.queryByRole('option', { name: /Holidays/ })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Náměstí/ })).toBeInTheDocument()
  })

  it('renders one chip per selected album and removes only that one', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' }, onChange, { facets: FACETS })

    expect(screen.getByText('Album: Holidays')).toBeInTheDocument()
    expect(screen.getByText('Album: Náměstí')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Remove filter: Album: Holidays' }))
    expect(onChange).toHaveBeenCalledWith({ album: 'al_2' })
  })

  it('clears the album facet when its last chip is removed', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1' }, onChange, { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Remove filter: Album: Holidays' }))
    expect(onChange).toHaveBeenCalledWith({ album: '' })
  })

  it('supports several labels combined with AND, each removable on its own', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, label: 'lb_1' }, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Label'))
    await user.click(screen.getByRole('option', { name: /Portrait/ }))
    expect(onChange).toHaveBeenCalledWith({ label: 'lb_1,lb_2' })
  })

  it('offers each subject in the person facet with its marker count', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    await user.click(screen.getByLabelText('Person'))
    expect(screen.getByRole('option', { name: /Alice/ })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Bob/ })).toBeInTheDocument()
  })

  it('writes the person picked from the searchable select to the view state', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Person'))
    await user.click(screen.getByRole('option', { name: /Alice/ }))
    expect(onChange).toHaveBeenCalledWith({ person: 'su_1' })
  })

  it('adds a second person to the selection instead of replacing the first', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, person: 'su_1' }, onChange, { facets: FACETS })

    await user.click(screen.getByLabelText('Person'))
    // The already-selected person is a chip below, not offered again here.
    expect(screen.queryByRole('option', { name: /Alice/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: /Bob/ }))
    expect(onChange).toHaveBeenCalledWith({ person: 'su_1,su_2' })
  })

  it('names a person chip by its subject name and colours it with the person hue', () => {
    renderBar({ ...LIBRARY_DEFAULTS, person: 'su_1' }, vi.fn(), { facets: FACETS })

    const chip = screen.getByText('Person: Alice')
    expect(chip).toHaveClass('kk-entity-person')
    expect(chip).not.toHaveClass('text-bg-primary')
  })

  it('removes a single person chip, clearing the facet when its last one goes', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, person: 'su_1' }, onChange, { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Remove filter: Person: Alice' }))
    expect(onChange).toHaveBeenCalledWith({ person: '' })
  })

  it('names an album/label chip by its title, not its uid', () => {
    renderBar({ ...LIBRARY_DEFAULTS, year: '2023', album: 'al_1', label: 'lb_1' }, vi.fn(), {
      facets: FACETS,
    })

    expect(screen.getByText('Year: 2023')).toBeInTheDocument()
    expect(screen.getByText('Album: Holidays')).toBeInTheDocument()
    expect(screen.getByText('Label: Beach')).toBeInTheDocument()
  })

  it('names each chip of a multi-album selection by its own title', () => {
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1,al_2' }, vi.fn(), { facets: FACETS })

    expect(screen.getByText('Album: Holidays')).toBeInTheDocument()
    expect(screen.getByText('Album: Náměstí')).toBeInTheDocument()
  })

  it('falls back to the raw uid when the facet options do not name it', () => {
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_gone' }, vi.fn(), { facets: FACETS })
    expect(screen.getByText('Album: al_gone')).toBeInTheDocument()
  })

  it('colours an album chip and a tag chip with different entity hues', () => {
    renderBar({ ...LIBRARY_DEFAULTS, album: 'al_1', label: 'lb_1' }, vi.fn(), { facets: FACETS })

    const albumChip = screen.getByText('Album: Holidays')
    const tagChip = screen.getByText('Label: Beach')

    // Each carries its kind's hue class, so an album and a tag are told apart at
    // a glance — and neither leaks into the other's colour.
    expect(albumChip).toHaveClass('kk-entity-album')
    expect(tagChip).toHaveClass('kk-entity-tag')
    expect(albumChip).not.toHaveClass('kk-entity-tag')
    expect(tagChip).not.toHaveClass('kk-entity-album')
    // Entity chips drop the shared primary colour that used to mean "album or tag".
    expect(albumChip).not.toHaveClass('text-bg-primary')
    expect(tagChip).not.toHaveClass('text-bg-primary')
  })

  it('keeps the neutral primary colour for non-entity filter chips', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, vi.fn())
    expect(screen.getByText('Rating: ≥ 4')).toHaveClass('text-bg-primary')
  })
})

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; a phone-width test overrides
 * it so the bar takes its narrow branch.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

describe('FilterBar narrow viewport (phone)', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('keeps the facet pickers out of the resting layout, echoing an active one as a chip', () => {
    mockViewport(true)
    renderBar({ ...LIBRARY_DEFAULTS, year: '2023' }, vi.fn(), { facets: FACETS })

    // The four facet selects no longer stack between the search box and the
    // photos — they have folded into the (shut) filters drawer…
    expect(screen.queryByLabelText('Year')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Album')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Person')).not.toBeInTheDocument()
    // …yet an active facet stays visible as a chip, so the filtered set is never a
    // mystery even with the drawer closed.
    expect(screen.getByText('Year: 2023')).toBeInTheDocument()
  })

  it('reveals the facet pickers inside the filters drawer once it is opened', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    expect(screen.queryByLabelText('Year')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Filters/ }))
    // The same progressive-disclosure surface the advanced filters already used
    // now carries the facets too.
    expect(await screen.findByLabelText('Year')).toBeInTheDocument()
    expect(screen.getByLabelText('Album')).toBeInTheDocument()
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

  it('pushes the eye flag filter when the eye option is selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Flag'), 'eye')
    expect(onChange).toHaveBeenCalledWith({ flag: 'eye' })
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

  it('renders the eye value on the flag chip', () => {
    renderBar({ ...LIBRARY_DEFAULTS, flag: 'eye' }, vi.fn())
    expect(screen.getByText('Flag: Eyes')).toBeInTheDocument()
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

describe('FilterBar favorites toggle', () => {
  it('hides the favorites control unless the page opts in', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    await openPanel(user)
    expect(screen.queryByLabelText('Favorites')).not.toBeInTheDocument()
  })

  it('sets the favorite filter when favorites-only is chosen', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { showFavorite: true })

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Favorites'), 'true')
    expect(onChange).toHaveBeenCalledWith({ favorite: 'true' })
  })

  it('clears the favorite filter when it is switched back to any', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, favorite: 'true' }, onChange, { showFavorite: true })

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Favorites'), '')
    expect(onChange).toHaveBeenCalledWith({ favorite: '' })
  })

  it('renders a removable Favorites chip when the filter is active', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, favorite: 'true' }, onChange, { showFavorite: true })

    // The chip carries the neutral primary colour (favorites is not an entity).
    const chip = screen.getByText('Favorites', { selector: '.kukatko-filter-chip' })
    expect(chip).toHaveClass('text-bg-primary')
    await user.click(screen.getByRole('button', { name: 'Remove filter: Favorites' }))
    expect(onChange).toHaveBeenCalledWith({ favorite: '' })
  })
})
