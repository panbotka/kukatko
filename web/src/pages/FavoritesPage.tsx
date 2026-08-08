import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { useReloadKey } from '../hooks/useReloadKey'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'

/**
 * The favorites view: the same filter/sort bar and virtualized infinite-scroll
 * grid as the library, scoped to the current user's favorites via the
 * `favorite=true` list filter. Each tile keeps its favorite heart, so a photo can
 * be unfavorited in place (the change is optimistic; it reappears on the next
 * reload if the request failed). The view state lives in the URL like the library.
 *
 * Editors can also multi-select tiles — the corner checkmark is offered straight
 * away, as on the library — and picking one raises the library's own floating
 * batch bar with the full set of batch actions. Since the list *is* the
 * favorites filter, a bulk edit that clears the favorite flag takes those photos
 * out of the view: the selection is cleared before the refetch, so no photo that
 * just left the grid stays selected.
 */
export function FavoritesPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('favorites.title'))
  const location = useLocation()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const [reloadKey, reload] = useReloadKey()

  // Scope every page to the acting user's favorites; the rest of the filters and
  // the sort apply on top, exactly as in the library.
  const params = useMemo(() => ({ ...viewToParams(view), favorite: 'true' }), [view])
  // Each tile carries the favorites scope so the detail page pages prev/next
  // within favorites and Esc/Back returns here, not the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, favorite: 'true', mode: '' }),
    [view],
  )
  // Where the grid was left, per view, so opening a photo and coming back — Back,
  // or the viewer's own "back to list", which pops the same entry — returns to
  // the tile it was opened from. This list only ever grew by appending pages, so
  // it also has to come back as long as it was before the offset means anything.
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = usePhotoLibrary(
    params,
    { reloadKey, initialCount: restoreCount },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const hasPhotos = status === 'ready' && photos.length > 0
  const selecting = selection.count > 0

  // Select every tile that has paged in, matching the library's select-all: it
  // never reaches beyond what the grid has actually loaded.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((p) => p.uid))
  }, [photos, selection])

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('favorites.title')}</h1>

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('favorites.empty.title')} hint={t('favorites.empty.hint')} />
      )}

      {hasPhotos && (
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
            favoritable
            detailQuery={detailQuery}
            restoreStateFrom={gridScroll.restoreFrom}
            onStateChanged={gridScroll.onStateChanged}
          />
        </div>
      )}

      {bulk.canBulkEdit && selecting && (
        <BatchActionBar bulk={bulk} onSelectAll={selectAllInView} />
      )}
    </>
  )
}
