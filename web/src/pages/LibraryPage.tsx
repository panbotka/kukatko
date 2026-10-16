import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router-dom'
import { type ListRange } from 'react-virtuoso'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { buildChips } from '../components/library/filterChips'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid, type PhotoGridHandle } from '../components/library/PhotoGrid'
import { FeaturedAlbumsStrip } from '../components/library/FeaturedAlbumsStrip'
import { WhatsNewPanel } from '../components/library/WhatsNewPanel'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { QueryNoticesAlert } from '../components/search/QueryNoticesAlert'
import { UnknownFiltersAlert } from '../components/search/UnknownFiltersAlert'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridKeyboardNavigation } from '../hooks/useGridKeyboardNavigation'
import { useGridScrollMemory, useRememberedGridScroll } from '../hooks/useGridScrollMemory'
import { useLibraryFacets } from '../hooks/useLibraryFacets'
import { PAGE_SIZE, usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { useReloadKey } from '../hooks/useReloadKey'
import { useUploaders } from '../hooks/useUploaders'
import { detailQueryString } from '../lib/detailView'
import { type GridOrigin, gridScrollKey } from '../lib/gridScroll'
import {
  hasActiveFilters,
  LIBRARY_DEFAULTS,
  type LibraryView,
  viewToParams,
} from '../lib/libraryView'
import { searchHref } from '../lib/searchView'
import { type SlideshowScope } from '../lib/slideshowView'
import { useUrlState } from '../lib/urlState'
import { useProjectPath } from '../project/ProjectContext'
import { ProjectLink } from '../project/ProjectLink'
import { favoritePhoto } from '../services/photos'

/** The library plays every photo the filters leave — it scopes to no album or label. */
const NO_SCOPE: SlideshowScope = {}

/**
 * How many photos short of the last loaded one the visible range may reach
 * before the next page is asked for. Waiting for the very last row to come on
 * screen (virtuoso's `endReached`) makes every page boundary a stall the reader
 * scrolls into; half a page is a few screens of tiles at any density, which is
 * time enough for one request to land before the wall runs out.
 */
const LIBRARY_PREFETCH_PHOTOS = PAGE_SIZE / 2

/**
 * The main photo library: a filter/sort bar over a virtualized thumbnail grid.
 * The entire view (filters, sort) lives in the URL, so Back / Forward restore the
 * exact view and sharing the URL reproduces it. The grid grows the ordinary way:
 * the first page on load, the next one as the reader nears the end of what is
 * loaded, and a closing line once there is nothing more — so the scrollbar
 * measures what has been seen rather than the whole library, and only the rows
 * on screen are mounted however far the reader goes. Where the reader was
 * scrolled to is remembered per view for the session (`useGridScrollMemory`),
 * together with how many photos were loaded, so opening a photo and coming back
 * reloads that many behind the skeleton and lands on the tile it was opened
 * from rather than at the top. Every tile carries a favorite heart
 * (a personal toggle for all roles); a curator (or above) additionally gets a modern
 * multi-select — a corner checkmark on each tile (hover to reveal, Shift+click for
 * a range) and a floating batch action bar that rises once anything is picked, for
 * add-to-album, add/remove-label, favorite, archive, download and the full editor
 * via the bulk API. Escape clears the selection and hides the bar.
 *
 * **The page spends no line on a heading row, on any viewport.** "Knihovna"
 * over the photo wall told the reader what the navigation already highlights,
 * and the two view actions beside it (Slideshow, Save view) are occasional —
 * together they were a fifth of a phone's first screen and the top of a
 * desktop's. The heading stays for assistive technology (`visually-hidden`, so
 * the page still opens with an `h1`) and the actions go to {@link FilterBar} as
 * `viewActions`, which seats them in a line that exists anyway: beside the
 * result count on desktop, in the filters drawer on a phone. That, with the
 * digest condensed to one line and the filter pickers moved behind the Filters
 * button, is what puts the first row of photographs on the first screen at
 * 1280 x 800: measured against the deployed build over the same ~20k-photo
 * catalogue, 325 px -> 205 px with the digest dismissed, 261 px with it showing,
 * against the ~490 px the page used to open at.
 */
export function LibraryPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('library.title'))
  const { canCurate } = useAuth()
  const navigate = useNavigate()
  const projectPath = useProjectPath()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const [savingView, setSavingView] = useState(false)

  // Memoise the API params so the data hook only reloads when the query changes.
  const params = useMemo(() => viewToParams(view), [view])
  // The detail link carries this view — album, label, person and the favorites
  // toggle included, so prev/next pages through the same filtered grid — but never
  // the search scope, which the library never applies.
  const detailQuery = useMemo(() => detailQueryString({ ...view, mode: '', view: '' }), [view])
  // A bulk edit can change what the filters match, so bump the key to refetch.
  const [reloadKey, reload] = useReloadKey()
  // Where the grid was left, per view, so stepping into a photo and coming back
  // — Back, or the viewer's own "back to list", which pops the same entry —
  // returns to the tile it was opened from instead of to the top of the library.
  // The list only ever grew by appending pages, so it has to come back as long
  // as it was (`initialCount`, fetched behind the skeleton and shown at once)
  // before the remembered offset means anything.
  const location = useLocation()
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useRememberedGridScroll(scrollKey)?.count ?? 0
  const {
    photos,
    total,
    totalVideos,
    status,
    loadingMore,
    moreError,
    hasMore,
    unknownTokens,
    notices,
    loadMore,
    retry,
  } = usePhotoLibrary(params, { reloadKey, initialCount: restoreCount })
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })
  const facets = useLibraryFacets(params)
  // Who contributed to what is on screen — the option list behind the uploader
  // filter, counted under the rest of the view like every other facet.
  const uploaders = useUploaders(params)
  // Hover-select: every tile carries a corner checkmark for a curator, with no
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
      favOverrides.has(p.uid) ? { ...p, is_favorite: favOverrides.get(p.uid) } : p,
    )
  }, [photos, favOverrides])

  // A ref to the grid, so the keyboard focus can scroll the tile it moved to
  // into view.
  const gridRef = useRef<PhotoGridHandle>(null)
  // Ask for the next page a little before the wall runs out rather than at its
  // very last row (see LIBRARY_PREFETCH_PHOTOS). `loadMore` is a no-op while a
  // page is in flight or none remain, so reporting every range change is cheap;
  // the grid's own `onEndReached` stays wired as the backstop for a range that
  // jumps straight to the end (a keyboard End, a screen taller than a page).
  const loadedRef = useRef(photos.length)
  loadedRef.current = photos.length
  const onRangeChanged = useCallback(
    (range: ListRange) => {
      if (range.endIndex >= loadedRef.current - LIBRARY_PREFETCH_PHOTOS) {
        loadMore()
      }
    },
    [loadMore],
  )

  // Keyboard navigation over the grid: a visible focus highlight moved by the
  // arrow keys / hjkl, with Enter/x/f/Escape acting on the focused tile. A
  // justified wall has no one column count — each row holds as many photos as
  // their shapes allowed — so "one row down" is how many tiles the highlighted
  // row itself holds, read off the rendered row (the first one, before anything
  // is highlighted). It lands within a tile of the column it started in, which
  // is what the move means to the reader.
  const gridWrapRef = useRef<HTMLDivElement>(null)
  const getColumns = useCallback(() => {
    const wrap = gridWrapRef.current
    if (!wrap) {
      return 1
    }
    const focused = wrap.querySelector<HTMLElement>('[data-focused="true"]')
    const row =
      focused?.closest<HTMLElement>('.kukatko-photo-row') ??
      wrap.querySelector<HTMLElement>('.kukatko-photo-row')
    return row !== null && row.childElementCount > 0 ? row.childElementCount : 1
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
      // Named as the tiles name it, so the viewer records against this grid.
      const state: GridOrigin = { gridOrigin: gridScroll.key }
      const path = detailQuery === '' ? `/photos/${p.uid}` : `/photos/${p.uid}?${detailQuery}`
      void navigate(projectPath(path), { state })
    },
    [displayPhotos, detailQuery, navigate, projectPath, gridScroll.key],
  )
  const selectPhoto = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p || !canCurate) {
        return
      }
      selection.toggle(p.uid)
    },
    [displayPhotos, canCurate, selection],
  )
  // Select every tile that has paged in, not every match: it never reaches
  // beyond what the grid has actually loaded.
  const selectAllInView = useCallback(() => {
    selection.selectMany(displayPhotos.map((p) => p.uid))
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
  const activeFilters = buildChips(view, t, i18n.language, {
    facets,
    uploaders,
    includeQuery: true,
  })
  const clearFilters = () => {
    setView({ ...LIBRARY_DEFAULTS, sort: view.sort })
  }

  // The page's own view actions. The page no longer has a heading row to put
  // them in on any viewport, so it hands them to the filter bar, which renders
  // them in exactly one place per viewport (beside the result count on desktop,
  // in the filters drawer on a phone) — so neither button is ever in the
  // document twice.
  const viewActions = (
    <>
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
    </>
  )

  return (
    <>
      {/* The navigation already says which destination this is — the bottom tab
          bar on a phone, the navbar's active item on desktop — so a headline
          repeating it buys nothing and costs the top of the first screen, which
          on the app's front door is photographs. The heading survives for a
          screen reader, which reads it without it taking a row. */}
      <h1 className="visually-hidden">{t('library.title')}</h1>

      {/* What happened while the reader was away. It sits above the filters
          because it is about the library as a whole, not about the current
          view, and because it is the one thing on this page a returning reader
          should see before they start filtering. It renders nothing on a first
          visit, when nothing changed, or once dismissed. */}
      <WhatsNewPanel />

      {/* The albums a curator promoted ("Doporučujeme"), above the timeline. A
          promotion is for the front door, not for a search: the moment the
          reader narrows the view to something of their own it steps aside. It
          renders nothing when nothing is featured. */}
      {!hasActiveFilters(view) && <FeaturedAlbumsStrip />}

      <FilterBar
        view={view}
        onChange={setView}
        // Only a finished request has a count. The list resets to zero the
        // moment the query changes, so passing `total` unconditionally made
        // the bar — and, on a phone, the drawer's own button — read "no photos"
        // for the length of every refetch, which is exactly the number the
        // reader is waiting to find out. Pending says so; an error says nothing
        // at all, because the page states the failure in full below.
        total={status === 'ready' ? total : undefined}
        // What that count may be called: a shelf of clips is not seven photos.
        totalVideos={totalVideos}
        totalPending={status === 'loading'}
        facets={facets}
        uploaders={uploaders}
        showFavorite
        searchHref={projectPath(searchHref(view))}
        viewActions={viewActions}
      />

      {/* A mistyped filter key (`osoba:` for `person:`) is not "no such photos":
          it degraded to free text, so say which token fell through instead of
          letting an empty grid blame the library — and, when one of the real
          keys is close enough, offer it and type the fix back into the box. */}
      <UnknownFiltersAlert
        tokens={unknownTokens}
        query={view.q}
        onFix={(fixed) => {
          setView({ q: fixed }, { replace: true })
        }}
      />
      {/* And an empty grid that is a decision rather than an absence — `person:me`
          from an account that has not said who it is — says so, with the way to
          fix it. */}
      <QueryNoticesAlert notices={notices} />

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
          hint={t('library.emptyCatalog.hint')}
          action={
            <ProjectLink to="/upload" className="btn btn-primary">
              {t('library.emptyCatalog.action')}
            </ProjectLink>
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
              loadingMore={loadingMore}
              moreError={moreError}
              onEndReached={loadMore}
              onRetry={retry}
              // The wall's last line once every match is in, so its end reads
              // as the end of the result rather than as a page that never came.
              endNote={hasMore ? undefined : t('library.end')}
              selection={bulk.gridSelection}
              favoritable
              onFavoriteChange={noteFavorite}
              detailQuery={detailQuery}
              gridRef={gridRef}
              onRangeChanged={onRangeChanged}
              focusedIndex={focusedIndex}
              scroll={gridScroll}
            />
          </div>
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
