import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { hasActiveFilters, type LibraryView, LIBRARY_DEFAULTS } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'

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
 * Library filter + sort controls. Sort, the tri-state and date filters push a
 * history entry (so Back steps through views), while the free-text inputs
 * replace it (so live typing does not flood history). All state lives in the URL
 * via `onChange`; the bar is fully controlled by `view`. Generic over the view
 * type so it serves both the library ({@link LibraryView}) and the search page
 * (a superset adding `mode`); only the library fields are ever written here, so
 * any extra fields (e.g. the search mode) are preserved untouched.
 */
export function FilterBar<T extends LibraryView>({
  view,
  onChange,
  total,
  showSearch = true,
  showSort = true,
}: FilterBarProps<T>) {
  const { t } = useTranslation()

  const push = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>)
  }
  const replace = (patch: Partial<LibraryView>) => {
    onChange(patch as Partial<T>, { replace: true })
  }

  return (
    <Form className="mb-3" role="search" aria-label={t('library.filters.label')}>
      <Row className="g-2 align-items-end">
        {showSearch && (
          <Col xs={12} md={4} lg={3}>
            <Form.Group controlId="library-search">
              <Form.Label className="small mb-1">{t('library.filters.search')}</Form.Label>
              <Form.Control
                type="search"
                value={view.q}
                placeholder={t('library.filters.searchPlaceholder')}
                onChange={(e) => {
                  replace({ q: e.target.value })
                }}
              />
            </Form.Group>
          </Col>
        )}

        {showSort && (
          <Col xs={6} md={3} lg={2}>
            <Form.Group controlId="library-sort">
              <Form.Label className="small mb-1">{t('library.filters.sort')}</Form.Label>
              <Form.Select
                value={view.sort}
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
            </Form.Group>
          </Col>
        )}

        <Col xs={6} md={3} lg={2}>
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

        <Col xs={6} md={3} lg={2}>
          <TriStateSelect
            id="library-has-gps"
            label={t('library.filters.hasGps')}
            value={view.has_gps}
            onChange={(value) => {
              push({ has_gps: value })
            }}
          />
        </Col>

        <Col xs={6} md={3} lg={2}>
          <TriStateSelect
            id="library-private"
            label={t('library.filters.private')}
            value={view.private}
            onChange={(value) => {
              push({ private: value })
            }}
          />
        </Col>

        <Col xs={6} md={3} lg={2}>
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

        <Col xs={6} md={3} lg={2}>
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

        <Col xs={12} sm={6} md={4} lg={3}>
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

        <Col xs={6} sm={3} md={2}>
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

        <Col xs={6} sm={3} md={2}>
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

      <div className="d-flex align-items-center justify-content-between mt-2">
        <span className="text-secondary small" aria-live="polite">
          {t('library.count', { count: total })}
        </span>
        {hasActiveFilters(view, { ignoreQuery: !showSearch }) && (
          <Button
            type="button"
            size="sm"
            variant="outline-secondary"
            onClick={() => {
              // Keep the current sort, and keep the query when it is owned by the
              // page's own search box (search page) rather than this bar.
              push({ ...LIBRARY_DEFAULTS, sort: view.sort, ...(showSearch ? {} : { q: view.q }) })
            }}
          >
            {t('library.filters.clear')}
          </Button>
        )}
      </div>
    </Form>
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
