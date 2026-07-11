import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SelectionStart } from '../components/organize/SelectionStart'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { SavedSearchesDropdown } from '../components/savedsearch/SavedSearchesDropdown'
import { GlobalSearchSections } from '../components/search/GlobalSearchSections'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { usePhotoSearch } from '../hooks/usePhotoSearch'
import { useReloadKey } from '../hooks/useReloadKey'
import { detailQueryString } from '../lib/detailView'
import { viewToParams } from '../lib/libraryView'
import { SEARCH_DEFAULTS, type SearchView, toMode } from '../lib/searchView'
import { useUrlState } from '../lib/urlState'

/** Delay before a typed query is committed to the URL and a search runs. */
const SEARCH_DEBOUNCE_MS = 350

/**
 * The search page: a prominent query input and a mode selector over the same
 * virtualized, infinite-scroll grid the library uses, with the library filters
 * applicable. Query, mode and filters all live in the URL, so Back/Forward
 * restore the exact search and the URL is shareable. Typing is debounced before
 * it commits to the URL (and triggers a fetch). When a semantic/hybrid search
 * falls back to full-text because the embeddings sidecar is offline, a
 * non-blocking notice explains that semantic ranking was skipped.
 *
 * The results can be played as a slideshow, which replays the search itself (the
 * `mode` travels in the URL) rather than re-listing the library by the query.
 *
 * This page also owns saved searches: the header pairs a "save this view" button
 * with the {@link SavedSearchesDropdown} that lists, applies and manages them.
 *
 * Editors can enter selection mode over the results and bulk-edit the picked
 * photos; the search re-runs afterwards, since an edit can change what the query
 * and filters match.
 */
export function SearchPage() {
  const { t } = useTranslation()
  const [view, setView] = useUrlState<SearchView>(SEARCH_DEFAULTS)

  const params = useMemo(() => viewToParams(view), [view])
  const mode = toMode(view.mode)
  // Each tile carries the search scope — the query, filters and (always-present)
  // mode — so the detail page pages prev/next through the same ranked results and
  // Esc/Back returns to the search, not the library with `q` as a substring filter.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, favorite: '', mode }),
    [view, mode],
  )
  const [reloadKey, reload] = useReloadKey()
  const { photos, total, status, degraded, loadingMore, moreError, loadMore, retry } =
    usePhotoSearch(params, mode, { reloadKey })

  const bulk = useBulkEdit({ onEdited: reload })
  const selection = bulk.selection
  const hasResults = status === 'ready' && photos.length > 0

  // Local, debounced mirror of the URL query so typing stays responsive but the
  // URL (and the fetch) only update after the user pauses. The query is the
  // page's own input, separate from the filter bar.
  const [text, setText] = useState(view.q)
  const [savingView, setSavingView] = useState(false)

  // Keep the input in sync when the URL query changes from elsewhere (a saved
  // search, Back/Forward, a shared link).
  useEffect(() => {
    setText(view.q)
  }, [view.q])

  // A new query or mode is a different result set, and an empty query shows no
  // grid at all — a selection made against the old results has nowhere to live,
  // so leave selection mode with it. Filters, which merely narrow the same
  // search, keep the selection, as they do on the library.
  const leaveSelection = selection.disable
  useEffect(() => {
    leaveSelection()
  }, [view.q, mode, leaveSelection])

  // Debounce committing the typed query to the URL; an unchanged value is a no-op.
  useEffect(() => {
    if (text === view.q) {
      return
    }
    const id = setTimeout(() => {
      setView({ q: text }, { replace: true })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      clearTimeout(id)
    }
  }, [text, view.q, setView])

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('search.title')}</h1>
        {/* The search's own actions step aside while a selection is being made,
            as on the library: the selection bar below is then the only toolbar. */}
        {!selection.active && (
          <div className="d-flex align-items-center gap-2 flex-wrap">
            {hasResults && <SlideshowStart scope={{ mode }} view={view} count={total} />}
            {/* Saved searches live here rather than in the navbar: they are a
                search-page concern, and `/saved` stays reachable from the menu. */}
            <SavedSearchesDropdown />
            <Button
              variant="outline-secondary"
              size="sm"
              title={t('savedSearches.saveViewTitle')}
              onClick={() => {
                setSavingView(true)
              }}
            >
              {t('savedSearches.saveView')}
            </Button>
            {hasResults && <SelectionStart bulk={bulk} />}
          </div>
        )}
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <BulkEditControl bulk={bulk} />
        </SelectionBar>
      )}

      <Form
        role="search"
        aria-label={t('search.formLabel')}
        className="mb-3"
        onSubmit={(e) => {
          e.preventDefault()
          // Commit immediately on submit, bypassing the debounce.
          setView({ q: text }, { replace: true })
        }}
      >
        <Row className="g-2 align-items-end">
          <Col xs={12} md={8} lg={9}>
            <Form.Group controlId="search-query">
              <Form.Label className="small mb-1">{t('search.queryLabel')}</Form.Label>
              <Form.Control
                type="search"
                value={text}
                autoFocus
                placeholder={t('search.placeholder')}
                onChange={(e) => {
                  setText(e.target.value)
                }}
              />
            </Form.Group>
          </Col>
          <Col xs={12} md={4} lg={3}>
            <Form.Group controlId="search-mode">
              <Form.Label className="small mb-1">{t('search.modeLabel')}</Form.Label>
              <Form.Select
                value={view.mode}
                onChange={(e) => {
                  setView({ mode: e.target.value })
                }}
              >
                <option value="hybrid">{t('search.mode.hybrid')}</option>
                <option value="fulltext">{t('search.mode.fulltext')}</option>
                <option value="semantic">{t('search.mode.semantic')}</option>
              </Form.Select>
            </Form.Group>
          </Col>
        </Row>
      </Form>

      <FilterBar view={view} onChange={setView} total={total} showSearch={false} showSort={false} />

      <GlobalSearchSections query={view.q} />

      {degraded && (
        <Alert variant="warning" className="py-2">
          {t('search.degraded')}
        </Alert>
      )}

      {status === 'idle' && (
        <div className="text-center text-secondary py-5">
          <p className="mb-0 kk-section-title">{t('search.prompt')}</p>
        </div>
      )}

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('search.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('search.empty.title')} hint={t('search.empty.hint')} />
      )}

      {hasResults && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
          selection={bulk.gridSelection}
          detailQuery={detailQuery}
        />
      )}

      <SaveSearchModal
        show={savingView}
        params={view}
        onHide={() => {
          setSavingView(false)
        }}
        onSaved={() => {
          setSavingView(false)
        }}
      />
    </>
  )
}
