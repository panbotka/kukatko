import { useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'

/**
 * The favorites view: the same filter/sort bar and virtualized infinite-scroll
 * grid as the library, scoped to the current user's favorites via the
 * `favorite=true` list filter. Each tile keeps its favorite heart, so a photo can
 * be unfavorited in place (the change is optimistic; it reappears on the next
 * reload if the request failed). The view state lives in the URL like the library.
 */
export function FavoritesPage() {
  const { t } = useTranslation()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)

  // Scope every page to the acting user's favorites; the rest of the filters and
  // the sort apply on top, exactly as in the library.
  const params = useMemo(() => ({ ...viewToParams(view), favorite: 'true' }), [view])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = usePhotoLibrary(params)

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('favorites.title')}</h1>

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

      {status === 'ready' && photos.length > 0 && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
          favoritable
          ratable
        />
      )}
    </>
  )
}
