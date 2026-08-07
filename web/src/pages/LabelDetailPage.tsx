import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useParams } from 'react-router-dom'

import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BatchActionBar } from '../components/organize/BatchActionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { isNotFound } from '../services/auth'
import { fetchLabel, type Label } from '../services/organize'

/**
 * Fetch lifecycle of the label record. `missing` is a 404 kept apart from
 * `error`: a label reached from an audit entry may well have been deleted since
 * — the log records the deletion — and "this no longer exists" is a different
 * message from "could not be loaded".
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; label: Label }

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
 * from the outset, as on the library — and picking one raises the library's own
 * floating batch bar, so the full set of batch actions (add to album, add/remove
 * labels, favorite, archive, download, stack, the full editor) is available here
 * too. Dropping this very label is one of them, after which the grid refetches,
 * since the edit may have taken photos out of the label.
 */
export function LabelDetailPage() {
  const { t } = useTranslation()
  const location = useLocation()
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
  // Where the grid was left, per view, so opening a photo and coming back — Back,
  // or the viewer's own "back to list", which pops the same entry — returns to
  // the tile it was opened from. This list only ever grew by appending pages, so
  // it also has to come back as long as it was before the offset means anything.
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey, initialCount: restoreCount },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  const bulk = useBulkEdit({ onEdited: reload, hoverSelect: true })
  const selection = bulk.selection
  const selecting = selection.count > 0

  // Select every tile that has paged in, matching the library's select-all: it
  // never reaches beyond what the grid has actually loaded.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((p) => p.uid))
  }, [photos, selection])

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
        setState({ status: isNotFound(err) ? 'missing' : 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  if (state.status === 'missing') {
    return (
      <ErrorState
        title={t('labelDetail.missing')}
        hint={t('labelDetail.missingHint')}
        action={<BackLink to={LABELS_PATH} label={t('labelDetail.back')} />}
      />
    )
  }

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
        {/* `kk-min-w-0`: a label name is user data and can be one long unbroken
            word — the group must be allowed to shrink so the title wraps inside
            the header instead of widening the page. */}
        <div className="d-flex align-items-center gap-2 flex-wrap kk-min-w-0">
          <BackLink to={LABELS_PATH} label={t('labelDetail.back')} />
          <h1 className="kk-page-title mb-0">{title}</h1>
        </div>
        {/* The label's own actions stay put during a selection: the batch bar
            floats over the bottom edge and never contends with the header. */}
        {hasPhotos && (
          <div className="d-flex gap-1 flex-wrap">
            <SlideshowStart scope={scope} view={view} count={total} />
          </div>
        )}
      </div>

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('labelDetail.empty.title')} hint={t('labelDetail.empty.hint')} />
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
