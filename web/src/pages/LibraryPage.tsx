import { useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'

/**
 * The main photo library: a filter/sort bar over a virtualized, infinite-scroll
 * thumbnail grid. The entire view (filters, sort) lives in the URL, so Back /
 * Forward restore the exact view and sharing the URL reproduces it. The grid
 * pages through the API as the user scrolls.
 */
export function LibraryPage() {
  const { t } = useTranslation()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)

  // Memoise the API params so the data hook only reloads when the query changes.
  const params = useMemo(() => viewToParams(view), [view])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = usePhotoLibrary(params)

  return (
    <>
      <h1 className="h3 mb-3">{t('library.title')}</h1>

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
        <div className="text-center text-secondary py-5">
          <p className="mb-1 fs-5">{t('library.empty.title')}</p>
          <p className="mb-0 small">{t('library.empty.hint')}</p>
        </div>
      )}

      {status === 'ready' && photos.length > 0 && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
        />
      )}
    </>
  )
}
