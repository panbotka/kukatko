import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { fetchLabel, type Label } from '../services/organize'

/** Fetch lifecycle of the label record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; label: Label }

/**
 * A label's page: the label name as a header above the photo grid scoped to that
 * label. Filters and sort live in the URL (shared {@link FilterBar} +
 * urlState), so the scoped view round-trips through the URL exactly like the main
 * library and Back/Forward restore it.
 *
 * Editors can enter selection mode and bulk-edit the picked photos — dropping
 * this very label among other things — after which the grid refetches, since the
 * edit may have taken photos out of the label.
 */
export function LabelDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, setReloadKey] = useState('0')

  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ label: uid }), [uid])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey },
  )

  const reload = useCallback(() => {
    setReloadKey((k) => String(Number(k) + 1))
  }, [])

  const bulk = useBulkEdit({ onEdited: reload })
  const selection = bulk.selection

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchLabel(uid, controller.signal)
      .then((label) => {
        setState({ status: 'ready', label })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  if (state.status === 'error') {
    return (
      <Alert variant="danger">
        {t('labelDetail.error')} <Link to="/labels">{t('labelDetail.back')}</Link>
      </Alert>
    )
  }

  const title = state.status === 'ready' ? state.label.name : ''
  const hasPhotos = status === 'ready' && photos.length > 0

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <Link to="/labels" className="text-decoration-none">
            ← {t('labelDetail.back')}
          </Link>
          <h1 className="kk-page-title mb-0">{title}</h1>
        </div>
        {!selection.active && hasPhotos && (
          <div className="d-flex gap-1 flex-wrap">
            <SlideshowStart scope={scope} view={view} count={total} />
            {bulk.canBulkEdit && (
              <Button variant="outline-secondary" size="sm" onClick={selection.enable}>
                {t('labelDetail.select')}
              </Button>
            )}
          </div>
        )}
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
        <EmptyState title={t('labelDetail.empty.title')} hint={t('labelDetail.empty.hint')} />
      )}

      {hasPhotos && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
          selection={bulk.gridSelection}
        />
      )}
    </>
  )
}
