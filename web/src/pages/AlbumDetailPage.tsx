import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { DownloadZipButton } from '../components/organize/DownloadZipButton'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SelectionStart } from '../components/organize/SelectionStart'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { detailQueryString } from '../lib/detailView'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import {
  type Album,
  deleteAlbum,
  fetchAlbum,
  removeAlbumPhotos,
  updateAlbum,
} from '../services/organize'

/** Fetch lifecycle of the album record. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; album: Album }

/**
 * Where the back link leads. The albums index keeps no view state of its own in
 * the URL, so the bare route restores it exactly; should it ever grow filters,
 * this is the one place that has to carry them.
 */
const ALBUMS_PATH = '/albums'

/**
 * An album's detail page: a header (title, count, private badge, back link) with
 * editor controls (rename/delete via modal, select), above the photo grid
 * scoped to the album. An album is always presented chronologically (oldest
 * capture first, upload time standing in for undated photos), so the page
 * renders no sort selector; filters live in the URL (shared {@link FilterBar} +
 * urlState). Editors can select photos to remove from the album, set one as the
 * cover or bulk-edit their metadata, and rename or delete the album. Mutation
 * controls are hidden from viewers.
 *
 * The page is either browsing or selecting (`selection.active`).
 */
export function AlbumDetailPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const navigate = useNavigate()
  const { uid = '' } = useParams<{ uid: string }>()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState(false)
  const [pendingDelete, setPendingDelete] = useState(false)
  const [reloadKey, reload] = useReloadKey()
  const [actionError, setActionError] = useState(false)

  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ album: uid }), [uid])
  // Each tile carries the album scope so the detail page pages prev/next within
  // this album and Esc/Back returns to it, not the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, album: uid, label: '', favorite: '', mode: '' }),
    [view, uid],
  )
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { reloadKey },
  )

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

  const leaveMode = useCallback(() => {
    selection.disable()
  }, [selection])

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
    setActionError(false)
    try {
      await deleteAlbum(state.album.uid)
      void navigate('/albums')
    } catch {
      setActionError(true)
    }
  }, [state, navigate])

  if (state.status === 'error') {
    return (
      <Alert variant="danger" className="d-flex align-items-center gap-3 flex-wrap">
        <span>{t('albumDetail.error')}</span>
        <BackLink to={ALBUMS_PATH} label={t('albumDetail.back')} />
      </Alert>
    )
  }

  const album = state.status === 'ready' ? state.album : null

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <BackLink to={ALBUMS_PATH} label={t('albumDetail.back')} />
          <h1 className="kk-page-title mb-0">{album?.title ?? ''}</h1>
          {album?.private && <Badge bg="secondary">{t('albums.private')}</Badge>}
        </div>
        {album && !selection.active && (
          <div className="d-flex gap-1 flex-wrap">
            {photos.length > 0 && <SlideshowStart scope={scope} view={view} count={total} />}
            {total > 0 && (
              <DownloadZipButton albumUid={uid} name={album.title} variant="outline-secondary" />
            )}
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
                <SelectionStart bulk={bulk} />
                <Button
                  variant="outline-danger"
                  size="sm"
                  onClick={() => {
                    setPendingDelete(true)
                  }}
                >
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

      {/* Albums are always chronological; the shared FilterBar hides its sort
          selector here because the backend pins the album order server-side. */}
      <FilterBar view={view} onChange={setView} total={total} showSort={false} />

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

      {status === 'ready' && photos.length > 0 && (
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

      {canWrite && album && (
        <ConfirmModal
          show={pendingDelete}
          title={t('albumDetail.confirmTitle')}
          confirmLabel={t('albumDetail.deleteConfirm')}
          onCancel={() => {
            setPendingDelete(false)
          }}
          onConfirm={() => {
            setPendingDelete(false)
            void removeAlbum()
          }}
        >
          {t('albumDetail.confirmDelete', { name: album.title })}
        </ConfirmModal>
      )}
    </>
  )
}
