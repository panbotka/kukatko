import { useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SelectionStart } from '../components/organize/SelectionStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { useReloadKey } from '../hooks/useReloadKey'
import { detailQueryString } from '../lib/detailView'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'

/**
 * The favorites view: the same filter/sort bar and virtualized infinite-scroll
 * grid as the library, scoped to the current user's favorites via the
 * `favorite=true` list filter. Each tile keeps its favorite heart, so a photo can
 * be unfavorited in place (the change is optimistic; it reappears on the next
 * reload if the request failed). The view state lives in the URL like the library.
 *
 * Editors can also enter selection mode and bulk-edit the picked photos. Since
 * the list *is* the favorites filter, a bulk edit that clears the favorite flag
 * takes those photos out of the view: the selection is cleared before the refetch,
 * so no photo that just left the grid stays selected.
 */
export function FavoritesPage() {
  const { t } = useTranslation()
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
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = usePhotoLibrary(
    params,
    { reloadKey },
  )

  const bulk = useBulkEdit({ onEdited: reload })
  const selection = bulk.selection
  const hasPhotos = status === 'ready' && photos.length > 0

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('favorites.title')}</h1>
        {hasPhotos && <SelectionStart bulk={bulk} />}
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <BulkEditControl bulk={bulk} />
        </SelectionBar>
      )}

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('library.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('favorites.empty.title')} hint={t('favorites.empty.hint')} />
      )}

      {hasPhotos && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
          selection={bulk.gridSelection}
          favoritable
          detailQuery={detailQuery}
        />
      )}
    </>
  )
}
