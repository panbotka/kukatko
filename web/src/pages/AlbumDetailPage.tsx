import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { ReorderableGrid } from '../components/organize/ReorderableGrid'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import {
  hasActiveFilters,
  LIBRARY_DEFAULTS,
  type LibraryView,
  viewToParams,
} from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import {
  type Album,
  deleteAlbum,
  fetchAlbum,
  removeAlbumPhotos,
  reorderAlbumPhotos,
  updateAlbum,
} from '../services/organize'
import { type Photo } from '../services/photos'

/** Fetch lifecycle of the album record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; album: Album }

/**
 * An album's detail page: a header (title, count, private badge, back link) with
 * editor controls (rename/delete via modal, reorder, select), above the photo
 * grid scoped to the album. Filters and sort live in the URL (shared
 * {@link FilterBar} + urlState). Editors can reorder the album by dragging (or
 * the per-tile controls) — persisted via `PATCH /albums/{uid}/order` — select
 * photos to remove from the album, set one as the cover or bulk-edit their
 * metadata, and rename or delete the album. Mutation controls are hidden from
 * viewers.
 *
 * The page is in exactly one of three modes: browsing, reordering (`reordering`)
 * or selecting (`selection.active`); entering one leaves the others.
 */
export function AlbumDetailPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const navigate = useNavigate()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState(false)
  const [reordering, setReordering] = useState(false)
  const [reloadKey, setReloadKey] = useState('0')
  const [reorderOrder, setReorderOrder] = useState<Photo[]>([])
  const [actionError, setActionError] = useState(false)

  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ album: uid }), [uid])
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
    fetchAlbum(uid, controller.signal)
      .then((album) => {
        setState({ status: 'ready', album })
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

  const enterReorder = useCallback(() => {
    selection.disable()
    setReorderOrder(photos)
    setReordering(true)
  }, [photos, selection])

  const enterSelect = useCallback(() => {
    setReordering(false)
    selection.enable()
  }, [selection])

  const leaveMode = useCallback(() => {
    selection.disable()
    setReordering(false)
  }, [selection])

  const persistOrder = useCallback(
    async (orderedUids: string[]) => {
      // Reflect the new order immediately, then persist it.
      setReorderOrder((prev) => {
        const byUid = new Map(prev.map((p) => [p.uid, p]))
        return orderedUids.map((id) => byUid.get(id)).filter((p): p is Photo => p !== undefined)
      })
      setActionError(false)
      try {
        await reorderAlbumPhotos(uid, orderedUids)
      } catch {
        setActionError(true)
      }
    },
    [uid],
  )

  const removeSelected = useCallback(async () => {
    const uids = [...selection.selected]
    if (uids.length === 0) {
      return
    }
    setActionError(false)
    try {
      await removeAlbumPhotos(uid, uids)
      // Leave selection mode before reloading: the removed photos vanish from the
      // grid, and a selection still holding their UIDs would send them to the
      // next action. A failed removal keeps the selection so it can be retried.
      leaveMode()
      reload()
    } catch {
      setActionError(true)
    }
  }, [selection.selected, uid, leaveMode, reload])

  const setCover = useCallback(async () => {
    if (state.status !== 'ready' || selection.count !== 1) {
      return
    }
    const [photoUid] = [...selection.selected]
    const album = state.album
    setActionError(false)
    try {
      const updated = await updateAlbum(album.uid, {
        title: album.title,
        description: album.description,
        private: album.private,
        order_by: album.order_by,
        cover_photo_uid: photoUid,
      })
      setState({ status: 'ready', album: updated })
      leaveMode()
    } catch {
      setActionError(true)
    }
  }, [state, selection, leaveMode])

  const removeAlbum = useCallback(async () => {
    if (state.status !== 'ready') {
      return
    }
    if (!window.confirm(t('albumDetail.confirmDelete', { name: state.album.title }))) {
      return
    }
    setActionError(false)
    try {
      await deleteAlbum(state.album.uid)
      void navigate('/albums')
    } catch {
      setActionError(true)
    }
  }, [state, navigate, t])

  if (state.status === 'error') {
    return (
      <Alert variant="danger">
        {t('albumDetail.error')} <Link to="/albums">{t('albumDetail.back')}</Link>
      </Alert>
    )
  }

  const album = state.status === 'ready' ? state.album : null
  // Reorder makes sense only on the album's own order — disable it when filters
  // or a non-default sort would make the on-screen order not the album order.
  const canReorder = !hasActiveFilters(view) && view.sort === LIBRARY_DEFAULTS.sort

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <Link to="/albums" className="text-decoration-none">
            ← {t('albumDetail.back')}
          </Link>
          <h1 className="kk-page-title mb-0">{album?.title ?? ''}</h1>
          {album?.private && <Badge bg="secondary">{t('albums.private')}</Badge>}
        </div>
        {album && !reordering && !selection.active && (
          <div className="d-flex gap-1 flex-wrap">
            {photos.length > 0 && <SlideshowStart scope={scope} view={view} count={total} />}
            {canWrite && (
              <>
                <Button
                  variant="outline-secondary"
                  size="sm"
                  onClick={() => {
                    setEditing(true)
                  }}
                >
                  {t('albumDetail.edit')}
                </Button>
                <Button variant="outline-secondary" size="sm" onClick={enterSelect}>
                  {t('albumDetail.select')}
                </Button>
                {canReorder && (
                  <Button variant="outline-secondary" size="sm" onClick={enterReorder}>
                    {t('albumDetail.reorder')}
                  </Button>
                )}
                <Button variant="outline-danger" size="sm" onClick={() => void removeAlbum()}>
                  {t('albumDetail.delete')}
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      {actionError && <Alert variant="danger">{t('albumDetail.actionError')}</Alert>}

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={leaveMode}>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={selection.count !== 1}
            onClick={() => void setCover()}
          >
            {t('albumDetail.setCover')}
          </Button>
          <BulkEditControl bulk={bulk} />
          <Button
            variant="danger"
            size="sm"
            disabled={selection.count === 0}
            onClick={() => void removeSelected()}
          >
            {t('albumDetail.removeSelected')}
          </Button>
        </SelectionBar>
      )}

      {reordering && (
        <div className="d-flex align-items-center gap-2 mb-3">
          <span className="text-secondary small me-auto">{t('albumDetail.reorderHint')}</span>
          <Button variant="primary" size="sm" onClick={leaveMode}>
            {t('albumDetail.reorderDone')}
          </Button>
        </div>
      )}

      {!reordering && <FilterBar view={view} onChange={setView} total={total} />}

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
        <EmptyState title={t('albumDetail.empty.title')} hint={t('albumDetail.empty.hint')} />
      )}

      {status === 'ready' && photos.length > 0 && reordering && (
        <ReorderableGrid photos={reorderOrder} onReorder={(uids) => void persistOrder(uids)} />
      )}

      {status === 'ready' && photos.length > 0 && !reordering && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
          selection={bulk.gridSelection}
        />
      )}

      {canWrite && album && (
        <AlbumEditModal
          album={album}
          show={editing}
          onHide={() => {
            setEditing(false)
          }}
          onSaved={(updated) => {
            setState({ status: 'ready', album: updated })
            setEditing(false)
          }}
        />
      )}
    </>
  )
}
