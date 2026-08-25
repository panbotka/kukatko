import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CapabilitiesContext } from '../../capabilities/CapabilitiesContext'
import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'
import { type AlbumCount, type LabelCount } from '../../services/organize'
import { type SubjectCount } from '../../services/people'
import { type UploaderBucket } from '../../services/photos'

import { FilterBar } from './FilterBar'

/** The knobs a test may turn, on top of the view and its change handler. */
interface BarProps {
  showSearch?: boolean
  showSort?: boolean
  showDensity?: boolean
  facets?: LibraryFacets
  uploaders?: readonly UploaderBucket[]
  showFavorite?: boolean
  searchHref?: string
  /** The `semantic_search` capability the bar reads; defaults to available. */
  semanticSearch?: boolean
  /** The count to state; pass `undefined` for "there is nothing to count". */
  total?: number
  /** The page's own view actions, which the drawer hosts on a phone. */
  mobileActions?: ReactNode
}

/**
 * The bar inside the providers it needs, as an element rather than a render, so
 * a test can hand the very same tree to `rerender` — that is how the page
 * delivering a fresh `total` under an open drawer is reproduced.
 */
function barTree(view: LibraryView, onChange: SetUrlState<LibraryView>, props: BarProps = {}) {
  const { semanticSearch = true, ...barProps } = props
  // `in`, not a default: passing `total: undefined` is itself the case under
  // test ("nothing to count"), and a default would swallow it back to 0.
  const total = 'total' in props ? props.total : 0
  return (
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider value={{ semantic_search: semanticSearch, known: true }}>
        <MemoryRouter>
          <FilterBar view={view} onChange={onChange} total={total} {...barProps} />
        </MemoryRouter>
      </CapabilitiesContext.Provider>
    </I18nextProvider>
  )
}

function renderBar(view: LibraryView, onChange: SetUrlState<LibraryView>, props: BarProps = {}) {
  return render(barTree(view, onChange, props))
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
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/**
 * A subject the person facet offers, trimmed to the fields the bar reads. The
 * marker count is deliberately the larger of the two — the bar filters photos, so
 * showing the marker count would overstate what picking the person yields.
 */
function subject(uid: string, name: string, photoCount: number): SubjectCount {
  return {
    uid,
    slug: uid,
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photoCount + 100,
    photo_count: photoCount,
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

/**
 * The uploader facet, as `useUploaders` would deliver it: the contributors to
 * the current view, largest first, with the imported photos as the nameless
 * bucket the control has to word itself.
 */
const UPLOADERS: readonly UploaderBucket[] = [
  { uid: 'us_1', name: 'Tomáš Novák', count: 12 },
  { uid: 'us_2', name: 'Anna', count: 3 },
  { uid: '', name: '', count: 2 },
]

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

  it('states the photo count it was given', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: 3 })
    expect(screen.getByText('Photos: 3')).toBeInTheDocument()
  })

  it('states nothing when there is no result set to count', () => {
    // The search page before a query is typed: "Photos: 0" would read as an
    // empty library rather than as "nothing searched for yet".
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: undefined })
    expect(screen.queryByText(/^photos:/i)).not.toBeInTheDocument()
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

    // The quick filter says what it does — text *or* a filter; the link says
    // where ranked full-text and semantic search live.
    expect(
      screen.getByPlaceholderText('Search — text, or a filter like year:1965 or person:Jarmila'),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Full-text & semantic search/ })).toHaveAttribute(
      'href',
      '/search?q=sunset',
    )
  })

  it('omits the search link when the page does not offer one', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.queryByRole('link', { name: /Full-text/ })).not.toBeInTheDocument()
  })

  it('hides the semantic-search link when the embeddings box is offline, keeping the filter help', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'sunset' }, vi.fn(), {
      searchHref: '/search?q=sunset',
      semanticSearch: false,
    })

    // The link that promises semantic search is gone — full-text still works,
    // but there is no point advertising semantics while the box is unreachable.
    expect(screen.queryByRole('link', { name: /Full-text/ })).not.toBeInTheDocument()
    // …while the quick-filter help text (unrelated to embeddings) stays put.
    expect(screen.getByText(/Searches title, description and notes/)).toBeInTheDocument()
  })

  it('shows the semantic-search link and the filter help when the box is reachable', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'sunset' }, vi.fn(), {
      searchHref: '/search?q=sunset',
      semanticSearch: true,
    })

    expect(screen.getByText(/Searches title, description and notes/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Full-text & semantic search/ })).toHaveAttribute(
      'href',
      '/search?q=sunset',
    )
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
  it('drops the entity pickers when the page supplies no options, keeping the period', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    // Every grid can be narrowed in time, including one already scoped to an
    // album or a place, so the period control is not part of the facet bundle.
    expect(screen.getByRole('button', { name: 'Period: Any period' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Album')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Person')).not.toBeInTheDocument()
  })

  it('offers only the decades the library holds, expandable to their years', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Period: Any period' }))
    // 2021 and 2023 live in one decade, which carries both their counts…
    expect(screen.getByRole('button', { name: '2020–2029 15' })).toBeInTheDocument()
    // …and no decade is offered that the library cannot answer.
    expect(screen.queryByRole('button', { name: /^1960–1969/ })).not.toBeInTheDocument()
    // The years hide behind the decade until they are asked for.
    expect(screen.queryByRole('button', { name: '2023 12' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show the years of 2020–2029' }))
    expect(screen.getByRole('button', { name: '2023 12' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '2021 3' })).toBeInTheDocument()
  })

  it('writes a picked decade to the view state as one inclusive period', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Period: Any period' }))
    await user.click(screen.getByRole('button', { name: '2020–2029 15' }))

    expect(onChange).toHaveBeenCalledWith({
      taken_after: '2020-01-01',
      taken_before: '2029-12-31',
      year: '',
    })
  })

  it('writes a picked year the same way, through the same pair of keys', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Period: Any period' }))
    await user.click(screen.getByRole('button', { name: 'Show the years of 2020–2029' }))
    await user.click(screen.getByRole('button', { name: '2023 12' }))

    expect(onChange).toHaveBeenCalledWith({
      taken_after: '2023-01-01',
      taken_before: '2023-12-31',
      year: '',
    })
  })

  it('states the period in words on the trigger instead of making it be opened', () => {
    renderBar(
      { ...LIBRARY_DEFAULTS, taken_after: '1960-01-01', taken_before: '1969-12-31' },
      vi.fn(),
      { facets: FACETS },
    )
    expect(screen.getByRole('button', { name: 'Period: 1960–1969' })).toBeInTheDocument()
  })

  it('words an open-ended period as such', () => {
    renderBar({ ...LIBRARY_DEFAULTS, taken_before: '1949-12-31' }, vi.fn())
    expect(screen.getByRole('button', { name: 'Period: until 1949' })).toBeInTheDocument()
  })

  it('keeps the exact-date fields inside the one period control', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, taken_after: '2019-06-01' }, onChange, { facets: FACETS })

    // Not a second filter in the advanced panel: they are the fine grain of the
    // same one, so "summer 2019" is reachable without leaving the control.
    expect(screen.queryByLabelText('Taken from')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /^Period:/ }))
    const from = screen.getByLabelText('Taken from')
    expect(from).toHaveValue('2019-06-01')

    fireEvent.change(screen.getByLabelText('Taken until'), { target: { value: '2019-08-31' } })
    expect(onChange).toHaveBeenCalledWith({
      taken_after: '2019-06-01',
      taken_before: '2019-08-31',
      year: '',
    })
  })

  it('folds a legacy year= URL into the period rather than dropping it', () => {
    renderBar({ ...LIBRARY_DEFAULTS, year: '1965' }, vi.fn(), { facets: FACETS })
    expect(screen.getByRole('button', { name: 'Period: 1965' })).toBeInTheDocument()
    expect(screen.getByText('Period: 1965')).toBeInTheDocument()
  })

  it('clears the period, the legacy key included, from its own resting row', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, year: '1965' }, onChange, { facets: FACETS })

    await user.click(screen.getByRole('button', { name: 'Period: 1965' }))
    await user.click(screen.getByRole('button', { name: 'Any period' }))
    expect(onChange).toHaveBeenCalledWith({ taken_after: '', taken_before: '', year: '' })
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

    const remove = screen.getByRole('button', { name: 'Remove filter: Album: Holidays' })
    // Every chip carries its own X, so the hover hint has to say which filter
    // this one drops — the same sentence the accessible name carries.
    expect(remove).toHaveAttribute('title', 'Remove filter: Album: Holidays')

    await user.click(remove)
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

  it('offers each subject in the person facet with its photo count', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    await user.click(screen.getByLabelText('Person'))
    // The count beside a person is how many photos picking them yields — the same
    // question the album and label options answer, not how many faces were found.
    expect(screen.getByRole('option', { name: /Alice/ })).toHaveTextContent('9')
    expect(screen.getByRole('option', { name: /Alice/ })).not.toHaveTextContent('109')
    expect(screen.getByRole('option', { name: /Bob/ })).toHaveTextContent('5')
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

    expect(screen.getByText('Period: 2023')).toBeInTheDocument()
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

/**
 * Opens the phone drawer and returns its panel. An open Offcanvas is a portal
 * and the bar's own controls are siblings in the same document (not
 * `aria-hidden`), so anything asserted *about the drawer* has to be scoped to
 * this element with `within()`.
 */
async function openFilterDrawer(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Filters/ }))
  return screen.findByRole('dialog')
}

describe('FilterBar narrow viewport (phone)', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('keeps the primary pickers out of the resting layout, echoing an active one as a chip', () => {
    mockViewport(true)
    renderBar(
      { ...LIBRARY_DEFAULTS, taken_after: '2023-01-01', taken_before: '2023-12-31' },
      vi.fn(),
      { facets: FACETS },
    )

    // The four pickers no longer stack between the search box and the photos —
    // they have folded into the (shut) filters drawer…
    expect(screen.queryByRole('button', { name: /^Period:/ })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Album')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Person')).not.toBeInTheDocument()
    // …yet an active filter stays visible as a chip, so the filtered set is never
    // a mystery even with the drawer closed.
    expect(screen.getByText('Period: 2023')).toBeInTheDocument()
  })

  it('reveals the primary pickers inside the filters drawer once it is opened', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS })

    expect(screen.queryByRole('button', { name: /^Period:/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Filters/ }))
    // The same progressive-disclosure surface the advanced filters already used
    // now carries the primary row too.
    expect(await screen.findByRole('button', { name: /^Period:/ })).toBeInTheDocument()
    expect(screen.getByLabelText('Album')).toBeInTheDocument()
  })

  it('leaves the phone header with the search field, the Filters button and the count', () => {
    mockViewport(true)
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: 20637, searchHref: '/search?q=' })

    // What survives above the photographs…
    expect(screen.getByLabelText('Filter the library')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Filters/ })).toBeInTheDocument()
    // …with the one number the bar states riding beside the button that changes
    // it, rather than on a line of its own.
    expect(screen.getByText('Photos: 20637')).toBeInTheDocument()

    // …and what does not: the display controls and the note explaining the query
    // language, which together cost two of the header's three rows.
    expect(screen.queryByLabelText('Sort')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Tiles per row')).not.toBeInTheDocument()
    expect(screen.queryByText(/Searches title, description and notes/)).not.toBeInTheDocument()
  })

  it('folds the sort order, the density and the search note into the drawer', async () => {
    mockViewport(true)
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { searchHref: '/search?q=' })

    const drawer = await openFilterDrawer(user)
    // The controls are not merely present somewhere — they are inside the
    // drawer, which is the only place a phone can reach them now.
    await user.selectOptions(within(drawer).getByLabelText('Sort'), 'oldest')
    expect(onChange).toHaveBeenCalledWith({ sort: 'oldest' })
    expect(within(drawer).getByLabelText('Tiles per row')).toBeInTheDocument()
    expect(within(drawer).getByText(/Searches title, description and notes/)).toBeInTheDocument()
  })

  it('hosts the page own view actions at the foot of the drawer, and only there', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), {
      mobileActions: <button type="button">Save view</button>,
    })

    // A phone has no heading row for them to sit in, so until the drawer is
    // opened they are nowhere — and never in the document twice.
    expect(screen.queryByRole('button', { name: 'Save view' })).not.toBeInTheDocument()
    const drawer = await openFilterDrawer(user)
    expect(within(drawer).getByRole('button', { name: 'Save view' })).toBeInTheDocument()
  })

  it('ignores the page view actions on a desktop, where the page keeps them itself', async () => {
    mockViewport(false)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), {
      mobileActions: <button type="button">Save view</button>,
    })

    await user.click(screen.getByRole('button', { name: /Filters/ }))
    // The desktop panel is the same `panel` element as the drawer's body; were
    // the actions unconditional, opening it would put a second copy of the
    // page's own buttons on the page.
    expect(screen.queryByRole('button', { name: 'Save view' })).not.toBeInTheDocument()
  })
})

/**
 * The drawer's footer, the fix for filtering blind on a phone. The drawer covers
 * the screen, so the "Photos: N" line the bar keeps beside the grid is behind
 * it: the reader used to set a year, a person and a rating, scroll ten fields
 * back up to the cross — the only exit — and only there learn the combination
 * matched nothing. These tests hold the footer to the two promises that fix it:
 * the count is readable *inside* the drawer and follows every filter change, and
 * there is always a way out, the empty result very much included.
 *
 * An open Offcanvas is a portal, so each test scopes its queries to the
 * `role="dialog"` panel: the bar's own controls are siblings in the same
 * document and are not `aria-hidden`, so an unscoped `getByRole` would happily
 * match the wrong button.
 */
describe('FilterBar drawer footer (phone)', () => {
  afterEach(() => {
    mockViewport(false)
  })

  /** Opens the drawer and returns its panel, scoped for `within()`. */
  async function openDrawer(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole('button', { name: /Filters/ }))
    return screen.findByRole('dialog')
  }

  it('carries the live result count on the button that closes the drawer', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: 227 })

    const drawer = await openDrawer(user)
    const apply = within(drawer).getByRole('button', { name: 'Show 227 photos' })

    await user.click(apply)
    // Back on the grid, with the filters the reader just set still in force.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Filters/ })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
  })

  it('restates the count as the filters change, without closing the drawer', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    const props = { facets: FACETS, total: 227 }
    const { rerender } = renderBar(LIBRARY_DEFAULTS, vi.fn(), props)

    const drawer = await openDrawer(user)
    expect(within(drawer).getByRole('button', { name: 'Show 227 photos' })).toBeInTheDocument()

    // The page refetches under the newly picked period and hands the bar a new
    // total. That is the whole point: the number moves while the drawer is open.
    rerender(
      barTree({ ...LIBRARY_DEFAULTS, taken_after: '2023-01-01' }, vi.fn(), {
        ...props,
        total: 12,
      }),
    )

    const stillOpen = screen.getByRole('dialog')
    expect(within(stillOpen).getByRole('button', { name: 'Show 12 photos' })).toBeInTheDocument()
    expect(within(stillOpen).getByRole('button', { name: /^Period:/ })).toBeInTheDocument()
  })

  it('says a single photo in the singular', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: 1 })

    const drawer = await openDrawer(user)
    expect(within(drawer).getByRole('button', { name: 'Show 1 photo' })).toBeInTheDocument()
  })

  it('still lets the reader out when the filters match nothing', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '5' }, vi.fn(), { total: 0 })

    const drawer = await openDrawer(user)
    // "Show 0 photos" would promise a grid that is not there; the button admits
    // the set is empty and stays the way out regardless.
    const apply = within(drawer).getByRole('button', { name: 'No photos — close' })
    expect(apply).toBeEnabled()

    await user.click(apply)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('drops the number when the page has no result set to count', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    // The search page before a query is typed: nothing has been searched for, so
    // a count would be an invention rather than an answer.
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: undefined })

    const drawer = await openDrawer(user)
    expect(within(drawer).getByRole('button', { name: 'Close filters' })).toBeInTheDocument()
    expect(within(drawer).queryByRole('button', { name: /Show/ })).not.toBeInTheDocument()
  })

  it('clears the filters from the footer without closing the drawer', async () => {
    mockViewport(true)
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '5', flag: 'pick' }, onChange, { total: 0 })

    const drawer = await openDrawer(user)
    await user.click(within(drawer).getByRole('button', { name: 'Clear filters' }))

    expect(onChange).toHaveBeenCalledWith({ ...LIBRARY_DEFAULTS, sort: LIBRARY_DEFAULTS.sort })
    // Clearing is how the reader recovers from an empty set, so the drawer stays
    // open for the count on the button beside it to show the recovery worked.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('offers nothing to clear when no filter is set', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { total: 5 })

    const drawer = await openDrawer(user)
    expect(within(drawer).getByRole('button', { name: 'Show 5 photos' })).toBeInTheDocument()
    expect(within(drawer).queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()
  })

  it('keeps the footer outside the scroll area, so it cannot cover the last field', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { facets: FACETS, total: 5 })

    const drawer = await openDrawer(user)
    const body = drawer.querySelector('.offcanvas-body')
    const footer = drawer.querySelector('.offcanvas-footer')
    expect(body).not.toBeNull()
    expect(footer).not.toBeNull()
    // Siblings in the offcanvas' flex column: the body is the scroller and the
    // footer is subtracted from it, so the last field scrolls above the footer
    // rather than under it. Were the footer inside the body it would scroll away
    // with the fields — and reserving padding instead would drift the moment the
    // footer's own height changed.
    expect(body?.contains(footer)).toBe(false)
    expect(footer?.parentElement).toBe(drawer)
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

  it('offers the flag values under the names the viewer uses, not the glyphs', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    await openPanel(user)
    const options = within(screen.getByLabelText('Flag')).getAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual([
      'Any',
      'Picked',
      'Rejected',
      'To look at later',
    ])
  })

  it('offers the contributors to the current view, with their counts', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { uploaders: UPLOADERS })

    await openPanel(user)
    const options = within(screen.getByLabelText('Uploaded by')).getAllByRole('option')
    // Largest contribution first, "anyone" at rest, and the photos nobody
    // uploaded named rather than shown as the reserved word behind them.
    expect(options.map((o) => o.textContent)).toEqual([
      'Anyone',
      'Tomáš Novák (12)',
      'Anna (3)',
      'Imported (2)',
    ])
  })

  it('pushes the picked uploader to the view state', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { uploaders: UPLOADERS })

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Uploaded by'), 'us_1')
    expect(onChange).toHaveBeenCalledWith({ uploader: 'us_1' })
  })

  it('picks the imported photos through the reserved value', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange, { uploaders: UPLOADERS })

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Uploaded by'), 'none')
    expect(onChange).toHaveBeenCalledWith({ uploader: 'none' })
  })

  it('omits the uploader control on a page that offers no facet', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    await openPanel(user)
    expect(screen.queryByLabelText('Uploaded by')).not.toBeInTheDocument()
  })

  it('admits when the query has already taken the uploader over', async () => {
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, q: 'uploader:tomas' }, vi.fn(), { uploaders: UPLOADERS })

    await openPanel(user)
    const select = screen.getByLabelText('Uploaded by')
    expect(within(select).getAllByRole('option')[0]).toHaveTextContent('Set by the query')
    expect(screen.getByText('uploader:tomas')).toBeInTheDocument()
  })

  it('names the chosen uploader on its chip, and the imported group by name', () => {
    const { rerender } = renderBar({ ...LIBRARY_DEFAULTS, uploader: 'us_1' }, vi.fn(), {
      uploaders: UPLOADERS,
    })
    expect(screen.getByText('Uploaded by: Tomáš Novák')).toBeInTheDocument()

    rerender(barTree({ ...LIBRARY_DEFAULTS, uploader: 'none' }, vi.fn(), { uploaders: UPLOADERS }))
    expect(screen.getByText('Uploaded by: Imported')).toBeInTheDocument()
  })

  it('keeps a picked uploader on offer when the rest of the view excludes them', async () => {
    const user = userEvent.setup()
    // `us_9` uploaded nothing that survives the other filters, so the facet does
    // not report them — but the filter is on, and a select reading "Anyone" over
    // a grid narrowed to one person would be the control lying about the results.
    renderBar({ ...LIBRARY_DEFAULTS, uploader: 'us_9' }, vi.fn(), { uploaders: UPLOADERS })

    await openPanel(user)
    const select = screen.getByLabelText<HTMLSelectElement>('Uploaded by')
    expect(select.value).toBe('us_9')
    expect(within(select).getByRole('option', { name: 'us_9 (0)' })).toBeInTheDocument()
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
    expect(screen.getByText('Flag: Picked')).toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: /Filters/ })
    expect(within(toggle).getByText('2')).toBeInTheDocument()
  })

  it('renders the eye value on the flag chip', () => {
    renderBar({ ...LIBRARY_DEFAULTS, flag: 'eye' }, vi.fn())
    expect(screen.getByText('Flag: To look at later')).toBeInTheDocument()
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

describe('FilterBar query language', () => {
  // The field always ran the whole `key:value` language; it only ever claimed
  // otherwise. These guard the claim, not the parser (which is the backend's).
  it('describes the query language in the placeholder and the hint', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { searchHref: '/search' })

    expect(screen.getByLabelText('Filter the library')).toHaveAttribute(
      'placeholder',
      expect.stringContaining('year:1965'),
    )
    expect(screen.getByText(/understands filters like year:1965/)).toBeInTheDocument()
  })

  it('offers the same query-language help the search page opens', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    const help = screen.getByRole('button', { name: 'Search query language help' })
    // Beside the field, in the header row — not buried in the filters drawer.
    expect(help.parentElement).toContainElement(screen.getByLabelText('Filter the library'))

    await user.click(help)
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent('Search query language')
  })

  it('hides the help when the page owns the query box itself', () => {
    // `/search` renders the bar with `showSearch={false}`; its own box already
    // carries the `?`, and a second one would be two triggers for one language.
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { showSearch: false })
    expect(
      screen.queryByRole('button', { name: 'Search query language help' }),
    ).not.toBeInTheDocument()
  })

  it('shows the period the query sets, so the two cannot contradict each other', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'year:1960-1969' }, vi.fn(), { facets: FACETS })

    // "Any period" over a grid holding only the sixties was the contradiction;
    // the control now reads the period out of the query itself.
    const period = screen.getByRole('button', { name: 'Period: 1960–1969' })
    expect(screen.queryByRole('button', { name: /Any period/ })).not.toBeInTheDocument()
    expect(screen.getByText('year:1960-1969')).toBeInTheDocument()
    expect(period).toHaveAttribute('aria-describedby', 'library-period-from-query')
  })

  it('quotes the tokens without inventing a period when the query sets several', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'year:1960-1969 before:1965-01-01' }, vi.fn())

    // Two tokens narrow the grid together; showing one of them as *the* period
    // would be a fresh contradiction rather than a fix.
    expect(screen.getByRole('button', { name: 'Period: Any period' })).toBeInTheDocument()
    expect(screen.getByText('year:1960-1969 before:1965-01-01')).toBeInTheDocument()
  })

  it('flags the period control for a date key too, not only for year:', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'after:2024-05-01' }, vi.fn())

    expect(screen.getByRole('button', { name: /^Period:/ })).toHaveAttribute(
      'aria-describedby',
      'library-period-from-query',
    )
    expect(screen.getByText('after:2024-05-01')).toBeInTheDocument()
  })

  it('flags the album and person pickers the query has taken over', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'album:"Léto 2024" subject:Jarmila' }, vi.fn(), {
      facets: FACETS,
    })

    // Aliases count: `subject:` is `person:` under another name.
    expect(screen.getByLabelText('Album')).toHaveAttribute('placeholder', 'Set by the query')
    expect(screen.getByLabelText('Person')).toHaveAttribute('placeholder', 'Set by the query')
    // The note is tied to the control, so it is announced with it and not just
    // read as loose text that happens to sit nearby.
    expect(screen.getByLabelText('Album')).toHaveAttribute(
      'aria-describedby',
      'library-album-from-query',
    )
    expect(screen.getByLabelText('Label')).not.toHaveAttribute('aria-describedby')
    expect(screen.getByText('album:"Léto 2024"')).toBeInTheDocument()
    expect(screen.getByText('subject:Jarmila')).toBeInTheDocument()
    // Untouched facets keep their own resting label.
    expect(screen.getByLabelText('Label')).toHaveAttribute('placeholder', 'Any label')
  })

  it('leaves every picker alone for a plain free-text query', () => {
    renderBar({ ...LIBRARY_DEFAULTS, q: 'svatba' }, vi.fn(), { facets: FACETS })

    expect(screen.getByRole('button', { name: 'Period: Any period' })).toBeInTheDocument()
    expect(screen.queryByText(/Already set by the query/)).not.toBeInTheDocument()
  })

  it('says nothing about a filter key the language does not know', () => {
    // `osoba:` is a typo for `person:`: it filters nothing (the backend searches
    // it as text), so claiming it drives the Person picker would be a new lie.
    renderBar({ ...LIBRARY_DEFAULTS, q: 'osoba:Jarmila' }, vi.fn(), { facets: FACETS })

    expect(screen.getByLabelText('Person')).toHaveAttribute('placeholder', 'Any person')
    expect(screen.queryByText(/Already set by the query/)).not.toBeInTheDocument()
  })
})
