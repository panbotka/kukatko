import { useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditModal } from '../components/organize/BulkEditModal'
import { SelectionBar } from '../components/organize/SelectionBar'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { useSelection } from '../hooks/useSelection'
import { detailQueryString } from '../lib/detailView'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { slideshowHref } from '../lib/slideshowView'
import { useUrlState } from '../lib/urlState'

/**
 * The main photo library: a filter/sort bar over a virtualized, infinite-scroll
 * thumbnail grid. The entire view (filters, sort) lives in the URL, so Back /
 * Forward restore the exact view and sharing the URL reproduces it. The grid
 * pages through the API as the user scrolls. Every tile carries a favorite heart
 * (a personal toggle for all roles); editors can additionally enter selection
 * mode to bulk-edit a multi-photo selection (albums, labels, description,
 * location, private, archive, favorite) via the bulk API.
 */
export function LibraryPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const selection = useSelection()
  const [editing, setEditing] = useState(false)

  // Memoise the API params so the data hook only reloads when the query changes.
  const params = useMemo(() => viewToParams(view), [view])
  // The detail link carries this view so prev/next and Back respect the order.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, album: '', label: '', favorite: '' }),
    [view],
  )
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = usePhotoLibrary(params)

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="h3 mb-0">{t('library.title')}</h1>
        {!selection.active && (
          <div className="d-flex gap-1 flex-wrap">
            {status === 'ready' && photos.length > 0 && (
              <Link to={slideshowHref({}, view)} className="btn btn-outline-secondary btn-sm">
                {t('slideshow.start')}
              </Link>
            )}
            {canWrite && (
              <Button variant="outline-secondary" size="sm" onClick={selection.enable}>
                {t('library.select')}
              </Button>
            )}
          </div>
        )}
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={photos.length === 0}
            onClick={() => {
              selection.selectMany(photos.map((p) => p.uid))
            }}
          >
            {t('library.selectAll')}
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={selection.count === 0}
            onClick={() => {
              setEditing(true)
            }}
          >
            {t('library.bulkEdit')}
          </Button>
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
          selection={
            selection.active
              ? { active: true, selected: selection.selected, onToggle: selection.toggle }
              : undefined
          }
          favoritable={!selection.active}
          detailQuery={detailQuery}
        />
      )}

      {canWrite && (
        <BulkEditModal
          show={editing}
          photoUids={[...selection.selected]}
          onHide={() => {
            setEditing(false)
          }}
          onDone={() => {
            setEditing(false)
            selection.disable()
          }}
        />
      )}
    </>
  )
}
