import type { TFunction } from 'i18next'
import { type ReactNode, useMemo, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Collapse from 'react-bootstrap/Collapse'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import Offcanvas from 'react-bootstrap/Offcanvas'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useCapabilities } from '../../capabilities/CapabilitiesContext'
import { useIsNarrowViewport } from '../../hooks/useIsNarrowViewport'
import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import {
  addToFilterList,
  hasActiveFilters,
  type LibraryView,
  LIBRARY_DEFAULTS,
  parseFilterList,
  periodOf,
  periodPatch,
  UPLOADER_NONE,
} from '../../lib/libraryView'
import { periodFromQuery } from '../../lib/period'
import { FACET_QUERY_KEYS, facetQueryTokens, queryFilterTokens } from '../../lib/queryLanguage'
import { type SetUrlState } from '../../lib/urlState'
import { type UploaderBucket } from '../../services/photos'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'
import { SearchQueryHelp } from '../search/SearchQueryHelp'

import { buildChips } from './filterChips'
import { GridDensityControl } from './GridDensityControl'
import { PeriodFilter } from './PeriodFilter'
import { SearchableSelect } from './SearchableSelect'

/** DOM id of the collapsible / offcanvas advanced-filter panel. */
const PANEL_ID = 'library-filter-panel'

/** Props for {@link FilterBar}. */
export interface FilterBarProps<T extends LibraryView> {
  view: T
  onChange: SetUrlState<T>
  /**
   * Total number of photos matching the current filters, shown as a count. Omit
   * it when there is no result set to count — the search page before a query is
   * typed — so the bar states nothing rather than claiming zero photos. On a
   * phone it is also what the drawer's primary button says
   * ({@link FilterDrawerFooter}), so the reader learns what a filter combination
   * yields without closing the drawer to find out.
   */
  total?: number
  /**
   * Whether to show the query field (and, with it, the query-language help
   * beside it). The search page hides both (`false`) because its prominent
   * query box already owns `q` and carries its own `?`. Defaults true.
   */
  showSearch?: boolean
  /**
   * Whether to show the sort control. The search page hides it (`false`) because
   * results are ranked by relevance/similarity and the backend ignores sort in
   * search modes. Defaults true.
   */
  showSort?: boolean
  /**
   * Which sort options the selector offers, as `view.sort` values. Omit for the
   * full library list; an album passes the two orders it has (`oldest`,
   * `newest`), because its sort *key* is pinned server-side to capture time and
   * offering "by title" there would be a control that quietly does nothing.
   */
  sortOptions?: readonly string[]
  /**
   * Whether to show the grid-density picker. The trash hides it (`false`) because
   * its grid is a card list, not the photo grid the density governs. Defaults true.
   */
  showDensity?: boolean
  /**
   * The Album / Label / Person facet option lists plus the years the library
   * holds. Omit on pages whose grid is already scoped to one album, label or
   * place: the three entity pickers are then dropped from the primary row (the
   * period control stays — every grid can be narrowed in time), and the period
   * control offers its exact dates without the decade list it has no counts for.
   * Album titles, label names and subject names also let the chips name a filter
   * instead of showing its UID.
   */
  facets?: LibraryFacets
  /**
   * Who uploaded the photos of the current view, with their counts — the option
   * list behind the uploader control. Passed separately from `facets` because it
   * outlives them: a page scoped to one album drops the album/label/person
   * pickers but is exactly where "show me what this person contributed" is
   * asked. Omit (or pass an empty list) and no uploader control is rendered.
   */
  uploaders?: readonly UploaderBucket[]
  /**
   * Whether to show the favorites toggle. Off by default: pages already scoped to
   * favorites (the Favorites page) would only offer a redundant, conflicting
   * control. The library opts in so "favorites + album + period" can be combined
   * in the main grid.
   */
  showFavorite?: boolean
  /**
   * Where the "search the full text instead" link points, carrying the current
   * view. Omit on pages that are not the library (and on `/search` itself, which
   * is the destination), and no link is rendered.
   */
  searchHref?: string
  /**
   * Page-level view actions (Slideshow, Save view) to host at the foot of the
   * filters drawer, rendered **only on a narrow viewport**. A phone has no room
   * for a page-heading row above the photos, so the page hands its actions here
   * instead of spending a line of the first screen on them; on desktop the page
   * keeps them in its own header and passes nothing.
   */
  mobileActions?: ReactNode
}

/**
 * Library filter + sort controls, built for a calm default and progressive
 * disclosure. On desktop the header is a single row: a prominent quick-filter
 * field (the visual anchor, matching title and description as you type), the
 * sort selector, the grid-density picker (how many photos sit side by side — a
 * per-device display preference, not part of the view), and a "Filters" toggle
 * badged with the count of active filters. The primary filter row — Period,
 * Album, Label, Person, the ways photos are actually found — sits below it in
 * its own always-visible four-across row (the three entity pickers only when the
 * page supplies `facets`). The remaining filters (camera, archived, favorites,
 * location, min rating, flag, uploader) live in a collapsible panel, so the
 * resting state stays uncluttered — the favorites toggle only when the page opts
 * in via `showFavorite`, the uploader only when the page supplies `uploaders`.
 *
 * **On a phone the header is the search field and the Filters button, and
 * nothing else.** Photos are browsed mostly on phones, which is where the app
 * has the least room: the desktop header alone stacked into three rows there,
 * and with the page heading, the search note and the count line above it the
 * first photo started past 350 px of a 852 px screen. So everything the header
 * carries besides the search — the sort order, the grid density, the primary
 * pickers, the note explaining the query language, and the page's own view
 * actions (`mobileActions`) — folds into the offcanvas drawer, which has room to
 * spare, and the result count rides in the header row itself, beside the
 * Filters button that opens them ({@link FilterDrawerFooter} states it again on
 * the way out). What is left above the photos is one field and one button.
 *
 * Every active filter — the primary row included — is echoed as a removable chip
 * plus a single clear-all action, so a filtered set is never a mystery even
 * while the drawer is shut. The drawer closes on a sticky footer carrying the
 * live result count ({@link FilterDrawerFooter}) rather than only on the cross
 * ten fields back up.
 *
 * There is exactly **one** control per thing being filtered. The time axis used
 * to have two — a Year dropdown of single years in the primary row and a
 * "taken after / taken before" pair buried in the panel — which between them
 * could neither express a decade nor agree with each other;
 * {@link PeriodFilter} is both of them, in the primary row, over one pair of URL
 * keys.
 *
 * The quick filter speaks the whole `key:value` query language, exactly as
 * `/search` does — `year:1960-1969` narrows the grid to the sixties here too —
 * with the residual free text matching title, description and notes as a
 * substring. So it carries the same {@link SearchQueryHelp} `?` the search page
 * uses rather than a second, drifting explanation of the language, and the
 * facet pickers below flag the facets the query has already taken over. What
 * `/search` adds is *ranking* — full-text relevance and semantic similarity —
 * which is what `searchHref` links to when the embeddings box is reachable.
 *
 * Facet and enum filters push a history entry (so Back steps through views) while
 * the free-text inputs replace it (so live typing does not flood history). All
 * state lives in the URL via `onChange`; the bar is fully controlled by `view`.
 * Generic over the view type so it serves both the library ({@link LibraryView})
 * and the search page (a superset adding `mode`); only the library fields are
 * ever written here, so any extra fields (e.g. the search mode) are preserved
 * untouched.
 */
export function FilterBar<T extends LibraryView>({
  view,
  onChange,
  total,
  showSearch = true,
  showSort = true,
  sortOptions,
  showDensity = true,
  facets,
  uploaders,
  showFavorite = false,
  searchHref,
  mobileActions,
}: FilterBarProps<T>) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const narrow = useIsNarrowViewport()
  // Only advertise semantic search when the embeddings box is reachable; the
  // plain quick-filter help below is unrelated and stays regardless.
  const { semantic_search: semanticSearch } = useCapabilities()

  const push = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>)
  }
  const replace = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>, { replace: true })
  }

  const chips = buildChips(view, t, i18n.language, { facets, uploaders })
  const clearVisible = hasActiveFilters(view, { ignoreQuery: !showSearch })
  // Which filters the query itself already sets. The pickers below would
  // otherwise keep reading "any period" while `year:1960-1969` in the box has
  // filtered the grid down to the sixties.
  const queryFilters = useMemo(() => queryFilterTokens(view.q), [view.q])

  const clearAll = () => {
    // Keep the current sort, and keep the query when it is owned by the page's
    // own search box (search page) rather than this bar.
    push({ ...LIBRARY_DEFAULTS, sort: view.sort, ...(showSearch ? {} : { q: view.q }) })
  }

  // The note under the search field, rendered in exactly one place: below the
  // header row on desktop, inside the drawer on a phone.
  const searchNote =
    showSearch && searchHref !== undefined ? (
      <SearchNote href={searchHref} semanticSearch={semanticSearch} />
    ) : null

  // Everything the phone header cannot afford folds in here, in the order a
  // reader looks for it: the two display controls the header used to carry
  // (sort, density), then the primary pickers, then the advanced filters, then
  // the reference material (what the search box understands) and the page's own
  // view actions. On desktop the display controls and the primary row keep their
  // always-visible places above and this panel holds only the advanced filters.
  const panel = (
    <>
      {narrow && (showSort || showDensity) && (
        <>
          <DisplayControls
            view={view}
            push={push}
            showSort={showSort}
            sortOptions={sortOptions}
            showDensity={showDensity}
          />
          <hr className="my-3" />
        </>
      )}
      {narrow && (
        <>
          <PrimaryFilterRow view={view} facets={facets} push={push} queryFilters={queryFilters} />
          <hr className="my-3" />
        </>
      )}
      <AdvancedFilters
        view={view}
        push={push}
        replace={replace}
        showFavorite={showFavorite}
        uploaders={uploaders}
        queryFilters={queryFilters}
      />
      {narrow && searchNote !== null && (
        <>
          <hr className="my-3" />
          {searchNote}
        </>
      )}
      {narrow && mobileActions !== undefined && (
        <>
          <hr className="my-3" />
          {/* `d-grid` stretches whatever the page handed over into full-width
              rows, so a page never has to know how its buttons are laid out
              here. */}
          <div className="d-grid gap-2">{mobileActions}</div>
        </>
      )}
    </>
  )

  // The result count and the clear-all action. On desktop they are a row of
  // their own under the bar. On a phone the row rides *inside* the header's flex
  // row as a full-width wrapped line, so the count lands directly beneath the
  // Filters button (`flex-row-reverse` keeps it on that side) instead of costing
  // a line of its own between the search field and the photos.
  const status = (
    <div
      className={`kukatko-filter-status d-flex align-items-center justify-content-between gap-2 ${
        narrow ? 'w-100 flex-row-reverse' : 'mt-2'
      }`}
    >
      {/* The live region stays mounted while the count is absent, so the number
          is announced when it arrives instead of a new region appearing. */}
      <span className="text-secondary small" aria-live="polite">
        {total !== undefined && t('library.count', { count: total })}
      </span>
      {clearVisible && (
        <Button
          type="button"
          size="sm"
          variant="link"
          className="text-decoration-none px-0"
          onClick={clearAll}
        >
          {t('library.filters.clear')}
        </Button>
      )}
    </div>
  )

  return (
    <Form className="kukatko-filter-bar" role="search" aria-label={t('library.filters.barLabel')}>
      <div className="d-flex flex-wrap align-items-center gap-2">
        {showSearch && (
          <>
            <InputGroup className="kukatko-filter-search">
              <InputGroup.Text aria-hidden="true">
                <SearchIcon />
              </InputGroup.Text>
              <Form.Control
                type="search"
                size="lg"
                value={view.q}
                aria-label={t('library.filters.search')}
                placeholder={t('library.filters.searchPlaceholder')}
                onChange={(e) => {
                  replace({ q: e.target.value })
                }}
              />
            </InputGroup>
            {/* The very same help the search page opens: this field speaks the
                same query language, so it gets the same one explanation rather
                than a second, drifting copy of it. */}
            <SearchQueryHelp />
          </>
        )}

        {/* Sort and density are display preferences, used far less often than
            the search field beside them. On a phone they are the difference
            between a one-row header and a three-row one, so there they live in
            the drawer (see `panel`). */}
        {!narrow && showSort && (
          <SortSelect
            className="kukatko-filter-sort w-auto"
            size="lg"
            value={view.sort}
            options={sortOptions}
            onChange={(sort) => {
              push({ sort })
            }}
          />
        )}

        {!narrow && showDensity && <GridDensityControl />}

        <Button
          type="button"
          size="lg"
          variant={open || chips.length > 0 ? 'primary' : 'outline-primary'}
          className="d-inline-flex align-items-center gap-2"
          aria-expanded={open}
          aria-controls={PANEL_ID}
          onClick={() => {
            setOpen((prev) => !prev)
          }}
        >
          <FunnelIcon />
          {t('library.filters.toggle')}
          {chips.length > 0 && (
            <Badge bg="light" text="dark" pill>
              {chips.length}
            </Badge>
          )}
        </Button>

        {/* On a phone the count wraps onto its own line right under the Filters
            button. It is the one number the reader wants from this bar, and it
            now costs no line of its own. */}
        {narrow && status}
      </div>

      {/* The note sits below the alignment row, not inside the search field's flex
          item: kept a sibling of that item it would stretch the search column and
          the row's centre alignment would push the sort selector down. On a phone
          it is two lines of reference material above the photos, so there it
          moves into the drawer (see `panel`) — the placeholder and the `?` beside
          the field carry the same message at no cost in height. */}
      {!narrow && searchNote}

      {/* Desktop keeps the primary pickers in a persistent row; on a phone they
          move into the filters drawer (see `panel` above) so the photos start
          near the top of the screen instead of below four stacked selects. */}
      {!narrow && (
        <PrimaryFilterRow view={view} facets={facets} push={push} queryFilters={queryFilters} />
      )}

      {chips.length > 0 && (
        <div className="d-flex flex-wrap align-items-center gap-2 mt-2">
          {chips.map((chip) => {
            // Album and tag chips carry a distinct hue + leading icon from the
            // shared entity convention; every other filter keeps the neutral
            // primary chip so colour stays reserved for "which entity is this".
            const entity = chip.kind === undefined ? undefined : ENTITY_STYLE[chip.kind]
            // Named once, worn twice: the accessible name and the hover hint.
            const removeLabel = t('library.filters.removeFilter', { name: chip.label })
            return (
              <span
                key={chip.key}
                className={`kukatko-filter-chip badge rounded-pill ${
                  entity === undefined ? 'text-bg-primary' : entity.className
                }`}
              >
                {entity !== undefined && <Icon name={entity.icon} className="me-1" />}
                {chip.label}
                <button
                  type="button"
                  className="btn-close btn-close-white ms-2"
                  aria-label={removeLabel}
                  title={removeLabel}
                  onClick={() => {
                    push(chip.clear)
                  }}
                />
              </span>
            )
          })}
        </div>
      )}

      {!narrow && status}

      {narrow ? (
        <Offcanvas
          show={open}
          onHide={() => {
            setOpen(false)
          }}
          placement="end"
          aria-label={t('library.filters.toggle')}
        >
          <Offcanvas.Header closeButton>
            <Offcanvas.Title>{t('library.filters.toggle')}</Offcanvas.Title>
          </Offcanvas.Header>
          <Offcanvas.Body id={PANEL_ID}>{panel}</Offcanvas.Body>
          {/* A sibling of the body, not a child of it: `.offcanvas` is a flex
              column whose body grows and scrolls, so the footer is pinned to the
              bottom edge *and* subtracted from the scroll area — the last field
              scrolls above it rather than under it. */}
          <FilterDrawerFooter
            total={total}
            clearVisible={clearVisible}
            onClear={clearAll}
            onClose={() => {
              setOpen(false)
            }}
          />
        </Offcanvas>
      ) : (
        <Collapse in={open}>
          <div id={PANEL_ID}>
            <div className="card card-body bg-body-tertiary mt-2">{panel}</div>
          </div>
        </Collapse>
      )}
    </Form>
  )
}

/**
 * The note under the search field: what the box really does — a substring match
 * over title, description and notes *and* the full `key:value` query language,
 * which works here exactly as it does on `/search`; the `?` beside the field
 * spells that language out in full. The link to ranked full-text/semantic search
 * only appears when semantic search is actually available (the embeddings box is
 * reachable), since the link's own label promises semantics.
 *
 * Defined once and placed twice: under the header row on desktop, at the foot of
 * the filters drawer on a phone, where two lines of reference material would
 * otherwise stand between the search field and the photographs.
 */
function SearchNote({ href, semanticSearch }: { href: string; semanticSearch: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="form-text mt-1">
      {t('library.filters.searchHint')}{' '}
      {semanticSearch && (
        <Link to={href} className="text-decoration-none">
          {t('library.filters.fullSearchLink')}
        </Link>
      )}
    </div>
  )
}

/**
 * Every sort the library offers, in the order the selector lists them, each with
 * the key naming it. One list, so a page offering fewer of them picks by value
 * and never restates a label.
 */
const SORT_OPTIONS = [
  { value: 'newest', label: 'library.sort.newest' },
  { value: 'oldest', label: 'library.sort.oldest' },
  { value: 'added', label: 'library.sort.added' },
  { value: 'title', label: 'library.sort.title' },
  { value: 'size', label: 'library.sort.size' },
  { value: 'rating', label: 'library.sort.rating' },
] as const

/**
 * The sort selector, defined once for both of the places it is shown: the
 * desktop header row (unlabelled, `aria-label` only, sized to the row) and the
 * phone drawer (under a visible label, full width). Only the presentation
 * differs — the option list must not.
 *
 * `options` narrows that list for a grid whose sort key is not the reader's to
 * choose (an album, pinned to capture time server-side); omitted, it is every
 * sort the library has.
 */
function SortSelect({
  id,
  className,
  size,
  value,
  options,
  onChange,
}: {
  id?: string
  className?: string
  size?: 'sm' | 'lg'
  value: string
  options?: readonly string[]
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  // The caller's order, not the library's: an album rests at oldest-first, so
  // that is the option it lists first.
  const offered =
    options === undefined
      ? SORT_OPTIONS
      : options.flatMap((value) => SORT_OPTIONS.filter((o) => o.value === value))
  return (
    <Form.Select
      id={id}
      className={className}
      size={size}
      value={value}
      aria-label={t('library.filters.sort')}
      onChange={(e) => {
        onChange(e.target.value)
      }}
    >
      {offered.map((sort) => (
        <option key={sort.value} value={sort.value}>
          {t(sort.label)}
        </option>
      ))}
    </Form.Select>
  )
}

/**
 * The two display controls the phone header cannot afford: the sort order and
 * the grid density. They are not filters — nothing here narrows the result —
 * but they are what the header used to spend two of its three rows on, and the
 * drawer is the one surface with room to spare. Each keeps the label it wears
 * elsewhere, so the reader recognises the control they lost from the bar rather
 * than meeting a new one.
 *
 * Rendered only on a narrow viewport, so neither control ever exists twice in
 * the document.
 */
function DisplayControls({
  view,
  push,
  showSort,
  sortOptions,
  showDensity,
}: {
  view: LibraryView
  push: (patch: Partial<LibraryView>) => void
  showSort: boolean
  sortOptions: readonly string[] | undefined
  showDensity: boolean
}) {
  const { t } = useTranslation()
  return (
    <Row className="kukatko-filter-display g-3">
      {showSort && (
        <Col xs={12}>
          <Form.Label className="small mb-1" htmlFor="library-sort">
            {t('library.filters.sort')}
          </Form.Label>
          <SortSelect
            id="library-sort"
            value={view.sort}
            options={sortOptions}
            onChange={(sort) => {
              push({ sort })
            }}
          />
        </Col>
      )}
      {showDensity && (
        <Col xs={12}>
          {/* A plain caption, not a `<label>`: the stepper is a button group,
              which nothing can be labelled *for*. It names itself to assistive
              technology through the group's own `aria-label`. */}
          <div className="small mb-1">{t('library.density.label')}</div>
          <GridDensityControl />
        </Col>
      )}
    </Row>
  )
}

/**
 * What the drawer's primary button says. The live result count when there is
 * one; at zero the empty-set wording instead of "Show 0 photos", which reads
 * like a broken promise; and, when the page has no result set to count at all
 * (`total` undefined — the search page before a query is typed), a plain "close"
 * rather than a fabricated number.
 */
function applyLabel(t: TFunction, total: number | undefined): string {
  if (total === undefined) {
    return t('library.filters.applyClose')
  }
  if (total === 0) {
    return t('library.filters.applyEmpty')
  }
  return t('library.filters.apply', { count: total })
}

/**
 * The phone drawer's sticky footer: the way out, with the answer already on it.
 *
 * On a phone the filters take over the screen, so the "Photos: N" line the bar
 * keeps beside the grid sits *under* the drawer, invisible. Without this footer
 * the reader sets a year, a person and a rating, scrolls ten fields back up to
 * the only exit — the cross in the header — and only then learns the combination
 * matches nothing. The primary button carries that number instead and closes the
 * drawer, so the count is read before the trip back and the trip is one tap from
 * wherever the scrolling stopped. It needs no state of its own: `total` is the
 * same prop the bar already states, so the number follows every filter change
 * live, with the drawer still open.
 *
 * It stays usable at zero — an empty result is exactly when the reader most
 * needs a way out — so the button only changes its wording, never its ability to
 * close.
 *
 * "Clear filters" repeats the bar's own action (and, like it, appears only when
 * there is something to clear) and deliberately does *not* close: clearing is
 * how the reader recovers from an empty set, and the count on the button beside
 * it updates in place to show the recovery worked.
 */
function FilterDrawerFooter({
  total,
  clearVisible,
  onClear,
  onClose,
}: {
  total: number | undefined
  clearVisible: boolean
  onClear: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="offcanvas-footer kukatko-filter-footer">
      <Button type="button" size="lg" variant="primary" className="w-100" onClick={onClose}>
        {applyLabel(t, total)}
      </Button>
      {clearVisible && (
        <Button
          type="button"
          size="lg"
          variant="outline-secondary"
          className="w-100"
          onClick={onClear}
        >
          {t('library.filters.clear')}
        </Button>
      )}
    </div>
  )
}

/**
 * The primary filter row: the ways photos are actually found. The period they
 * were taken in — always offered, because every grid in the app can be narrowed
 * in time — and, when the page supplies `facets`, the albums, labels and people
 * (subjects) a photo belongs to or contains. Album, label and person are
 * type-to-filter selects because all three collections grow without bound;
 * the period is {@link PeriodFilter}, a decade list over the years the catalog
 * holds with its exact-date fields underneath.
 *
 * Album, label and person are multi-select: each pick *adds* to the current set
 * (combined with AND — a photo must be in every chosen album, carry every chosen
 * label and contain every chosen person), and the already-chosen ones show as
 * removable chips below. The select therefore never displays a "current" value —
 * it is a pure add-picker resting on its "any" placeholder — and it drops the
 * already-selected entries from its options so the same entry cannot be added
 * twice.
 *
 * All four push a history entry, so Back steps back through the choices.
 *
 * A picker is not the only way to set its filter: `year:1960-1969` or
 * `person:Jarmila` typed into the search box filters the grid just as hard, and
 * the picker knows nothing about it. Rather than let a control read "any period"
 * over a grid holding only the sixties, each one whose key appears in the query
 * says so underneath itself, quoting the tokens responsible
 * ({@link queryFilterTokens}) — and the period control goes further and *shows*
 * the period the query sets ({@link periodFromQuery}), so the two can never
 * disagree. The pickers keep working: adding a filter on top of the query narrows
 * further, as ANDed filters do everywhere else.
 */
function PrimaryFilterRow({
  view,
  facets,
  push,
  queryFilters,
}: {
  view: LibraryView
  facets: LibraryFacets | undefined
  push: (patch: Partial<LibraryView>) => void
  queryFilters: ReadonlyMap<string, string[]>
}) {
  const selectedAlbums = parseFilterList(view.album)
  const selectedLabels = parseFilterList(view.label)
  const selectedPeople = parseFilterList(view.person)
  const fromQuery = {
    period: facetQueryTokens(queryFilters, FACET_QUERY_KEYS.period),
    album: facetQueryTokens(queryFilters, FACET_QUERY_KEYS.album),
    label: facetQueryTokens(queryFilters, FACET_QUERY_KEYS.label),
    person: facetQueryTokens(queryFilters, FACET_QUERY_KEYS.person),
  }
  return (
    <Row className="kukatko-filter-facets g-2 mt-1">
      <Col xs={12} md={6} lg={3}>
        <PeriodFilter
          id="library-period"
          value={periodOf(view)}
          years={facets?.years ?? []}
          queryPeriod={periodFromQuery(queryFilters)}
          describedBy={fromQuery.period === '' ? undefined : 'library-period-from-query'}
          onChange={(period) => {
            push(periodPatch(period))
          }}
        />
        <QueryOverrideNote id="library-period-from-query" tokens={fromQuery.period} />
      </Col>

      {facets !== undefined && (
        <FacetPickers
          view={view}
          facets={facets}
          push={push}
          fromQuery={fromQuery}
          selected={{ albums: selectedAlbums, labels: selectedLabels, people: selectedPeople }}
        />
      )}
    </Row>
  )
}

/**
 * The album / label / person columns of {@link PrimaryFilterRow}, split out so
 * the row itself stays readable and the pages that scope their grid to one album
 * or place simply do not render them.
 */
function FacetPickers({
  view,
  facets,
  push,
  fromQuery,
  selected,
}: {
  view: LibraryView
  facets: LibraryFacets
  push: (patch: Partial<LibraryView>) => void
  fromQuery: { album: string; label: string; person: string }
  selected: { albums: string[]; labels: string[]; people: string[] }
}) {
  const { t } = useTranslation()
  const selectedAlbums = selected.albums
  const selectedLabels = selected.labels
  const selectedPeople = selected.people
  return (
    <>
      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-album"
          label={t('library.filters.album')}
          anyLabel={
            fromQuery.album === ''
              ? t('library.filters.anyAlbum')
              : t('library.filters.setByQueryOption')
          }
          describedBy={fromQuery.album === '' ? undefined : 'library-album-from-query'}
          value=""
          options={facets.albums
            .filter((album) => !selectedAlbums.includes(album.uid))
            .map((album) => ({
              value: album.uid,
              label: album.title,
              count: album.photo_count,
            }))}
          onChange={(value) => {
            push({ album: addToFilterList(view.album, value) })
          }}
        />
        <QueryOverrideNote id="library-album-from-query" tokens={fromQuery.album} />
      </Col>

      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-label"
          label={t('library.filters.label')}
          anyLabel={
            fromQuery.label === ''
              ? t('library.filters.anyLabel')
              : t('library.filters.setByQueryOption')
          }
          describedBy={fromQuery.label === '' ? undefined : 'library-label-from-query'}
          value=""
          options={facets.labels
            .filter((label) => !selectedLabels.includes(label.uid))
            .map((label) => ({
              value: label.uid,
              label: label.name,
              count: label.photo_count,
            }))}
          onChange={(value) => {
            push({ label: addToFilterList(view.label, value) })
          }}
        />
        <QueryOverrideNote id="library-label-from-query" tokens={fromQuery.label} />
      </Col>

      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-person"
          label={t('library.filters.person')}
          anyLabel={
            fromQuery.person === ''
              ? t('library.filters.anyPerson')
              : t('library.filters.setByQueryOption')
          }
          describedBy={fromQuery.person === '' ? undefined : 'library-person-from-query'}
          value=""
          options={facets.subjects
            .filter((subject) => !selectedPeople.includes(subject.uid))
            .map((subject) => ({
              value: subject.uid,
              label: subject.name,
              // A photo count, like the album and label options above it: picking
              // a person filters the library to photos, and one photo can carry
              // several of that person's faces.
              count: subject.photo_count,
            }))}
          onChange={(value) => {
            push({ person: addToFilterList(view.person, value) })
          }}
        />
        <QueryOverrideNote id="library-person-from-query" tokens={fromQuery.person} />
      </Col>
    </>
  )
}

/**
 * The note under a facet picker whose facet the search query already sets,
 * quoting the tokens verbatim so the reader can find them in the box and edit
 * them. Renders nothing when `tokens` is empty, so callers need no condition of
 * their own.
 */
function QueryOverrideNote({ id, tokens }: { id: string; tokens: string }) {
  const { t } = useTranslation()
  if (tokens === '') {
    return null
  }
  return (
    <div id={id} className="form-text mt-1">
      <Icon name="info-circle" className="me-1" />
      {t('library.filters.setByQuery')} <code>{tokens}</code>
    </div>
  )
}

/**
 * The advanced-filter controls, shared by the desktop collapse and mobile
 * offcanvas. The capture-date pair that used to open this panel is gone from
 * here: it was the second, hidden half of a time filter whose visible half could
 * not express a decade, and both are now {@link PeriodFilter} in the primary row.
 */
function AdvancedFilters({
  view,
  push,
  replace,
  showFavorite,
  uploaders,
  queryFilters,
}: {
  view: LibraryView
  push: (patch: Partial<LibraryView>) => void
  replace: (patch: Partial<LibraryView>) => void
  showFavorite: boolean
  uploaders: readonly UploaderBucket[] | undefined
  queryFilters: ReadonlyMap<string, string[]>
}) {
  const { t } = useTranslation()
  return (
    <Row className="kukatko-filter-panel g-3">
      <Col xs={12} sm={6} lg={3}>
        <Form.Group controlId="library-archived">
          <Form.Label className="small mb-1">{t('library.filters.archived')}</Form.Label>
          <Form.Select
            value={view.archived}
            onChange={(e) => {
              push({ archived: e.target.value })
            }}
          >
            <option value="false">{t('library.archived.hide')}</option>
            <option value="true">{t('library.archived.show')}</option>
            <option value="only">{t('library.archived.only')}</option>
          </Form.Select>
        </Form.Group>
      </Col>

      {showFavorite && (
        <Col xs={6} sm={6} lg={3}>
          <Form.Group controlId="library-favorite">
            <Form.Label className="small mb-1">{t('library.filters.favorite')}</Form.Label>
            {/* A two-state filter, not a tri-state: the backend only scopes on
                "favorites only", so there is no meaningful "not favorited" value.
                Presented as a select to line up with the archived/GPS
                controls beside it. */}
            <Form.Select
              value={view.favorite}
              onChange={(e) => {
                push({ favorite: e.target.value })
              }}
            >
              <option value="">{t('library.triState.any')}</option>
              <option value="true">{t('library.favorite.only')}</option>
            </Form.Select>
          </Form.Group>
        </Col>
      )}

      <Col xs={6} sm={6} lg={3}>
        <TriStateSelect
          id="library-has-gps"
          label={t('library.filters.hasGps')}
          value={view.has_gps}
          onChange={(value) => {
            push({ has_gps: value })
          }}
        />
      </Col>

      <Col xs={6} sm={6} lg={3}>
        <Form.Group controlId="library-min-rating">
          <Form.Label className="small mb-1">{t('library.filters.minRating')}</Form.Label>
          <Form.Select
            value={view.min_rating}
            onChange={(e) => {
              push({ min_rating: e.target.value })
            }}
          >
            <option value="">{t('library.minRating.any')}</option>
            {[1, 2, 3, 4, 5].map((n) => (
              <option key={n} value={String(n)}>
                {t('library.minRating.atLeast', { n })}
              </option>
            ))}
          </Form.Select>
        </Form.Group>
      </Col>

      <Col xs={6} sm={6} lg={3}>
        <Form.Group controlId="library-flag">
          <Form.Label className="small mb-1">{t('library.filters.flag')}</Form.Label>
          <Form.Select
            value={view.flag}
            onChange={(e) => {
              push({ flag: e.target.value })
            }}
          >
            <option value="">{t('library.flag.any')}</option>
            <option value="pick">{t('library.flag.picks')}</option>
            <option value="reject">{t('library.flag.rejects')}</option>
            <option value="eye">{t('library.flag.eyes')}</option>
          </Form.Select>
        </Form.Group>
      </Col>

      {uploaders !== undefined && uploaders.length > 0 && (
        <Col xs={12} sm={6} lg={3}>
          <UploaderSelect
            value={view.uploader}
            uploaders={uploaders}
            fromQuery={facetQueryTokens(queryFilters, FACET_QUERY_KEYS.uploader)}
            onChange={(uploader) => {
              push({ uploader })
            }}
          />
        </Col>
      )}

      <Col xs={12} sm={6}>
        <Form.Group controlId="library-camera">
          <Form.Label className="small mb-1">{t('library.filters.camera')}</Form.Label>
          <Form.Control
            type="text"
            value={view.camera}
            placeholder={t('library.filters.cameraPlaceholder')}
            onChange={(e) => {
              replace({ camera: e.target.value })
            }}
          />
        </Form.Group>
      </Col>
    </Row>
  )
}

/**
 * Who uploaded the photos: "anyone" at rest, then one option per contributor to
 * the current view — named, with their count, largest contribution first — and
 * the imported photos as a group of their own where the facet reports them.
 *
 * The options come from the facet rather than from the account list, so after an
 * event the control offers that event's contributors instead of every account on
 * the instance, and each count is what picking it will actually show. It is a
 * single choice, not a multi-select like the album/label/person pickers: a photo
 * has exactly one uploader, so two of them could only ever match nothing.
 *
 * Like the pickers in the primary row it admits when the search box has already
 * taken the filter over (`uploader:tomas` typed into the query), rather than
 * resting on "anyone" over a grid that holds one person's photos.
 */
function UploaderSelect({
  value,
  uploaders,
  fromQuery,
  onChange,
}: {
  value: string
  uploaders: readonly UploaderBucket[]
  fromQuery: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  // A picked uploader who contributes nothing to the *rest* of the view — every
  // photo of theirs excluded by another filter — is not in the facet, and a
  // select whose value matches no option would silently read "anyone" over a
  // filtered grid. They are offered at zero instead: the count is the honest
  // answer, and the control keeps saying which filter is on.
  const offered = uploaders.some((uploader) => uploaderValue(uploader.uid) === value)
    ? uploaders
    : [...uploaders, { uid: value === UPLOADER_NONE ? '' : value, name: '', count: 0 }]
  return (
    <Form.Group controlId="library-uploader">
      <Form.Label className="small mb-1">{t('library.filters.uploader')}</Form.Label>
      <Form.Select
        value={value}
        aria-describedby={fromQuery === '' ? undefined : 'library-uploader-from-query'}
        onChange={(e) => {
          onChange(e.target.value)
        }}
      >
        <option value="">
          {fromQuery === ''
            ? t('library.filters.anyUploader')
            : t('library.filters.setByQueryOption')}
        </option>
        {(value === '' ? uploaders : offered).map((uploader) => (
          <option key={uploaderValue(uploader.uid)} value={uploaderValue(uploader.uid)}>
            {/* The imported group has no name of its own — only the reader's
                language has one for it; an uploader the facet could not name
                falls back to their uid rather than to a blank row. */}
            {uploader.uid === ''
              ? t('library.filters.uploaderImported')
              : uploader.name || uploader.uid}{' '}
            ({uploader.count})
          </option>
        ))}
      </Form.Select>
      <QueryOverrideNote id="library-uploader-from-query" tokens={fromQuery} />
    </Form.Group>
  )
}

/**
 * The URL value one facet entry stands for: the uploader's uid, or the reserved
 * word for the group the facet reports without one (the imported photos).
 */
function uploaderValue(uid: string): string {
  return uid === '' ? UPLOADER_NONE : uid
}

/** A reusable any/yes/no select for tri-state boolean filters. */
function TriStateSelect({
  id,
  label,
  value,
  onChange,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Form.Group controlId={id}>
      <Form.Label className="small mb-1">{label}</Form.Label>
      <Form.Select
        value={value}
        onChange={(e) => {
          onChange(e.target.value)
        }}
      >
        <option value="">{t('library.triState.any')}</option>
        <option value="true">{t('library.triState.yes')}</option>
        <option value="false">{t('library.triState.no')}</option>
      </Form.Select>
    </Form.Group>
  )
}

/** A magnifier glyph (Bootstrap Icons "search") marking the search field. */
function SearchIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      fill="currentColor"
      viewBox="0 0 16 16"
      aria-hidden="true"
    >
      <path d="M11.742 10.344a6.5 6.5 0 1 0-1.397 1.398h-.001q.044.06.098.115l3.85 3.85a1 1 0 0 0 1.415-1.414l-3.85-3.85a1 1 0 0 0-.115-.1zM12 6.5a5.5 5.5 0 1 1-11 0 5.5 5.5 0 0 1 11 0" />
    </svg>
  )
}

/** A funnel glyph (Bootstrap Icons "funnel") marking the filters toggle. */
function FunnelIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      fill="currentColor"
      viewBox="0 0 16 16"
      aria-hidden="true"
    >
      <path d="M1.5 1.5A.5.5 0 0 1 2 1h12a.5.5 0 0 1 .5.5v2a.5.5 0 0 1-.128.334L10 8.692V13.5a.5.5 0 0 1-.342.474l-3 1A.5.5 0 0 1 6 14.5V8.692L1.628 3.834A.5.5 0 0 1 1.5 3.5z" />
    </svg>
  )
}
