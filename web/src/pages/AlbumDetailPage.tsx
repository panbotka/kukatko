import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { type ListRange, type VirtuosoGridHandle } from 'react-virtuoso'

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
import { type TimelineJump, TimelineScrubber } from '../components/library/TimelineScrubber'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { type BatchExtraAction, BatchActionBar } from '../components/organize/BatchActionBar'
import { DownloadZipButton } from '../components/organize/DownloadZipButton'
import { SlideshowStart } from '../components/slideshow/SlideshowStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useWindowedPhotos } from '../hooks/useWindowedPhotos'
import { detailQueryString } from '../lib/detailView'
import { gridScrollKey } from '../lib/gridScroll'
import { ALBUM_DEFAULTS, ALBUM_SORTS, type LibraryView, viewToParams } from '../lib/libraryView'
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
 * Query param holding the month (`YYYY-MM`) the grid is positioned at — the same
 * key the library uses, meaning the same thing, so a jump survives Back and a
 * shared link. It is not part of the view state: it filters nothing, so it must
 * not reach the API, and dropping it whenever a filter or the order changes
 * (which the urlState writer does by itself, writing only the view's own keys) is
 * exactly right, because either renumbers every position.
 */
const ANCHOR_PARAM = 'at'

/**
 * How much time an album has to span before it is given a timeline rail: two
 * years, in months. Below it the rail would be a scale with almost nothing on it
 * — a wedding, a holiday, one afternoon — and it costs a strip of the screen and
 * the tap targets under it. Above it the album is a stretch of history that
 * scrolling alone cannot cross: the one this was written for holds 781 photos
 * from 1910 to 2026.
 */
const TIMELINE_MIN_MONTHS = 24

/**
 * An album's detail page: a header (title, description, count, private badge,
 * back link) with editor controls (rename/delete via modal), above the photo grid
 * scoped to the album. An album is always presented chronologically — the backend
 * pins the sort key to capture time, with the upload time standing in for undated
 * photos — so the only ordering choice is the direction, and the shared
 * {@link FilterBar} offers exactly those two ({@link ALBUM_SORTS}), resting at
 * oldest-first. That, the filters, and the position on the timeline all live in
 * the URL. Editors can select photos to remove from the album, set one as the
 * cover or bulk-edit their metadata, and rename or delete the album. Mutation
 * controls are hidden from viewers.
 *
 * An album that spans years gets the library's own {@link TimelineScrubber}
 * beside its grid ({@link TIMELINE_MIN_MONTHS}). For that the grid is a *window*
 * over the album (`useWindowedPhotos`), not a growing prefix of it: it is as tall
 * as the whole album from the first response and fetches the pages under the
 * viewport, so jumping to 1936 in an album of 781 photos costs one scroll and one
 * request instead of paging through everything in between.
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

  const [urlView, setView] = useUrlState<LibraryView>(ALBUM_DEFAULTS)
  // An album offers two orders, not the library's six ({@link ALBUM_SORTS}), so a
  // sort key from elsewhere — a stale link, a hand-typed URL — is read as the
  // album's own default rather than left to sit in a selector that cannot show
  // it. Sanitised once, here, so the grid, the selector and the links out of this
  // page cannot disagree about which order the reader is in.
  const view = useMemo<LibraryView>(
    () =>
      ALBUM_SORTS.includes(urlView.sort) ? urlView : { ...urlView, sort: ALBUM_DEFAULTS.sort },
    [urlView],
  )
  // The album scope rides in the params themselves, so the grid, the timeline
  // and the slideshow all ask the backend the same question.
  const params = useMemo(() => ({ ...viewToParams(view), album: uid }), [view, uid])
  const scope = useMemo(() => ({ album: uid }), [uid])
  // Each tile carries the album scope so the detail page pages prev/next within
  // this album and Esc/Back returns to it, not the whole library.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, album: uid, label: '', favorite: '', mode: '' }),
    [view, uid],
  )
  // Where the grid was left, per view, so opening a photo and coming back — Back,
  // or the viewer's own "back to list", which pops the same entry — returns to
  // the tile it was opened from. The windowed grid is as tall as the whole album
  // from its first response, so the offset alone restores it: there is no loaded
  // length to catch up to first.
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const { photos, total, status, moreError, ensureRange, retry } = useWindowedPhotos(params, {
    reloadKey,
  })
  const gridScroll = useGridScrollMemory({ key: scrollKey })

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
  // the library's: it never reaches beyond what the grid has actually loaded,
  // which on a windowed list is the pages around the reader's position.
  const selectAllInView = useCallback(() => {
    selection.selectMany(photos.filter((p) => p !== undefined).map((p) => p.uid))
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

  // Timeline wiring, the library's: a ref to scroll the grid, the first visible
  // index to highlight the current month, and a jump straight to a month's
  // absolute index. The rail is hidden while a selection is being gathered — it
  // overlays the right edge, where the tiles' own controls are — and it hides
  // itself on an album too short in time to need one.
  const gridRef = useRef<VirtuosoGridHandle>(null)
  // The grid's wrapper, which is where the fixed rail starts on a phone: an album
  // page has a header of its own above the grid, and its height is not a constant
  // the rail may assume (the description wraps, the cover row comes and goes).
  const gridWrapRef = useRef<HTMLDivElement>(null)
  const [rangeStart, setRangeStart] = useState(0)
  const onRangeChanged = useCallback(
    (range: ListRange) => {
      setRangeStart(range.startIndex)
      ensureRange(range.startIndex, range.endIndex)
    },
    [ensureRange],
  )
  const [searchParams, setSearchParams] = useSearchParams()
  const anchor = searchParams.get(ANCHOR_PARAM) ?? ''
  const showScrubber = !selecting
  const jumpTo = useCallback(
    (jump: TimelineJump) => {
      // Start the fetch before the scroll: both are one request/one frame, and
      // overlapping them is what keeps the placeholders on screen briefest.
      ensureRange(jump.index, jump.index)
      gridRef.current?.scrollToIndex({ index: jump.index, align: 'start' })
      if ((searchParams.get(ANCHOR_PARAM) ?? '') === jump.month) {
        return
      }
      const next = new URLSearchParams(searchParams)
      next.set(ANCHOR_PARAM, jump.month)
      setSearchParams(next, { replace: jump.replace })
    },
    [ensureRange, searchParams, setSearchParams],
  )

  // Named after the album it shows, so two open albums are told apart in the tab
  // strip and in history — under the same display name the heading uses, not the
  // raw machine-made title. It sits above the early returns below because a hook
  // may not be called conditionally; until the album loads there is no name to
  // give, and the tab falls back to the bare app name.
  useDocumentTitle(
    state.status === 'ready'
      ? t('documentTitle.album', { name: albumDisplayTitle(state.album.title, i18n.language) })
      : null,
  )

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

      {/* What the album actually is, in the words of whoever made it. It was
          stored, editable and returned by the API, and shown to nobody. It sits
          under the heading, before the controls, because it is the answer to
          "what am I looking at" — and it is the album's own prose, so it keeps
          its line breaks and is allowed to be as long as it was written. */}
      {album != null && album.description !== '' && (
        <p className="kk-prose-note text-secondary mb-3">{album.description}</p>
      )}

      {actionError && <Alert variant="danger">{t('albumDetail.actionError')}</Alert>}

      {/* An album is always chronological — the backend pins the sort key — so
          the selector offers the two directions and nothing else. */}
      <FilterBar view={view} onChange={setView} total={total} sortOptions={ALBUM_SORTS} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('albumDetail.empty.title')} hint={t('albumDetail.empty.hint')} />
      )}

      {status === 'ready' && photos.length > 0 && (
        <>
          <div
            ref={gridWrapRef}
            // On a phone the fixed timeline rail has no page margin to hang in,
            // so the grid gives it a lane of its own along the right edge (CSS,
            // phone widths only) — without it the rail lies over the right
            // column's tiles and takes the taps meant for them.
            className={showScrubber ? 'kukatko-grid-timeline-lane' : undefined}
            // Keep the last rows scrollable clear of the floating bar while a
            // selection is active, so nothing hides behind it.
            style={{ paddingBottom: selecting ? 'var(--kk-batch-clearance)' : undefined }}
          >
            <PhotoGrid
              photos={photos}
              // A windowed list has no "end" to reach: it loads from what is on
              // screen (`onRangeChanged`), so nothing appends and the footer only
              // ever has to offer a retry.
              loadingMore={false}
              moreError={moreError}
              onRetry={retry}
              selection={bulk.gridSelection}
              detailQuery={detailQuery}
              gridRef={gridRef}
              onRangeChanged={onRangeChanged}
              restoreStateFrom={gridScroll.restoreFrom}
              onStateChanged={gridScroll.onStateChanged}
            />
          </div>
          {showScrubber && (
            <TimelineScrubber
              params={params}
              activeIndex={rangeStart}
              gridWrapRef={gridWrapRef}
              anchor={anchor}
              minSpanMonths={TIMELINE_MIN_MONTHS}
              onJump={jumpTo}
            />
          )}
        </>
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
