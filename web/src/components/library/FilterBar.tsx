import type { TFunction } from 'i18next'
import { useEffect, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Collapse from 'react-bootstrap/Collapse'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import Offcanvas from 'react-bootstrap/Offcanvas'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { hasActiveFilters, type LibraryView, LIBRARY_DEFAULTS } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'

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
}

/**
 * Library filter + sort controls, redesigned for a calm default and progressive
 * disclosure. The header is a single row: a prominent search field (the visual
 * anchor), the sort selector, and a "Filters" toggle badged with the count of
 * active advanced filters. The advanced filters (date range, camera, archived,
 * location, private, min rating, flag) live in a collapsible panel on desktop
 * and an offcanvas drawer on phones, so the resting state stays uncluttered.
 * Every active filter is echoed as a removable chip plus a single clear-all
 * action.
 *
 * Behaviour is unchanged: sort, the tri-state and date filters push a history
 * entry (so Back steps through views) while the free-text inputs replace it (so
 * live typing does not flood history). All state lives in the URL via
 * `onChange`; the bar is fully controlled by `view`. Generic over the view type
 * so it serves both the library ({@link LibraryView}) and the search page (a
 * superset adding `mode`); only the library fields are ever written here, so any
 * extra fields (e.g. the search mode) are preserved untouched.
 */
export function FilterBar<T extends LibraryView>({
  view,
  onChange,
  total,
  showSearch = true,
  showSort = true,
}: FilterBarProps<T>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const narrow = useIsNarrow()

  const push = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>)
  }
  const replace = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>, { replace: true })
  }

  const chips = buildChips(view, t)
  const clearVisible = hasActiveFilters(view, { ignoreQuery: !showSearch })

  const clearAll = () => {
    // Keep the current sort, and keep the query when it is owned by the page's
    // own search box (search page) rather than this bar.
    push({ ...LIBRARY_DEFAULTS, sort: view.sort, ...(showSearch ? {} : { q: view.q }) })
  }

  const panel = <AdvancedFilters view={view} push={push} replace={replace} />

  return (
    <Form className="mb-3" role="search" aria-label={t('library.filters.label')}>
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

      {chips.length > 0 && (
        <div className="d-flex flex-wrap align-items-center gap-2 mt-2">
          {chips.map((chip) => (
            <span key={chip.key} className="kukatko-filter-chip badge rounded-pill text-bg-primary">
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
          ))}
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

/** A single active-filter descriptor rendered as a removable chip. */
interface FilterChip {
  /** Stable key for React and the filter it represents. */
  key: string
  /** Human-readable "Field: value" summary shown on the chip. */
  label: string
  /** The patch that clears just this filter. */
  clear: Partial<LibraryView>
}

/**
 * Derives the removable chips for every active advanced filter. The free-text
 * query is intentionally excluded: it either has its own visible search box or
 * (on the search page) belongs to the page, so it never appears as a chip. The
 * returned length doubles as the "active filters" count on the toggle badge.
 */
function buildChips(view: LibraryView, t: TFunction): FilterChip[] {
  const chips: FilterChip[] = []
  const bool = (v: string) => t(v === 'true' ? 'library.triState.yes' : 'library.triState.no')

  if (view.archived !== LIBRARY_DEFAULTS.archived) {
    chips.push({
      key: 'archived',
      label: t(view.archived === 'only' ? 'library.archived.only' : 'library.archived.show'),
      clear: { archived: LIBRARY_DEFAULTS.archived },
    })
  }
  if (view.has_gps !== '') {
    chips.push({
      key: 'has_gps',
      label: `${t('library.filters.hasGps')}: ${bool(view.has_gps)}`,
      clear: { has_gps: '' },
    })
  }
  if (view.private !== '') {
    chips.push({
      key: 'private',
      label: `${t('library.filters.private')}: ${bool(view.private)}`,
      clear: { private: '' },
    })
  }
  if (view.camera !== '') {
    chips.push({
      key: 'camera',
      label: `${t('library.filters.camera')}: ${view.camera}`,
      clear: { camera: '' },
    })
  }
  if (view.taken_after !== '') {
    chips.push({
      key: 'taken_after',
      label: `${t('library.filters.takenAfter')}: ${view.taken_after}`,
      clear: { taken_after: '' },
    })
  }
  if (view.taken_before !== '') {
    chips.push({
      key: 'taken_before',
      label: `${t('library.filters.takenBefore')}: ${view.taken_before}`,
      clear: { taken_before: '' },
    })
  }
  if (view.min_rating !== '') {
    chips.push({
      key: 'min_rating',
      label: `${t('library.filters.minRating')}: ${t('library.minRating.atLeast', { n: view.min_rating })}`,
      clear: { min_rating: '' },
    })
  }
  if (view.flag !== '') {
    chips.push({
      key: 'flag',
      label: `${t('library.filters.flag')}: ${t(view.flag === 'pick' ? 'library.flag.picks' : 'library.flag.rejects')}`,
      clear: { flag: '' },
    })
  }
  return chips
}

/** The advanced-filter controls, shared by the desktop collapse and mobile offcanvas. */
function AdvancedFilters({
  view,
  push,
  replace,
}: {
  view: LibraryView
  push: (patch: Partial<LibraryView>) => void
  replace: (patch: Partial<LibraryView>) => void
}) {
  const { t } = useTranslation()
  return (
    <Row className="kukatko-filter-panel g-3">
      <Col xs={12} lg={6}>
        <fieldset className="mb-0">
          <legend className="col-form-label pt-0 fw-semibold fs-6">
            {t('library.filters.dateRange')}
          </legend>
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

/**
 * Tracks whether the viewport is phone-width (≤ 767.98px, Bootstrap's `md`
 * breakpoint) so the advanced filters open as an offcanvas drawer instead of an
 * inline collapse. Falls back to desktop when `matchMedia` is unavailable.
 */
function useIsNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => matchesNarrow())
  useEffect(() => {
    const mq = narrowQuery()
    if (!mq || typeof mq.addEventListener !== 'function') return
    const handler = (e: MediaQueryListEvent) => {
      setNarrow(e.matches)
    }
    mq.addEventListener('change', handler)
    setNarrow(mq.matches)
    return () => {
      mq.removeEventListener('change', handler)
    }
  }, [])
  return narrow
}

const NARROW_QUERY = '(max-width: 767.98px)'

/**
 * Resolves the narrow-viewport media query, or `null` when `matchMedia` is
 * unavailable — jsdom, for instance, exposes the function but returns `undefined`.
 */
function narrowQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  // The DOM types promise a MediaQueryList, but jsdom's stub returns undefined;
  // route through `unknown` + a guard so a broken environment yields null rather
  // than crashing on `.matches`.
  const result: unknown = window.matchMedia(NARROW_QUERY)
  return isMediaQueryList(result) ? result : null
}

/** Narrows an unknown value to a usable {@link MediaQueryList}. */
function isMediaQueryList(value: unknown): value is MediaQueryList {
  return typeof value === 'object' && value !== null && 'matches' in value
}

/** Reads the current narrow-viewport state, guarding against missing matchMedia. */
function matchesNarrow(): boolean {
  return narrowQuery()?.matches ?? false
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
