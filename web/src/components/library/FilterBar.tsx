import { useState } from 'react'
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

import { useIsNarrowViewport } from '../../hooks/useIsNarrowViewport'
import { type LibraryFacets } from '../../hooks/useLibraryFacets'
import {
  addToFilterList,
  hasActiveFilters,
  type LibraryView,
  LIBRARY_DEFAULTS,
  parseFilterList,
} from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'

import { buildChips } from './filterChips'
import { GridDensityControl } from './GridDensityControl'
import { SearchableSelect } from './SearchableSelect'

/** DOM id of the collapsible / offcanvas advanced-filter panel. */
const PANEL_ID = 'library-filter-panel'

/** Props for {@link FilterBar}. */
export interface FilterBarProps<T extends LibraryView> {
  view: T
  onChange: SetUrlState<T>
  /** Total number of photos matching the current filters, shown as a count. */
  total: number
  /**
   * Whether to show the substring search input. The search page hides it
   * (`false`) because its prominent query box already owns `q`. Defaults true.
   */
  showSearch?: boolean
  /**
   * Whether to show the sort control. The search page hides it (`false`) because
   * results are ranked by relevance/similarity and the backend ignores sort in
   * search modes. Defaults true.
   */
  showSort?: boolean
  /**
   * Whether to show the grid-density picker. The trash hides it (`false`) because
   * its grid is a card list, not the photo grid the density governs. Defaults true.
   */
  showDensity?: boolean
  /**
   * The Year / Album / Label / Person facet option lists. Omit to hide the facet
   * row — pages whose grid is already scoped to one album, label or place have
   * nothing to offer there. Album titles, label names and subject names also let
   * the chips name a filter instead of showing its UID.
   */
  facets?: LibraryFacets
  /**
   * Whether to show the favorites toggle. Off by default: pages already scoped to
   * favorites (the Favorites page) would only offer a redundant, conflicting
   * control. The library opts in so "favorites + album + year" can be combined in
   * the main grid.
   */
  showFavorite?: boolean
  /**
   * Where the "search the full text instead" link points, carrying the current
   * view. Omit on pages that are not the library (and on `/search` itself, which
   * is the destination), and no link is rendered.
   */
  searchHref?: string
}

/**
 * Library filter + sort controls, built for a calm default and progressive
 * disclosure. The header is a single row: a prominent quick-filter field (the
 * visual anchor, matching title and description as you type), the sort selector,
 * the grid-density picker (how many photos sit side by side — a per-device
 * display preference, not part of the view), and a "Filters" toggle badged with
 * the count of active filters. Below it sits
 * the facet row — Year, Album, Label, Person — the ways photos are actually found;
 * it appears only when the page supplies `facets`. The remaining filters (date
 * range, camera, archived, favorites, location, private, min rating, flag) live in
 * a collapsible panel on desktop and an offcanvas drawer on phones, so the resting
 * state stays uncluttered — the favorites toggle only when the page opts in via
 * `showFavorite`. Every active filter is echoed as a removable chip plus a single
 * clear-all action.
 *
 * The quick filter is a substring match, not a search: `searchHref` puts a
 * labelled link to `/search` beside it, which is where full-text and semantic
 * search live. The two are never duplicated here.
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
  showDensity = true,
  facets,
  showFavorite = false,
  searchHref,
}: FilterBarProps<T>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const narrow = useIsNarrowViewport()

  const push = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>)
  }
  const replace = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>, { replace: true })
  }

  const chips = buildChips(view, t, { facets })
  const clearVisible = hasActiveFilters(view, { ignoreQuery: !showSearch })

  const clearAll = () => {
    // Keep the current sort, and keep the query when it is owned by the page's
    // own search box (search page) rather than this bar.
    push({ ...LIBRARY_DEFAULTS, sort: view.sort, ...(showSearch ? {} : { q: view.q }) })
  }

  const panel = (
    <AdvancedFilters view={view} push={push} replace={replace} showFavorite={showFavorite} />
  )

  return (
    <Form className="mb-3" role="search" aria-label={t('library.filters.barLabel')}>
      <div className="d-flex flex-wrap align-items-center gap-2">
        {showSearch && (
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
        )}

        {showSort && (
          <Form.Select
            className="kukatko-filter-sort w-auto"
            size="lg"
            value={view.sort}
            aria-label={t('library.filters.sort')}
            onChange={(e) => {
              push({ sort: e.target.value })
            }}
          >
            <option value="newest">{t('library.sort.newest')}</option>
            <option value="oldest">{t('library.sort.oldest')}</option>
            <option value="added">{t('library.sort.added')}</option>
            <option value="title">{t('library.sort.title')}</option>
            <option value="size">{t('library.sort.size')}</option>
            <option value="rating">{t('library.sort.rating')}</option>
          </Form.Select>
        )}

        {showDensity && <GridDensityControl />}

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
      </div>

      {/* The hint sits below the alignment row, not inside the search field's flex
          item: kept a sibling of that item it would stretch the search column and
          the row's centre alignment would push the sort selector down. */}
      {showSearch && searchHref !== undefined && (
        <div className="form-text mt-1">
          {t('library.filters.searchHint')}{' '}
          <Link to={searchHref} className="text-decoration-none">
            {t('library.filters.fullSearchLink')}
          </Link>
        </div>
      )}

      {facets && <FacetRow view={view} facets={facets} push={push} />}

      {chips.length > 0 && (
        <div className="d-flex flex-wrap align-items-center gap-2 mt-2">
          {chips.map((chip) => {
            // Album and tag chips carry a distinct hue + leading icon from the
            // shared entity convention; every other filter keeps the neutral
            // primary chip so colour stays reserved for "which entity is this".
            const entity = chip.kind === undefined ? undefined : ENTITY_STYLE[chip.kind]
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
                  aria-label={t('library.filters.removeFilter', { name: chip.label })}
                  onClick={() => {
                    push(chip.clear)
                  }}
                />
              </span>
            )
          })}
        </div>
      )}

      <div className="d-flex align-items-center justify-content-between mt-2">
        <span className="text-secondary small" aria-live="polite">
          {t('library.count', { count: total })}
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
 * The four facets photos are actually found by: the years present in the catalog
 * (each with its count, so the reader sees how much a year holds before
 * committing), and the albums, labels and people (subjects) the photo belongs to
 * or contains. Album, label and person are type-to-filter selects because all
 * three collections grow without bound; year is a plain select because the catalog
 * only ever holds a handful of years.
 *
 * Album, label and person are multi-select: each pick *adds* to the current set
 * (combined with AND — a photo must be in every chosen album, carry every chosen
 * label and contain every chosen person), and the already-chosen ones show as
 * removable chips below. The select therefore never displays a "current" value —
 * it is a pure add-picker resting on its "any" placeholder — and it drops the
 * already-selected entries from its options so the same entry cannot be added
 * twice.
 *
 * All four push a history entry, so Back steps back through facet choices.
 */
function FacetRow({
  view,
  facets,
  push,
}: {
  view: LibraryView
  facets: LibraryFacets
  push: (patch: Partial<LibraryView>) => void
}) {
  const { t } = useTranslation()
  const selectedAlbums = parseFilterList(view.album)
  const selectedLabels = parseFilterList(view.label)
  const selectedPeople = parseFilterList(view.person)
  return (
    <Row className="kukatko-filter-facets g-2 mt-1">
      <Col xs={12} md={6} lg={3}>
        <Form.Group controlId="library-year">
          <Form.Label className="small mb-1">{t('library.filters.year')}</Form.Label>
          <Form.Select
            value={view.year}
            onChange={(e) => {
              push({ year: e.target.value })
            }}
          >
            <option value="">{t('library.filters.anyYear')}</option>
            {facets.years.map((bucket) => (
              <option key={bucket.year} value={String(bucket.year)}>
                {t('library.filters.yearOption', { year: bucket.year, n: bucket.count })}
              </option>
            ))}
          </Form.Select>
        </Form.Group>
      </Col>

      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-album"
          label={t('library.filters.album')}
          anyLabel={t('library.filters.anyAlbum')}
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
      </Col>

      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-label"
          label={t('library.filters.label')}
          anyLabel={t('library.filters.anyLabel')}
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
      </Col>

      <Col xs={12} md={6} lg={3}>
        <SearchableSelect
          id="library-person"
          label={t('library.filters.person')}
          anyLabel={t('library.filters.anyPerson')}
          value=""
          options={facets.subjects
            .filter((subject) => !selectedPeople.includes(subject.uid))
            .map((subject) => ({
              value: subject.uid,
              label: subject.name,
              count: subject.marker_count,
            }))}
          onChange={(value) => {
            push({ person: addToFilterList(view.person, value) })
          }}
        />
      </Col>
    </Row>
  )
}

/** The advanced-filter controls, shared by the desktop collapse and mobile offcanvas. */
function AdvancedFilters({
  view,
  push,
  replace,
  showFavorite,
}: {
  view: LibraryView
  push: (patch: Partial<LibraryView>) => void
  replace: (patch: Partial<LibraryView>) => void
  showFavorite: boolean
}) {
  const { t } = useTranslation()
  return (
    <Row className="kukatko-filter-panel g-3">
      <Col xs={12} lg={6}>
        <fieldset className="mb-0">
          {/* Group the two date inputs for assistive tech, but keep the group
              name off-screen: a visible legend is a whole extra label row the
              single-input sibling columns lack, which drops the date inputs
              below their neighbours and misaligns the grid. The per-input
              labels below carry the visible text in the same `.small mb-1`
              style every other column uses, so all headings and inputs share a
              baseline while the fieldset still exposes "Date taken" to a11y. */}
          <legend className="visually-hidden">{t('library.filters.dateRange')}</legend>
          <Row className="g-2">
            <Col xs={6}>
              <Form.Group controlId="library-taken-after">
                <Form.Label className="small mb-1">{t('library.filters.takenAfter')}</Form.Label>
                <Form.Control
                  type="date"
                  value={view.taken_after}
                  onChange={(e) => {
                    push({ taken_after: e.target.value })
                  }}
                />
              </Form.Group>
            </Col>
            <Col xs={6}>
              <Form.Group controlId="library-taken-before">
                <Form.Label className="small mb-1">{t('library.filters.takenBefore')}</Form.Label>
                <Form.Control
                  type="date"
                  value={view.taken_before}
                  onChange={(e) => {
                    push({ taken_before: e.target.value })
                  }}
                />
              </Form.Group>
            </Col>
          </Row>
        </fieldset>
      </Col>

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
                Presented as a select to line up with the archived/GPS/private
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
        <TriStateSelect
          id="library-private"
          label={t('library.filters.private')}
          value={view.private}
          onChange={(value) => {
            push({ private: value })
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
