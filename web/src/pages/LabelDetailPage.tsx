import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'

import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { fetchLabel, type Label } from '../services/organize'

/** Fetch lifecycle of the label record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; label: Label }

/**
 * Where the back link leads. The labels index keeps no view state of its own in
 * the URL, so the bare route restores it exactly; should it ever grow filters,
 * this is the one place that has to carry them.
 */
const LABELS_PATH = '/labels'

/**
 * A label's page: the label name as a header above the photo grid scoped to that
 * label. Filters and sort live in the URL (shared {@link FilterBar} +
 * urlState), so the scoped view round-trips through the URL exactly like the main
 * library and Back/Forward restore it.
 *
 * Editors can multi-select tiles straight away — the corner checkmark is offered
 * from the outset, as on the library — and bulk-edit the picked photos, dropping
 * this very label among other things, after which the grid refetches, since the
 * edit may have taken photos out of the label.
 */
export function LabelDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()

  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ label: uid }), [uid])
  // Each tile carries the label scope so the detail page pages prev/next within
  // this label and Esc/Back returns to it, not the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, label: uid, album: '', favorite: '', mode: '' }),
    [view, uid],
  )
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey },
  )

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const selecting = selection.count > 0

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
      <ErrorState
        title={t('labelDetail.error')}
        action={<BackLink to={LABELS_PATH} label={t('labelDetail.back')} />}
      />
    )
  }

  const title = state.status === 'ready' ? state.label.name : ''
  const hasPhotos = status === 'ready' && photos.length > 0

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <BackLink to={LABELS_PATH} label={t('labelDetail.back')} />
          <h1 className="kk-page-title mb-0">{title}</h1>
        </div>
        {/* The label's own actions step aside while a selection is being made:
            the selection bar below is then the page's only toolbar. */}
        {!selecting && hasPhotos && (
          <div className="d-flex gap-1 flex-wrap">
            <SlideshowStart scope={scope} view={view} count={total} />
          </div>
        )}
      </div>

      {selecting && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <BulkEditControl bulk={bulk} />
        </SelectionBar>
      )}

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

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
          detailQuery={detailQuery}
        />
      )}
    </>
  )
}
