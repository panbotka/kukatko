import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
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
 */
export function LabelDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })

  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ label: uid }), [uid])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
  )

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

  return (
    <>
      <div className="d-flex align-items-center gap-2 mb-3 flex-wrap">
        <Link to="/labels" className="text-decoration-none">
          ← {t('labelDetail.back')}
        </Link>
        <h1 className="kk-page-title mb-0">{title}</h1>
        {photos.length > 0 && (
          <span className="ms-auto">
            <SlideshowStart scope={scope} view={view} count={total} />
          </span>
        )}
      </div>

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
