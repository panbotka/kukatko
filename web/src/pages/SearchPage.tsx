import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { SavedSearchesDropdown } from '../components/savedsearch/SavedSearchesDropdown'
import { GlobalSearchSections } from '../components/search/GlobalSearchSections'
import { SearchEmptyState } from '../components/search/SearchEmptyState'
import { SearchModeControl } from '../components/search/SearchModeControl'
import { SearchQueryHelp } from '../components/search/SearchQueryHelp'
import { SearchQueryInput } from '../components/search/SearchQueryInput'
import { QueryNoticesAlert } from '../components/search/QueryNoticesAlert'
import { UnknownFiltersAlert } from '../components/search/UnknownFiltersAlert'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { usePhotoSearch } from '../hooks/usePhotoSearch'
import { useReloadKey } from '../hooks/useReloadKey'
import { useRecordSearch } from '../hooks/useSearchHistory'
import { useSearchMode } from '../hooks/useSearchMode'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { hasActiveFilters, LIBRARY_DEFAULTS, viewToParams } from '../lib/libraryView'
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
 * non-blocking notice beside the mode selector explains that semantic ranking
 * was skipped — shown as soon as the instance reports the sidecar unreachable,
 * before a search runs, since by then the request has already gone out as
 * full-text and there is nothing to wait for.
 *
 * The results can be played as a slideshow, which replays the search itself (the
 * `mode` travels in the URL) rather than re-listing the library by the query.
 *
 * This page also owns saved searches: the header pairs a "save this view" button
 * with the {@link SavedSearchesDropdown} that lists, applies and manages them.
 * Alongside them, every query the reader *submits* here — Enter, or picking one
 * of them again — is remembered per user and offered back by
 * {@link SearchQueryInput} whenever the box is focused: saved searches are the
 * ones worth naming, the history the ones merely worth repeating. The searches
 * the debounce runs along the way are not submissions and are not remembered,
 * or a hesitation mid-word would leave its prefix in the history for good.
 *
 * Editors can multi-select results straight away — the corner checkmark is
 * offered from the outset, as on the library — and picking one raises the
 * library's own floating batch bar with the full set of batch actions; the
 * search re-runs afterwards, since an edit can change what the query and filters
 * match.
 */
export function SearchPage() {
  const { t } = useTranslation()
  const location = useLocation()
  const [view, setView] = useUrlState<SearchView>(SEARCH_DEFAULTS)

  const params = useMemo(() => viewToParams(view), [view])
  // The selector keeps showing what the reader picked; `mode` is what the search
  // actually runs as, which is full-text whenever the embeddings box is down.
  // Everything downstream — the fetch, the detail links, the slideshow — uses the
  // effective mode, so nothing further along re-asks for the unavailable one.
  const { mode, semanticAvailable, downgraded } = useSearchMode(toMode(view.mode))
  // Each tile carries the search scope — the query, filters and (always-present)
  // mode — so the detail page pages prev/next through the same ranked results and
  // Esc/Back returns to the search, not the library with `q` as a substring filter.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, favorite: '', mode }),
    [view, mode],
  )
  // Where the grid was left, per view, so opening a photo and coming back — Back,
  // or the viewer's own "back to list", which pops the same entry — returns to
  // the tile it was opened from. This list only ever grew by appending pages, so
  // it also has to come back as long as it was before the offset means anything.
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const [reloadKey, reload] = useReloadKey()
  const {
    photos,
    total,
    status,
    degraded,
    unknownTokens,
    notices,
    loadingMore,
    moreError,
    loadMore,
    retry,
  } = usePhotoSearch(params, mode, { reloadKey, initialCount: restoreCount })
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const selecting = selection.count > 0
  const hasQuery = view.q.trim() !== ''
  const hasResults = status === 'ready' && photos.length > 0
  // The query is this page's own input rather than one of the bar's filters, so
  // it does not count as narrowing here — and clearing keeps it, exactly as the
  // filter bar's own "clear" does on this page.
  const filtered = hasActiveFilters(view, { ignoreQuery: true })
  const clearFilters = useCallback(() => {
    setView({ ...LIBRARY_DEFAULTS, sort: view.sort, q: view.q })
  }, [setView, view.sort, view.q])
  // The tab quotes the query — „svatba" — because a search is only worth finding
  // again by what was asked for; an empty page is just "Hledání". It follows the
  // URL, not the debounced input, so history entries and the tab agree.
  useDocumentTitle(
    hasQuery ? t('documentTitle.search', { query: view.q.trim() }) : t('search.title'),
  )

  // Local, debounced mirror of the URL query so typing stays responsive but the
  // URL (and the fetch) only update after the user pauses. The query is the
  // page's own input, separate from the filter bar.
  const [text, setText] = useState(view.q)
  const [savingView, setSavingView] = useState(false)

  // Remember what was searched for, so the box can offer it back — here and on
  // whatever device the reader picks up next. Only a submitted query is recorded
  // (Enter below, or picking a recent search), never the query that merely ran:
  // typing pauses commit to the URL and search, so watching that would remember
  // every prefix the reader hesitated on.
  const recordSearch = useRecordSearch()

  // Keep the input in sync when the URL query changes from elsewhere (a saved
  // search, Back/Forward, a shared link).
  useEffect(() => {
    setText(view.q)
  }, [view.q])

  // A new query or mode is a different result set, and an empty query shows no
  // grid at all — a selection made against the old results has nowhere to live,
  // so leave selection mode with it. Filters, which merely narrow the same
  // search, keep the selection, as they do on the library.
  // Both the picked mode and the effective one are watched: the reader switching
  // modes is the obvious case, and the sidecar coming back re-ranks the same
  // query into a different result set without anyone touching the controls.
  const leaveSelection = selection.disable
  useEffect(() => {
    leaveSelection()
  }, [view.q, view.mode, mode, leaveSelection])

  // Select every result that has paged in, matching the library's select-all: it
  // never reaches beyond what the grid has actually loaded.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((p) => p.uid))
  }, [photos, selection])

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
        {/* The search's own actions stay put during a selection: the batch bar
            floats over the bottom edge and never contends with the header. */}
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
        </div>
      </div>

      <Form
        role="search"
        aria-label={t('search.formLabel')}
        className="mb-3"
        onSubmit={(e) => {
          e.preventDefault()
          // Commit immediately on submit, bypassing the debounce.
          setView({ q: text }, { replace: true })
          // Enter is the reader saying this is the query they meant — the one
          // act worth remembering, out of the several searches their typing ran.
          recordSearch(text)
        }}
      >
        {/* The box is the page: it now spans the whole width the mode selector
            used to share, which is what a field holding a whole query wants. */}
        <Form.Group controlId="search-query">
          <div className="d-flex align-items-center gap-2 mb-1">
            <Form.Label className="small mb-0">{t('search.queryLabel')}</Form.Label>
            {/* The query language's discoverability: filters + operators. */}
            <SearchQueryHelp />
          </div>
          <SearchQueryInput
            id="search-query"
            value={text}
            autoFocus
            placeholder={t('search.placeholder')}
            onChange={setText}
            onRun={(next) => {
              // Picking a recent search runs it at once: it is a whole query, so
              // waiting out the typing debounce would only feel slow. It is also
              // a submit, so it moves back to the front of the ring.
              setView({ q: next }, { replace: true })
              recordSearch(next)
            }}
          />
        </Form.Group>

        {/* How the search ranks is a preference, not a question to answer before
            looking for a photograph: the switch lives behind an "advanced"
            toggle and everyone else gets the smart mode. */}
        <div className="mt-2">
          <SearchModeControl
            mode={view.mode}
            semanticAvailable={semanticAvailable}
            onChange={(next) => {
              setView({ mode: next })
            }}
          />
        </div>

        {/* Beside the controls that caused it, and before any results: the
            fallback is known from the capability flag, not from a reply. */}
        {(downgraded || degraded) && (
          <Alert variant="warning" className="py-2 mt-2 mb-0">
            {t('search.degraded')}
          </Alert>
        )}
        {/* A mistyped key belongs beside the box that mistyped it, not below
            the filters and the cross-entity sections: it is feedback on what was
            just typed, and accepting the suggestion types the fix back in. */}
        <UnknownFiltersAlert
          tokens={unknownTokens}
          query={view.q}
          onFix={(fixed) => {
            // Both halves of the box: the URL is what the search runs on, `text`
            // is what the reader sees, and leaving the two apart would let the
            // typing debounce commit the broken query straight back.
            setText(fixed)
            setView({ q: fixed }, { replace: true })
          }}
        />
      </Form>

      {/* No query means no result set, so there is no count to state: showing
          "0 photos" above the "type something" prompt reads as an empty library. */}
      <FilterBar
        view={view}
        onChange={setView}
        total={hasQuery ? total : undefined}
        showSearch={false}
        showSort={false}
      />

      <GlobalSearchSections query={view.q} />

      <QueryNoticesAlert notices={notices} />

      {status === 'idle' && (
        <div className="text-center text-secondary py-5">
          <p className="mb-0 kk-section-title">{t('search.prompt')}</p>
        </div>
      )}

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('search.error.load')} onRetry={retry} />}

      {status === 'ready' && photos.length === 0 && (
        <SearchEmptyState
          query={view.q.trim()}
          hasFilters={filtered}
          onClearFilters={clearFilters}
          // Describing the photo is the one thing this library can do that no
          // filename search can — but only while the box that reads photographs
          // is up, and only when it is not already what just found nothing.
          canDescribe={semanticAvailable && mode !== 'semantic' && hasQuery}
          onDescribe={() => {
            setView({ mode: 'semantic' })
          }}
        />
      )}

      {hasResults && (
        // Keep the last rows scrollable clear of the floating bar while a
        // selection is active, so nothing hides behind it.
        <div style={{ paddingBottom: selecting ? 'var(--kk-batch-clearance)' : undefined }}>
          <PhotoGrid
            photos={photos}
            loadingMore={loadingMore}
            moreError={moreError}
            onEndReached={loadMore}
            onRetry={retry}
            selection={bulk.gridSelection}
            detailQuery={detailQuery}
            scroll={gridScroll}
          />
        </div>
      )}

      {bulk.canBulkEdit && selecting && (
        <BatchActionBar bulk={bulk} onSelectAll={selectAllInView} />
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
