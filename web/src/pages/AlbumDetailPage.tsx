import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { albumDisplayTitle } from '../i18n/albumNames'
import { BackLink } from '../components/BackLink'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { HeaderActions } from '../components/HeaderActions'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { type BatchExtraAction, BatchActionBar } from '../components/organize/BatchActionBar'
import { DownloadZipButton } from '../components/organize/DownloadZipButton'
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
import {
  type Album,
  deleteAlbum,
  fetchAlbum,
  removeAlbumPhotos,
  updateAlbum,
} from '../services/organize'

/**
 * Fetch lifecycle of the album record. `missing` is a 404 kept apart from
 * `error`: an album reached from an audit entry may well have been deleted since
 * — the log records the deletion — and "this no longer exists" is a different
 * message from "could not be loaded".
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; album: Album }

/**
 * Where the back link leads. The albums index keeps no view state of its own in
 * the URL, so the bare route restores it exactly; should it ever grow filters,
 * this is the one place that has to carry them.
 */
const ALBUMS_PATH = '/albums'

/**
 * An album's detail page: a header (title, count, private badge, back link) with
 * editor controls (rename/delete via modal), above the photo grid
 * scoped to the album. An album is always presented chronologically (oldest
 * capture first, upload time standing in for undated photos), so the page
 * renders no sort selector; filters live in the URL (shared {@link FilterBar} +
 * urlState). Editors can select photos to remove from the album, set one as the
 * cover or bulk-edit their metadata, and rename or delete the album. Mutation
 * controls are hidden from viewers.
 *
 * A writer's tiles offer the corner checkmark from the outset (hover-select, as
 * on the library), and picking the first photo raises the same floating batch
 * bar the library uses — with the album's own actions (set cover, remove from
 * album) merged into it, so the page never shows two competing toolbars.
 */
export function AlbumDetailPage() {
  const { t, i18n } = useTranslation()
  const { canWrite } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
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
        setState({ status: isNotFound(err) ? 'missing' : 'error' })
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

  // Select every tile that has paged in — the album's own select-all, matching
  // the library's: it never reaches beyond what the grid has actually loaded.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.map((p) => p.uid))
  }, [photos, selection])

  // The album's own batch actions, merged into the shared bar rather than shown
  // on a toolbar of their own. A cover is a single photo, so that action waits
  // for a selection of exactly one.
  const extraActions = useMemo<BatchExtraAction[]>(
    () => [
      {
        id: 'set-cover',
        icon: 'image',
        label: t('albumDetail.setCover'),
        disabled: selection.count !== 1,
        onClick: () => void setCover(),
      },
      {
        id: 'remove-from-album',
        icon: 'dash-lg',
        label: t('albumDetail.removeSelected'),
        danger: true,
        onClick: () => void removeSelected(),
      },
    ],
    [t, selection.count, setCover, removeSelected],
  )

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

  if (state.status === 'missing') {
    return (
      <ErrorState
        title={t('albumDetail.missing')}
        hint={t('albumDetail.missingHint')}
        action={<BackLink to={ALBUMS_PATH} label={t('albumDetail.back')} />}
      />
    )
  }

  if (state.status === 'error') {
    return (
      <ErrorState
        title={t('albumDetail.error')}
        action={<BackLink to={ALBUMS_PATH} label={t('albumDetail.back')} />}
      />
    )
  }

  const album = state.status === 'ready' ? state.album : null

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        {/* `kk-min-w-0`: an album title is user data and can be one long
            unbroken word — the group must be allowed to shrink so the title
            wraps inside the header instead of widening the page. */}
        <div className="d-flex align-items-center gap-2 flex-wrap kk-min-w-0">
          <BackLink to={ALBUMS_PATH} label={t('albumDetail.back')} />
          <h1 className="kk-page-title mb-0">
            {album == null ? '' : albumDisplayTitle(album.title, i18n.language)}
          </h1>
          {album?.private && <Badge bg="secondary">{t('albums.private')}</Badge>}
        </div>
        {/* The album's own controls stay put during a selection: the batch bar
            floats over the bottom edge and never contends with the header.
            Slideshow is the one action that stays inline on a phone; the rest —
            and the destructive delete behind its own divider — fold into the
            shared "…" overflow menu, so the header keeps to a single row. */}
        {album && (
          <HeaderActions
            id="album-actions-overflow"
            primary={[
              photos.length > 0 && (
                <SlideshowStart key="slideshow" scope={scope} view={view} count={total} />
              ),
            ]}
            secondary={[
              total > 0 && (
                <DownloadZipButton
                  key="download"
                  albumUid={uid}
                  name={album.title}
                  variant="outline-secondary"
                />
              ),
              canWrite && (
                <Button
                  key="edit"
                  variant="outline-secondary"
                  size="sm"
                  onClick={() => {
                    setEditing(true)
                  }}
                >
                  {t('albumDetail.edit')}
                </Button>
              ),
            ]}
            destructive={[
              canWrite && (
                <Button
                  key="delete"
                  variant="outline-danger"
                  size="sm"
                  onClick={() => {
                    setPendingDelete(true)
                  }}
                >
                  {t('albumDetail.delete')}
                </Button>
              ),
            ]}
          />
        )}
      </div>

      {actionError && <Alert variant="danger">{t('albumDetail.actionError')}</Alert>}

      {/* Albums are always chronological; the shared FilterBar hides its sort
          selector here because the backend pins the album order server-side. */}
      <FilterBar view={view} onChange={setView} total={total} showSort={false} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('albumDetail.empty.title')} hint={t('albumDetail.empty.hint')} />
      )}

      {status === 'ready' && photos.length > 0 && (
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
        <BatchActionBar bulk={bulk} onSelectAll={selectAllInView} extraActions={extraActions} />
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
