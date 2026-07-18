import { type CSSProperties, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { FavoriteToggle } from '../components/library/FavoriteButton'
import { FlagControl } from '../components/library/FlagControl'
import { RatingStars } from '../components/library/RatingStars'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { EditPanel } from '../components/photo/EditPanel'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PeoplePanel } from '../components/photo/PeoplePanel'
import { StackStrip } from '../components/photo/StackStrip'
import { TechnicalDetails } from '../components/photo/TechnicalDetails'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import './../components/photo/viewer.css'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { FacesPanel } from '../components/people/FacesPanel'
import { useAutoHideChrome } from '../hooks/useAutoHideChrome'
import { useFaces } from '../hooks/useFaces'
import { useFavorite } from '../hooks/useFavorite'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { usePinchZoom } from '../hooks/usePinchZoom'
import { useRating } from '../hooks/useRating'
import { useSwipeNavigation } from '../hooks/useSwipeNavigation'
import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from '../lib/detailView'
import { readFaceOverlay, writeFaceOverlay } from '../lib/faceOverlayPref'
import { formatDateTimeMinutes } from '../lib/format'
import { editPreviewStyle, editTransform, isIdentityEdit, NEUTRAL_EDIT } from '../lib/photoEdit'
import { photoDisplayTitle, photoTitleText, titleSource } from '../lib/photoTitle'
import { isTypingElement, ratingHotkey } from '../lib/ratingHotkeys'
import { toMode } from '../lib/searchView'
import { readUrlState } from '../lib/urlState'
import {
  downloadUrl,
  fetchEdit,
  fetchPhoto,
  type PhotoDetail,
  type PhotoEdit,
  setStackPrimary,
  thumbUrl,
  unstackAll,
  unstackMember,
} from '../services/photos'

/** Preview size for the viewer stage: a large fit-to-box preview, not a tile. */
const PREVIEW_SIZE = 'fit_1920'

/** Fetch lifecycle of the photo detail (the photo and its stored edit). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; photo: PhotoDetail; edit: PhotoEdit }

/**
 * The special slot at the top of the info drawer, or null for none. Faces and
 * edits both want that slot (and both alter what may be drawn over the photo), so
 * this single choice — rather than a boolean each — makes them fighting over it
 * unrepresentable: at most one is active, never both.
 */
type SidePanel = 'faces' | 'edit' | null

/**
 * The immersive full-bleed photo viewer, and the `/photos/:uid` route itself.
 * Opening a photo drops the whole viewport into a distraction-free viewer: the
 * image is centered and scaled to the largest fit without cropping over a warm
 * near-black backdrop, reflecting the saved non-destructive edit (or, while the
 * edit panel is open, the adjustments in progress). The chrome — a top action
 * bar and the prev/next arrows — melts away after a short idle and returns on any
 * pointer move or tap; a persistent close (Esc / ✕) always works and steps back
 * to the originating list at its exact prior scroll position.
 *
 * All the rich metadata and curation — caption & place (EXIF, date, location with
 * its map), people/faces, albums & labels, technical details, the variants stack,
 * similar photos and the non-destructive editor — live in a metadata drawer that
 * slides in from the side on demand rather than being always visible; the default
 * state is just the photo. The open photo and the drawer's open state both live
 * in URL params, so Back and refresh behave. Every mutation is role-gated; viewers
 * get a read-only viewer.
 */
export function PhotoDetailPage() {
  const { t, i18n } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: 'loading' })
  // Bumped after a thumbnail is regenerated: the derived image changed under a
  // stable URL, so appending this counter forces the browser to refetch it.
  const [thumbVersion, setThumbVersion] = useState(0)
  // Which special slot — if any — leads the info drawer. Seeded from the stored
  // face-overlay choice, which is read once from localStorage and written back on
  // every faces toggle, so it carries across photos and reloads. Faces are off by
  // default: the photo is the content, the boxes and their panel are opt-in.
  const [sidePanel, setSidePanel] = useState<SidePanel>(() => (readFaceOverlay() ? 'faces' : null))
  // The adjustments the edit panel is working on, or null for "nothing unsaved".
  // The viewer owns them because the preview surface is the ONE photo on stage:
  // the panel reports every slider move up here, and the photo re-renders with it.
  const [editDraft, setEditDraft] = useState<PhotoEdit | null>(null)
  // The face hovered on either side of the photo/panel pair, so hovering a box
  // highlights its row and hovering a row highlights its box.
  const [hoveredFace, setHoveredFace] = useState<number | null>(null)
  const faces = useFaces(uid)

  const view = useMemo(() => readUrlState(searchParams, DETAIL_DEFAULTS), [searchParams])
  const neighborParams = useMemo(() => detailToParams(view), [view])
  const detailQuery = detailQueryString(view)
  // A `mode` scope means the photo was opened from search, so prev/next must page
  // through `GET /search` in the same ranked order the results grid showed rather
  // than the plain library list.
  const searchMode = view.mode !== '' ? toMode(view.mode) : undefined
  const neighbors = usePhotoNeighbors(uid, neighborParams, true, searchMode)

  // The info drawer's open state lives in a URL param (`info`), so it is
  // deep-linkable and survives Back/refresh. It is deliberately NOT part of the
  // DetailView (DETAIL_DEFAULTS), so it never leaks into the neighbour params or
  // the Back link — it is a view of THIS photo, not a filter of the list.
  const panelOpen = searchParams.get('info') === '1'
  const setPanelOpen = (open: boolean): void => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (open) {
          next.set('info', '1')
        } else {
          next.delete('info')
        }
        return next
      },
      { replace: true },
    )
  }

  // Whether we can step back through history to restore the grid's exact scroll
  // position. Captured once at mount (paging replaces the entry, so a later key
  // change must not flip this): the initial entry's key is `default` only when the
  // photo was loaded directly (a deep link, a refresh, a shared URL), in which case
  // there is no grid entry to pop and Back must reconstruct the list URL instead.
  const openedDirectlyRef = useRef(location.key === 'default')

  // The neighbour's detail URL, carrying the originating order/scope so prev/next
  // keeps paging the same list, plus the drawer's open state so it stays open (or
  // shut) as you step through photos.
  const neighborTo = (neighbor: string): string => {
    const params = new URLSearchParams(detailQuery)
    if (panelOpen) {
      params.set('info', '1')
    }
    const query = params.toString()
    return query === '' ? `/photos/${neighbor}` : `/photos/${neighbor}?${query}`
  }

  // Page to a neighbour (prev/next) preserving the originating list order. Shared
  // by the on-image ‹/› arrows, the ←/→ keys and the touch swipe/pinch so all
  // navigate identically (same URL/state, same stop-at-ends semantics). Replace,
  // so paging never grows the history stack the close button pops.
  const goToNeighbor = (neighbor: string | null): void => {
    if (neighbor !== null) {
      void navigate(neighborTo(neighbor), { replace: true })
    }
  }

  // Close the viewer, returning to the originating list. Prefer stepping back
  // through history (`navigate(-1)`) so the browser restores the grid's exact
  // scroll position; only for a directly-opened photo — where there is no grid
  // entry behind us — do we push the reconstructed list URL instead.
  const close = (): void => {
    if (openedDirectlyRef.current) {
      void navigate(backHref(view))
    } else {
      void navigate(-1)
    }
  }

  // The favorite is lifted here so the header heart and the `f` shortcut share one
  // optimistic toggle. It resyncs to the photo's stored flag once it loads.
  const favorite = useFavorite(
    uid,
    state.status === 'ready' ? (state.photo.is_favorite ?? false) : false,
  )

  // The face/edit UI is derived here, above the loading/error guards below,
  // because the `m` shortcut is registered before them and must see the same
  // booleans the render does. `state` is read without destructuring so it stays
  // legal up here.
  const ready = state.status === 'ready' ? state.photo : null
  // What the one photo previews: the adjustments in progress while the edit panel
  // is open, otherwise the stored edit.
  const previewEdit = editDraft ?? (state.status === 'ready' ? state.edit : NEUTRAL_EDIT)
  // The overlay is only ever drawn over a still image: a video player's chrome is
  // not a photo, and faces are never detected on clips anyway.
  const isStill = ready !== null && ready.media_type !== 'video' && ready.media_type !== 'live'
  // While a neighbour loads the faces are keyed on the target photo, so they must
  // not be drawn over the still-displayed previous one.
  const loadingNext = ready !== null && ready.uid !== uid
  // A non-identity preview rules the whole face UI out: FaceOverlay places its
  // boxes in percentages of the figure that shrink-wraps the <img>, while the
  // preview's transform moves the rendered pixels underneath them — so the boxes
  // would miss the faces. Rather than draw them wrong, the faces stand down.
  const facesAvailable =
    isStill && !loadingNext && faces.faces.length > 0 && isIdentityEdit(previewEdit)
  const showFaces = facesAvailable && sidePanel === 'faces'
  // Edits are for stills only — the backend never re-renders a video edit, and the
  // player carries no preview surface to apply them to.
  const showEdit = canWrite && isStill && sidePanel === 'edit'

  // Faces and edits share the drawer's lead slot, so showing either one closes the
  // other; opening either opens the drawer (their panels live inside it).
  const openFaces = (): void => {
    setSidePanel('faces')
    writeFaceOverlay(true)
    setEditDraft(null)
    setPanelOpen(true)
  }

  // Hiding the faces drops the selection and the overlay, but leaves the drawer
  // open on the metadata — turning faces off is not "close everything".
  const toggleFaces = (): void => {
    if (sidePanel === 'faces') {
      setSidePanel(null)
      writeFaceOverlay(false)
      faces.select(null)
      return
    }
    openFaces()
  }

  // Opens the faces slot at a given face — how the Organize person-chips reach the
  // one place people are named.
  const editFace = (faceIndex: number): void => {
    openFaces()
    faces.select(faceIndex)
  }

  // Opening the edits takes the lead slot from the faces (their boxes cannot be
  // drawn over an edited preview), so the selection is dropped too. Closing
  // discards whatever is unsaved, returning the photo to showing exactly what is
  // stored.
  const toggleEdit = (): void => {
    setSidePanel((prev) => (prev === 'edit' ? null : 'edit'))
    setEditDraft(null)
    faces.select(null)
    setPanelOpen(true)
  }

  // The info button toggles the drawer. Closing it resets the lead slot so a
  // reopened drawer starts on the metadata, and drops the face overlay/selection
  // that only made sense with the panel open.
  const togglePanel = (): void => {
    if (panelOpen) {
      setPanelOpen(false)
      setSidePanel(null)
      setEditDraft(null)
      faces.select(null)
    } else {
      setPanelOpen(true)
    }
  }

  // The chrome (top bar + arrows) melts away after a short idle and returns on any
  // activity — except while the drawer is open, when the actions beside it (and
  // its own toggle) must stay reachable, so it is pinned visible.
  const chrome = useAutoHideChrome({ paused: panelOpen })

  // Touch: horizontal swipe pages when zoom is not in play (faces/edit on, where
  // pinch-zoom is disabled so the boxes/preview stay put). A mostly-vertical drag
  // is ignored (nothing scrolls under a fixed viewer), and the gesture is ignored
  // when it starts on the face boxes or the arrows (see useSwipeNavigation).
  const swipe = useSwipeNavigation({
    enabled: isStill && (showFaces || showEdit),
    onSwipe: (direction) => {
      goToNeighbor(direction === 'next' ? neighbors.next : neighbors.prev)
    },
  })

  // Touch: pinch/double-tap to zoom with drag-to-pan while zoomed, and a swipe to
  // page while at rest. Enabled only on a plain still (no faces overlay, no edit
  // preview) so a magnifying transform never drifts the boxes or fights the edit.
  const zoom = usePinchZoom({
    enabled: isStill && !showFaces && !showEdit,
    resetKey: uid,
    onSwipe: (direction) => {
      if (direction === 'next') {
        goToNeighbor(neighbors.next)
      } else {
        goToNeighbor(neighbors.prev)
      }
    },
  })

  // Viewer shortcuts: ←/→ page, `f` favorite, `m` faces, `i` info drawer, Escape
  // steps back out (a selected face, then the drawer, then the viewer itself).
  // Rating keys (0–5, p/r) are handled by the separate effect below. The hook
  // suppresses these while typing, which keeps `m`/`i` out of the name field.
  useKeyboardShortcuts({
    ArrowLeft: () => {
      goToNeighbor(neighbors.prev)
    },
    ArrowRight: () => {
      goToNeighbor(neighbors.next)
    },
    f: () => {
      favorite.toggle()
    },
    m: () => {
      if (facesAvailable) {
        toggleFaces()
      }
    },
    i: () => {
      togglePanel()
    },
    Escape: () => {
      if (faces.selected !== null) {
        faces.select(null)
        return
      }
      if (panelOpen || sidePanel !== null) {
        setPanelOpen(false)
        setSidePanel(null)
        setEditDraft(null)
        return
      }
      close()
    },
  })

  // The optimistic rating hook (stars + flag) drives both the chrome controls and
  // the number/p/r hotkeys. Instantiated before the loading/error guards (hook
  // rules) and resyncs to the photo's stored values once it loads.
  const initialRating = state.status === 'ready' ? (state.photo.rating ?? 0) : 0
  const initialFlag = state.status === 'ready' ? (state.photo.flag ?? 'none') : 'none'
  const rating = useRating(uid, initialRating, initialFlag)
  const { setRating, setFlag } = rating

  // Number keys 0–5 set the rating, p = pick, r = reject — but never while the
  // user is typing in an input/textarea/contenteditable.
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.metaKey || event.altKey || isTypingElement(event.target)) {
        return
      }
      const action = ratingHotkey(event.key)
      if (action === null) {
        return
      }
      event.preventDefault()
      if (action.kind === 'rating') {
        setRating(action.value)
      } else {
        setFlag(action.value)
      }
    }
    document.addEventListener('keydown', handler)
    return () => {
      document.removeEventListener('keydown', handler)
    }
  }, [setRating, setFlag])

  useEffect(() => {
    const controller = new AbortController()
    // Only blank to the full spinner on the very first load. When a photo is
    // already on screen (prev/next navigation), keep it mounted and fetch the next
    // one in the background, then swap in place — no full-screen flicker. The abort
    // on `uid` change still cancels the superseded request, so the latest target
    // always wins.
    setState((prev) => (prev.status === 'ready' ? prev : { status: 'loading' }))
    // A draft belongs to the photo it was made on: paging to a neighbour drops it.
    setEditDraft(null)
    Promise.all([fetchPhoto(uid, controller.signal), fetchEdit(uid, controller.signal)])
      .then(([photo, edit]) => {
        setState({ status: 'ready', photo, edit })
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

  // Preload the adjacent photos at preview size so stepping feels instant.
  useEffect(() => {
    for (const neighbor of [neighbors.prev, neighbors.next]) {
      if (neighbor !== null) {
        const img = new Image()
        img.src = thumbUrl(neighbor, PREVIEW_SIZE, downloadToken ?? undefined)
      }
    }
  }, [neighbors.prev, neighbors.next, downloadToken])

  if (state.status === 'loading') {
    return (
      <div className="kk-viewer" data-chrome="visible">
        <button
          type="button"
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__close"
          aria-label={t('photo.back')}
          onClick={close}
        >
          <Icon name="x-lg" />
        </button>
        <div className="kk-viewer__stage d-flex justify-content-center align-items-center">
          <Spinner animation="border" role="status" variant="light">
            <span className="visually-hidden">{t('photo.loading')}</span>
          </Spinner>
        </div>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <div className="kk-viewer" data-chrome="visible">
        <div className="kk-viewer__stage">
          <Alert variant="danger" className="d-flex align-items-center gap-3 flex-wrap m-0">
            <span>{t('photo.error')}</span>
            <Button variant="outline-light" size="sm" onClick={close}>
              {t('photo.back')}
            </Button>
          </Alert>
        </div>
      </div>
    )
  }

  const { photo, edit } = state
  // What the photo is called, as a person would say it: its title, or the facts it
  // carries (when, and where). Never the filename — that is the camera's name for
  // it and lives in the technical details.
  const captureDate =
    photo.taken_at !== undefined ? formatDateTimeMinutes(photo.taken_at, i18n.language) : ''
  const displayTitle = photoDisplayTitle(titleSource(photo), captureDate)
  // The one-string form, for alt text and the players' titles.
  const title = photoTitleText(displayTitle, t('photo.untitled'))

  const setPhoto = (updated: PhotoDetail): void => {
    setState({ status: 'ready', photo: updated, edit })
  }
  // Stack mutations always refresh the photo being viewed (not the member that was
  // mutated), so the variants strip and the member-count reflect the change.
  const reloadPhoto = async (): Promise<void> => {
    setPhoto(await fetchPhoto(uid))
  }
  const handleSetStackPrimary = async (memberUid: string): Promise<void> => {
    await setStackPrimary(memberUid)
    await reloadPhoto()
  }
  const handleUnstackMember = async (memberUid: string): Promise<void> => {
    await unstackMember(memberUid)
    await reloadPhoto()
  }
  const handleUnstackAll = async (): Promise<void> => {
    await unstackAll(uid)
    await reloadPhoto()
  }
  // A saved edit becomes the stored one and clears the draft, so the photo keeps
  // previewing the very same adjustments — now from `state` rather than in flight.
  const onEditSaved = (saved: PhotoEdit): void => {
    setState({ status: 'ready', photo, edit: saved })
    setEditDraft(null)
  }
  // The panel reports an updater, not a finished edit, so adjustments made in the
  // same React batch compose instead of overwriting each other. The first one has
  // no draft to build on yet, so it starts from the stored edit.
  const applyEdit = (update: (prev: PhotoEdit) => PhotoEdit): void => {
    setEditDraft((prev) => update(prev ?? edit))
  }
  const onThumbnailRegenerated = (): void => {
    setThumbVersion((v) => v + 1)
  }

  const basePoster = thumbUrl(photo.uid, PREVIEW_SIZE, downloadToken)
  // The thumb URL is built from the UID (stable), so a regenerated thumbnail would
  // otherwise be masked by the browser cache. Append a version once the user
  // regenerates it, so the new image actually shows without a hard reload.
  const poster =
    thumbVersion > 0
      ? `${basePoster}${basePoster.includes('?') ? '&' : '?'}v=${String(thumbVersion)}`
      : basePoster

  // The still image's style composes the saved edit with the live zoom/pan (only
  // when zoom is enabled — a plain still). Rotate first, then scale/translate the
  // rotated image, matching editPreviewStyle's own transform ordering.
  const stillStyle: CSSProperties = { ...editPreviewStyle(previewEdit) }
  if (isStill && !showFaces && !showEdit) {
    const rotation = editTransform(previewEdit)
    stillStyle.transform = `translate(${String(zoom.translateX)}px, ${String(zoom.translateY)}px) scale(${String(zoom.scale)})${rotation === 'none' ? '' : ` ${rotation}`}`
    stillStyle.transition = zoom.gesturing
      ? 'none'
      : 'transform var(--kk-duration-base) var(--kk-ease-standard)'
    stillStyle.cursor = zoom.isZoomed ? 'grab' : 'default'
  }

  // Render the stage media by kind: a range-streaming player for videos, a
  // hover/hold motion preview for live photos, and the edit-reflecting still for
  // images (with the detected faces drawn as a toggleable overlay on top of it).
  const renderStage = () => {
    if (photo.media_type === 'video') {
      return (
        <div className="kk-viewer__media">
          <VideoPlayer
            uid={photo.uid}
            title={title}
            poster={poster}
            downloadHref={photo.download_url}
            token={downloadToken}
          />
        </div>
      )
    }
    if (photo.media_type === 'live') {
      return (
        <div className="kk-viewer__media">
          <LivePhoto uid={photo.uid} title={title} poster={poster} token={downloadToken} />
        </div>
      )
    }
    return (
      <div
        // Keyed on the DISPLAYED photo (not the route uid): while a neighbour
        // loads, the previous photo stays mounted (no flicker), and the
        // fade/scale replays only once the new photo actually swaps in.
        key={photo.uid}
        className="kk-viewer__figure"
        data-swipe-surface=""
        onTouchStart={(event) => {
          zoom.handlers.onTouchStart(event)
          swipe.onTouchStart(event)
        }}
        onTouchMove={(event) => {
          zoom.handlers.onTouchMove(event)
          swipe.onTouchMove(event)
        }}
        onTouchEnd={(event) => {
          zoom.handlers.onTouchEnd(event)
          swipe.onTouchEnd(event)
        }}
      >
        <img
          className="kk-viewer__image"
          src={poster}
          alt={title}
          style={stillStyle}
          draggable={false}
        />
        {showFaces && (
          <FaceOverlay
            faces={faces.faces}
            selected={faces.selected?.face_index ?? null}
            hovered={hoveredFace}
            onSelect={(faceIndex) => {
              faces.select(faceIndex)
              setPanelOpen(true)
            }}
            onHover={setHoveredFace}
            readOnly={!canWrite}
          />
        )}
      </div>
    )
  }

  return (
    <div
      className="kk-viewer"
      role="dialog"
      aria-modal="true"
      aria-label={t('photo.viewer.label')}
      data-chrome={chrome.visible ? 'visible' : 'hidden'}
      data-panel={panelOpen ? 'open' : 'closed'}
    >
      {/* Persistent close: top-left, never fades with the chrome, so Esc always
          has a visible twin the pointer can find. Returns to the originating list. */}
      <button
        type="button"
        className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__close"
        aria-label={t('photo.back')}
        onClick={close}
      >
        <Icon name="x-lg" />
      </button>

      {/* Auto-hiding top action bar: the photo's name, then the curation loop and
          the drawer toggle. */}
      <div className="kk-viewer__chrome">
        <div className="kk-viewer__heading">
          <h1 className="kk-viewer__title">
            {displayTitle.kind === 'facts' ? (
              <>
                {displayTitle.date !== '' && <span>{displayTitle.date}</span>}
                {displayTitle.place !== '' && (
                  <span className="kk-viewer__title-muted">
                    {displayTitle.date !== '' && <span aria-hidden="true"> · </span>}
                    {displayTitle.place}
                  </span>
                )}
              </>
            ) : (
              title
            )}
          </h1>
        </div>
        <div className="kk-viewer__actions">
          <RatingStars
            rating={rating.rating}
            onRate={rating.setRating}
            disabled={rating.pending}
            size={22}
          />
          <FlagControl
            flag={rating.flag}
            onFlag={rating.setFlag}
            disabled={rating.pending}
            size={18}
          />
          <FavoriteToggle
            favorite={favorite.favorite}
            pending={favorite.pending}
            onToggle={() => {
              favorite.toggle()
            }}
          />
          {facesAvailable && (
            <button
              type="button"
              className="kk-viewer__btn kk-viewer__btn--icon"
              aria-pressed={showFaces}
              aria-label={showFaces ? t('faces.hide') : t('faces.toggle')}
              onClick={toggleFaces}
            >
              <Icon name="person-bounding-box" />
            </button>
          )}
          {canWrite && isStill && (
            <button
              type="button"
              className="kk-viewer__btn kk-viewer__btn--icon"
              aria-pressed={showEdit}
              aria-label={t('photo.edit.title')}
              onClick={toggleEdit}
            >
              <Icon name="sliders" />
            </button>
          )}
          <button
            type="button"
            className="kk-viewer__btn kk-viewer__btn--icon"
            aria-pressed={panelOpen}
            aria-label={t('photo.viewer.info')}
            onClick={togglePanel}
          >
            <Icon name="info-circle" />
          </button>
        </div>
      </div>

      {/* The stage: the photo owns the screen. */}
      <div className="kk-viewer__stage">{renderStage()}</div>

      {/* Paging keeps the current photo visible; a small spinner marks the load. */}
      {loadingNext && (
        <div className="kk-viewer__loading">
          <Spinner animation="border" size="sm" variant="light" role="status">
            <span className="visually-hidden">{t('photo.loadingNext')}</span>
          </Spinner>
        </div>
      )}

      {/* Prev / next: on-image arrows carrying the originating order/scope so the
          list survives navigation, fading with the chrome. Real links (right-click,
          open-in-tab); the ←/→ keys and touch swipe drive the very same URL, and
          `replace` keeps paging from growing the history the close button pops. */}
      {neighbors.prev !== null && (
        <Link
          to={neighborTo(neighbors.prev)}
          replace
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__nav kk-viewer__nav--prev"
          aria-label={t('photo.prev')}
        >
          <Icon name="chevron-left" />
        </Link>
      )}
      {neighbors.next !== null && (
        <Link
          to={neighborTo(neighbors.next)}
          replace
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__nav kk-viewer__nav--next"
          aria-label={t('photo.next')}
        >
          <Icon name="chevron-right" />
        </Link>
      )}

      {/* On a phone the drawer overlays the stage; a scrim dims the photo behind it
          and closes the drawer when tapped. Hidden at ≥ md, where the stage makes
          room beside the drawer instead. */}
      {panelOpen && (
        <button
          type="button"
          className="kk-viewer__panel-scrim"
          aria-label={t('photo.viewer.closeInfo')}
          onClick={() => {
            setPanelOpen(false)
          }}
        />
      )}

      {/* The metadata drawer: everything the photo carries, on demand. */}
      <aside
        className={`kk-viewer__panel${panelOpen ? ' is-open' : ''}`}
        aria-label={t('photo.viewer.info')}
        aria-hidden={!panelOpen}
      >
        <div className="kk-viewer__panel-head">
          <h2 className="kk-viewer__panel-title">{t('photo.viewer.info')}</h2>
          <button
            type="button"
            className="kk-viewer__btn kk-viewer__btn--icon"
            aria-label={t('photo.viewer.closeInfo')}
            onClick={() => {
              setPanelOpen(false)
            }}
          >
            <Icon name="x-lg" />
          </button>
        </div>
        <div className="kk-viewer__panel-body">
          {showEdit && (
            <section className="kk-viewer__section">
              <EditPanel
                uid={photo.uid}
                edit={previewEdit}
                onChange={applyEdit}
                onSaved={onEditSaved}
                onClose={toggleEdit}
              />
            </section>
          )}

          {showFaces && (
            <section className="kk-viewer__section">
              <FacesPanel
                faces={faces}
                canWrite={canWrite}
                hovered={hoveredFace}
                onHover={setHoveredFace}
                onClose={toggleFaces}
              />
            </section>
          )}

          <section className="kk-viewer__section">
            <p className="kk-text-eyebrow mb-2">{t('photo.sections.caption')}</p>
            <MetadataPanel photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
          </section>

          <section className="kk-viewer__section">
            <p className="kk-text-eyebrow mb-2">{t('photo.sections.organize')}</p>
            <OrganizePanel photo={photo} canWrite={canWrite} onChanged={setPhoto} />
            <hr />
            <PeoplePanel
              photoUid={photo.uid}
              faces={faces}
              canWrite={canWrite}
              loading={loadingNext}
              onEditFace={editFace}
            />
          </section>

          {photo.stack_members !== undefined && photo.stack_members.length > 1 && (
            <section className="kk-viewer__section">
              <StackStrip
                members={photo.stack_members}
                currentUid={photo.uid}
                canWrite={canWrite}
                onSetPrimary={handleSetStackPrimary}
                onUnstackMember={handleUnstackMember}
                onUnstackAll={handleUnstackAll}
                detailQuery={detailQuery}
              />
            </section>
          )}

          <section className="kk-viewer__section">
            <TechnicalDetails
              photo={photo}
              canWrite={canWrite}
              onThumbnailRegenerated={onThumbnailRegenerated}
            />
          </section>

          <section className="kk-viewer__section d-flex gap-2 flex-wrap">
            <Button as="a" href={photo.download_url} variant="outline-secondary" size="sm" download>
              {t('photo.download')}
            </Button>
            {!isIdentityEdit(edit) && (
              <Button
                as="a"
                href={downloadUrl(photo.uid, { token: downloadToken })}
                variant="outline-secondary"
                size="sm"
                download
              >
                {t('photo.downloadEdited')}
              </Button>
            )}
          </section>

          <section className="kk-viewer__section">
            <SimilarPhotos uid={photo.uid} />
          </section>
        </div>
      </aside>
    </div>
  )
}
