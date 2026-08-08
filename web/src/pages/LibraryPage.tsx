import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { type ListRange, type VirtuosoGridHandle } from 'react-virtuoso'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { buildChips } from '../components/library/filterChips'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { type TimelineJump, TimelineScrubber } from '../components/library/TimelineScrubber'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { UnknownFiltersAlert } from '../components/search/UnknownFiltersAlert'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useGridKeyboardNavigation } from '../hooks/useGridKeyboardNavigation'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useLibraryFacets } from '../hooks/useLibraryFacets'
import { useReloadKey } from '../hooks/useReloadKey'
import { useWindowedPhotos } from '../hooks/useWindowedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey } from '../lib/gridScroll'
import {
  hasActiveFilters,
  LIBRARY_DEFAULTS,
  type LibraryView,
  viewToParams,
} from '../lib/libraryView'
import { searchHref } from '../lib/searchView'
import { type SlideshowScope } from '../lib/slideshowView'
import { useUrlState } from '../lib/urlState'
import { favoritePhoto } from '../services/photos'

/** The library plays every photo the filters leave — it scopes to no album or label. */
const NO_SCOPE: SlideshowScope = {}

/**
 * Query param holding the month (`YYYY-MM`) the grid is positioned at. It is not
 * part of {@link LibraryView}: it filters nothing, so it must neither reach the
 * API nor be saved with a smart album — and dropping it whenever a filter changes
 * (which `writeUrlState` does, since it only writes the view's own keys) is
 * exactly right, because a new filter renumbers every position anyway.
 */
const ANCHOR_PARAM = 'at'

/**
 * The main photo library: a filter/sort bar over a virtualized thumbnail grid.
 * The entire view (filters, sort) lives in the URL, so Back / Forward restore the
 * exact view and sharing the URL reproduces it — the timeline's position
 * ({@link ANCHOR_PARAM}) included. Where the reader was scrolled to is remembered
 * per view for the session (`useGridScrollMemory`), so opening a photo and coming
 * back lands on the tile it was opened from rather than at the top. The grid is a
 * *window* over the result: it is
 * as tall as the whole library from the first response on and fetches the pages
 * under the viewport as they come into view, which is what lets the timeline jump
 * to any month at a fixed cost. Every tile carries a favorite heart
 * (a personal toggle for all roles); an editor additionally gets a modern
 * multi-select — a corner checkmark on each tile (hover to reveal, Shift+click for
 * a range) and a floating batch action bar that rises once anything is picked, for
 * add-to-album, add/remove-label, favorite, archive, download and the full editor
 * via the bulk API. Escape clears the selection and hides the bar.
 */
export function LibraryPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const navigate = useNavigate()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const [savingView, setSavingView] = useState(false)

  // Memoise the API params so the data hook only reloads when the query changes.
  const params = useMemo(() => viewToParams(view), [view])
  // The detail link carries this view — album, label, person and the favorites
  // toggle included, so prev/next pages through the same filtered grid — but never
  // the search scope, which the library never applies.
  const detailQuery = useMemo(() => detailQueryString({ ...view, mode: '' }), [view])
  // A bulk edit can change what the filters match, so bump the key to refetch.
  const [reloadKey, reload] = useReloadKey()
  // The grid is a *window* over the result, not a growing prefix of it: `photos`
  // is as long as the whole library with holes where pages are not loaded, so any
  // position is reachable in one scroll plus one fetch.
  const { photos, total, status, moreError, unknownTokens, ensureRange, retry } = useWindowedPhotos(
    params,
    { reloadKey },
  )
  const facets = useLibraryFacets(params)
  // Hover-select: every tile carries a corner checkmark for a writer, with no
  // explicit "enter selection mode" step, and the floating batch bar rises the
  // moment anything is picked.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection

  // The one live favorite state per photo, shared by both ways to flip it: the
  // `f` shortcut on the focused tile and the tile's own heart, which reports its
  // flips back here (`onFavoriteChange`). Keeping a single baseline is what makes
  // `f` after a heart-click toggle rather than repeat the click. The override is
  // applied to the displayed photos immediately (each tile's own useFavorite
  // resyncs from the prop) and rolled back if the request fails. Cleared whenever
  // the list is refetched — a new view, or a bulk edit that may itself have set
  // the favorite flag — so no override outlives its list.
  const [favOverrides, setFavOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())
  useEffect(() => {
    setFavOverrides(new Map())
  }, [detailQuery, reloadKey])
  const displayPhotos = useMemo(() => {
    if (favOverrides.size === 0) {
      return photos
    }
    return photos.map((p) =>
      p !== undefined && favOverrides.has(p.uid)
        ? { ...p, is_favorite: favOverrides.get(p.uid) }
        : p,
    )
  }, [photos, favOverrides])

  // Timeline scrubber wiring: a ref to the grid to scroll it, the first visible
  // index to highlight the current month, and a jump that scrolls straight to the
  // month's absolute index and fetches the one page that lands there — the cost
  // of a jump no longer depends on how far it goes, which is what made "jump to
  // 2011" unusable on a 20 000 photo library. The scrubber is only meaningful for
  // the default newest-first date order (the timeline is always date-grouped), so
  // it is hidden for other sorts and in selection mode.
  const gridRef = useRef<VirtuosoGridHandle>(null)
  // Where the grid was left, per view, so stepping into a photo and coming back
  // — Back, or the viewer's own "back to list", which pops the same entry —
  // returns to the tile it was opened from instead of to the top of the library.
  // The windowed grid is as tall as the whole result from its first response, so
  // there is no loaded length to catch up to first: the offset alone restores it.
  const location = useLocation()
  const gridScroll = useGridScrollMemory({
    key: gridScrollKey(location.pathname, location.search),
  })
  const [rangeStart, setRangeStart] = useState(0)
  const onRangeChanged = useCallback(
    (range: ListRange) => {
      setRangeStart(range.startIndex)
      ensureRange(range.startIndex, range.endIndex)
    },
    [ensureRange],
  )
  const showScrubber = view.sort === LIBRARY_DEFAULTS.sort && selection.count === 0

  // The month the view is positioned at, kept in the URL so Back, a reload and a
  // shared link all land where the reader was — the project's "Back always
  // works" rule applied to the one navigation that can skip thousands of photos.
  const [searchParams, setSearchParams] = useSearchParams()
  const anchor = searchParams.get(ANCHOR_PARAM) ?? ''
  const jumpTo = useCallback(
    (jump: TimelineJump) => {
      // Start the fetch before the scroll: both are one request/one frame, and
      // overlapping them is what keeps the placeholders on screen briefest.
      ensureRange(jump.index, jump.index)
      gridRef.current?.scrollToIndex({ index: jump.index, align: 'start' })
      if ((searchParams.get(ANCHOR_PARAM) ?? '') === jump.month) {
        return
      }
      const next = new URLSearchParams(searchParams)
      next.set(ANCHOR_PARAM, jump.month)
      setSearchParams(next, { replace: jump.replace })
    },
    [ensureRange, searchParams, setSearchParams],
  )

  // Keyboard navigation over the grid: a visible focus highlight moved by the
  // arrow keys / hjkl, with Enter/x/f/Escape acting on the focused tile. Row-wise
  // moves need the live column count, read from the rendered grid's computed
  // template so it tracks the responsive `auto-fill` layout.
  const gridWrapRef = useRef<HTMLDivElement>(null)
  const getColumns = useCallback(() => {
    const el = gridWrapRef.current?.querySelector<HTMLElement>('.kukatko-photo-grid')
    if (!el) {
      return 1
    }
    const tracks = getComputedStyle(el)
      .gridTemplateColumns.split(' ')
      .filter((track) => track.trim() !== '')
    return tracks.length > 0 ? tracks.length : 1
  }, [])
  const scrollFocusIntoView = useCallback((index: number) => {
    gridRef.current?.scrollToIndex(index)
  }, [])
  const openPhoto = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p) {
        return
      }
      void navigate(detailQuery === '' ? `/photos/${p.uid}` : `/photos/${p.uid}?${detailQuery}`)
    },
    [displayPhotos, detailQuery, navigate],
  )
  const selectPhoto = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p || !canWrite) {
        return
      }
      selection.toggle(p.uid)
    },
    [displayPhotos, canWrite, selection],
  )
  // Select every loaded tile in view (only what has paged in, not every match —
  // and with a windowed list that is the pages around the reader's position).
  const selectAllInView = useCallback(() => {
    selection.selectMany(displayPhotos.filter((p) => p !== undefined).map((p) => p.uid))
  }, [displayPhotos, selection])
  const toggleFavorite = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p) {
        return
      }
      const current = favOverrides.get(p.uid) ?? p.is_favorite ?? false
      const next = !current
      setFavOverrides((m) => new Map(m).set(p.uid, next))
      favoritePhoto(p.uid, next).catch(() => {
        setFavOverrides((m) => new Map(m).set(p.uid, current))
      })
    },
    [displayPhotos, favOverrides],
  )
  // A tile's own heart owns its request; the page only records the resulting
  // state so the shortcut starts from what the reader last saw.
  const noteFavorite = useCallback((uid: string, favorite: boolean) => {
    setFavOverrides((m) => new Map(m).set(uid, favorite))
  }, [])
  const { focusedIndex } = useGridKeyboardNavigation({
    count: displayPhotos.length,
    enabled: status === 'ready' && displayPhotos.length > 0,
    resetKey: detailQuery,
    getColumns,
    scrollToIndex: scrollFocusIntoView,
    onOpen: openPhoto,
    onToggleSelect: selectPhoto,
    onToggleFavorite: toggleFavorite,
    hasSelection: selection.count > 0,
    onClearSelection: selection.clear,
  })

  // "Nothing matches these filters" and "there are no photos yet" are different
  // messages: the first asks the reader to relax a filter, the second — the very
  // first thing a new user sees, since the library is the homepage — invites them
  // to upload. Only an unfiltered empty result can mean the catalog itself is bare.
  const noResults = status === 'ready' && photos.length === 0
  const catalogEmpty = noResults && !hasActiveFilters(view)

  // A reader staring at zero results needs to see exactly which filters got them
  // there — the quick-filter text included, unlike the bar's own chips — and be
  // able to drop them all in one click. Clearing keeps the sort: it narrows
  // nothing, so resetting it would be a surprise.
  const activeFilters = buildChips(view, t, { facets, includeQuery: true })
  const clearFilters = () => {
    setView({ ...LIBRARY_DEFAULTS, sort: view.sort })
  }

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('library.title')}</h1>
        <div className="d-flex gap-1 flex-wrap">
          {status === 'ready' && photos.length > 0 && (
            <SlideshowStart scope={NO_SCOPE} view={view} count={total} />
          )}
          {/* Saving a view is personal, like the favourite heart beside it — the
              saved search belongs to the reader, not to the library — so it is
              live for every role, including a viewer, and carries the same
              one-line explanation the search header gives it. */}
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

      <FilterBar
        view={view}
        onChange={setView}
        total={total}
        facets={facets}
        showFavorite
        searchHref={searchHref(view)}
      />

      {/* A mistyped filter key (`osoba:` for `person:`) is not "no such photos":
          it degraded to free text, so say which token fell through instead of
          letting an empty grid blame the library. */}
      <UnknownFiltersAlert tokens={unknownTokens} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

      {noResults && !catalogEmpty && (
        <EmptyState
          title={t('library.empty.title')}
          hint={t('library.empty.hintFilters', {
            filters: activeFilters.map((chip) => chip.label).join(' · '),
          })}
          action={
            <Button variant="primary" onClick={clearFilters}>
              {t('library.empty.clearFilters')}
            </Button>
          }
        />
      )}

      {catalogEmpty && (
        <EmptyState
          title={t('library.emptyCatalog.title')}
          hint={canWrite ? t('library.emptyCatalog.hint') : t('library.emptyCatalog.hintViewer')}
          action={
            canWrite ? (
              <Link to="/upload" className="btn btn-primary">
                {t('library.emptyCatalog.action')}
              </Link>
            ) : undefined
          }
        />
      )}

      {status === 'ready' && photos.length > 0 && (
        <>
          <div
            ref={gridWrapRef}
            // Keep the last rows scrollable clear of the floating bar while a
            // selection is active, so nothing hides behind it. The clearance
            // tracks the bar's measured height (`--kk-batch-clearance`), so it
            // holds however the bar wraps or collapses on a phone.
            style={{ paddingBottom: selection.count > 0 ? 'var(--kk-batch-clearance)' : undefined }}
          >
            <PhotoGrid
              photos={displayPhotos}
              // A windowed list has no "end" to reach: it loads from what is on
              // screen (`onRangeChanged`), so nothing appends and the footer only
              // ever has to offer a retry.
              loadingMore={false}
              moreError={moreError}
              onRetry={retry}
              selection={bulk.gridSelection}
              favoritable
              onFavoriteChange={noteFavorite}
              detailQuery={detailQuery}
              gridRef={gridRef}
              onRangeChanged={onRangeChanged}
              focusedIndex={focusedIndex}
              restoreStateFrom={gridScroll.restoreFrom}
              onStateChanged={gridScroll.onStateChanged}
            />
          </div>
          {showScrubber && (
            <TimelineScrubber
              params={params}
              activeIndex={rangeStart}
              anchor={anchor}
              onJump={jumpTo}
            />
          )}
        </>
      )}

      {bulk.canBulkEdit && selection.count > 0 && (
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
