import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BlurPlaceholder } from '../components/BlurPlaceholder'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { KeyboardShortcutsHelp } from '../components/KeyboardShortcutsHelp'
import { FavoriteToggle } from '../components/library/FavoriteButton'
import { FlagControl } from '../components/library/FlagControl'
import { RatingStars } from '../components/library/RatingStars'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { CommentsPanel } from '../components/photo/CommentsPanel'
import { EditPanel } from '../components/photo/EditPanel'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PhotoFlagBadges } from '../components/photo/PhotoFlagBadges'
import { PeoplePanel } from '../components/photo/PeoplePanel'
import { ProcessingPanel } from '../components/photo/ProcessingPanel'
import { StackStrip } from '../components/photo/StackStrip'
import { TechnicalDetails } from '../components/photo/TechnicalDetails'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import './../components/photo/viewer.css'
import { SharePhotosButton } from '../components/organize/SharePhotosButton'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { FacesPanel } from '../components/people/FacesPanel'
import { useMorph, useMorphMark } from '../components/morph/MorphContext'
import { useToast } from '../components/toast/ToastContext'
import { useAutoHideChrome } from '../hooks/useAutoHideChrome'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useFaces } from '../hooks/useFaces'
import { useFavorite } from '../hooks/useFavorite'
import { useImageFrame } from '../hooks/useImageFrame'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { useKeyboardInset } from '../hooks/useKeyboardInset'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { type NeighborPhoto, usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { usePinchZoom } from '../hooks/usePinchZoom'
import { useRating } from '../hooks/useRating'
import { useSwipeNavigation } from '../hooks/useSwipeNavigation'
import { useViewportBox } from '../hooks/useViewportBox'
import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from '../lib/detailView'
import { readFaceOverlay, writeFaceOverlay } from '../lib/faceOverlayPref'
import { formatDateTimeMinutes } from '../lib/format'
import { gridScrollKey, rememberGridPhoto } from '../lib/gridScroll'
import {
  editPreviewStyle,
  editTransform,
  hasCrop,
  isIdentityEdit,
  NEUTRAL_EDIT,
} from '../lib/photoEdit'
import { handoffPreviewUrl } from '../lib/photoHandoff'
import { photoDisplayTitle, photoTitleText, titleSource } from '../lib/photoTitle'
import { isTypingElement, ratingHotkey } from '../lib/ratingHotkeys'
import { stageRenditionName } from '../lib/rendition'
import { toMode } from '../lib/searchView'
import { isFormModalOpen } from '../lib/shortcuts'
import { readUrlState } from '../lib/urlState'
import { preloadUids } from '../lib/viewerPreload'
import { isNotFound } from '../services/auth'
import {
  archivePhoto,
  downloadUrl,
  fetchEdit,
  fetchPhoto,
  GRID_PREVIEW_SIZE,
  hidePhoto,
  type PhotoDetail,
  type PhotoEdit,
  setStackPrimary,
  thumbUrl,
  unarchivePhoto,
  unhidePhoto,
  unstackAll,
  unstackMember,
} from '../services/photos'

/**
 * Fetch lifecycle of the photo detail (the photo and its stored edit). `missing`
 * is a 404 kept apart from `error`: a photo reached from an audit entry may well
 * have been purged since — the log records the purge — and "this no longer
 * exists" is a different message from "could not be loaded".
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; photo: PhotoDetail; edit: PhotoEdit }

/**
 * The special slot at the top of the info drawer, or null for none. Faces and
 * edits both want that slot (and both alter what may be drawn over the photo), so
 * this single choice — rather than a boolean each — makes them fighting over it
 * unrepresentable: at most one is active, never both.
 */
type SidePanel = 'faces' | 'edit' | null

/**
 * Which of the top bar's three view toggles owns what the drawer currently shows.
 * Shutting the drawer takes the keyboard out of it (it goes `inert`), so whatever
 * focus it held has to land somewhere — and the control that reopens the very view
 * just closed is the one place that is never a surprise.
 */
type PanelToggle = 'info' | 'faces' | 'edit'

/**
 * The classes of a *flag toggle* — a control over a flag that holds the photo
 * back from the library (hidden from it, or archived into the trash) — for the
 * flag's current value.
 *
 * **The glyph on these two buttons shows STATE, never the action.** That is the
 * decision; please do not flip it back. It used to show the action (a hidden
 * photo got a plain eye meaning "click to show"), which contradicted the
 * `aria-pressed` state right next to it and was unreadable anyway: an eye and a
 * struck-through eye differ by a hairline at 1rem, so the state could only be
 * guessed by someone who already knew the convention. Now everything visual says
 * state — the glyph (a struck eye = hidden; the archive box names the flag
 * itself, as no glyph exists for "not in the trash"), `aria-pressed`, the
 * `active` marking and its `danger` tone — and only the `aria-label`/`title` say
 * what a click will do. The colour is never alone: `active` carries the state
 * for a colour-blind reader and forced colours, and {@link PhotoFlagBadges}
 * repeats it in words beside the title.
 */
function flagBtnClass(on: boolean): string {
  return `kk-viewer__btn kk-viewer__btn--icon kk-viewer__btn--flag${on ? ' active' : ''}`
}

/**
 * The immersive full-bleed photo viewer, and the `/photos/:uid` route itself.
 * Opening a photo drops the whole viewport into a distraction-free viewer: the
 * image is centered and scaled to the largest fit without cropping over a warm
 * near-black backdrop, reflecting the saved non-destructive edit (or, while the
 * edit panel is open, the adjustments in progress). The chrome — a top action
 * bar, the prev/next arrows and (on a phone) the bottom curation dock — melts
 * away after a short idle and returns on any pointer move or tap, all but the way
 * back and the photo's name: a phone has no "move the mouse" gesture, so those two
 * stay (dimmed) rather than leaving a screen with no visible way off it. A
 * persistent back arrow (Esc / ←) always works and steps back to the originating
 * list at its exact prior scroll position.
 *
 * Where the everyday curation controls sit depends on the reach: with a mouse
 * the top edge costs nothing, so they ride the top action bar; on a phone that
 * corner is the hardest place to hit one-handed on a tall screen, so stars,
 * personal mark, favorite and archive move down into a thumb-reachable dock
 * along the bottom edge and the top bar keeps only the occasional view toggles.
 *
 * All the rich metadata and curation — caption & place (EXIF, date, location with
 * its map), people/faces, albums & labels, technical details, the variants stack,
 * similar photos and the non-destructive editor — live in a metadata drawer that
 * opens on demand rather than being always visible: from the side on a wide
 * screen, as a bottom sheet over a little under half the height on a phone, where
 * "beside the photo" does not exist. The default state is just the photo, and the
 * photo stays visible (and pannable) beside or above the drawer either way — the
 * faces panel's numbered rows only mean anything against the numbered boxes drawn
 * on the photograph. The open photo and the drawer's open state both live
 * in URL params, so Back and refresh behave. Every mutation is role-gated; viewers
 * get a read-only viewer.
 */
export function PhotoDetailPage() {
  const { t, i18n } = useTranslation()
  const toast = useToast()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken, user, isAdmin, isMaintainer } = useAuth()
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
  // The drawer's non-scrolling footer node. The metadata edit form portals its
  // Save/Cancel bar in here so it stays pinned to the drawer's bottom while editing
  // — a footer beside the scrolling body, not a sticky bar that only pins mid-scroll.
  const [panelFoot, setPanelFoot] = useState<HTMLDivElement | null>(null)
  // In flight while the archive/restore (trash) mutation runs, so its button is
  // disabled and cannot be double-fired.
  const [archivePending, setArchivePending] = useState(false)
  // In flight while the hide/show (library visibility) mutation runs, so its
  // button is disabled and cannot be double-fired.
  const [hidePending, setHidePending] = useState(false)
  const faces = useFaces(uid)
  // How far the on-screen keyboard reaches up the window, so the phone's bottom
  // sheet can lift clear of it — otherwise tapping into the comment composer puts
  // the composer behind the very keyboard that is meant to fill it.
  const keyboardInset = useKeyboardInset()
  // Phone width moves the curation loop from the top bar into the bottom dock.
  // The choice is made in JS rather than by a pair of CSS display rules, so the
  // controls exist exactly ONCE in the DOM: a second, hidden copy would give
  // every star and heart a twin for assistive tech (and for a query) to find.
  const narrow = useIsNarrowViewport()
  // The grid ⇄ viewer morph: this page is the half the clicked tile grows into,
  // and `morphMark` is what marks the photograph on stage as that half.
  const morph = useMorph()
  const morphMark = useMorphMark(uid)

  const view = useMemo(() => readUrlState(searchParams, DETAIL_DEFAULTS), [searchParams])
  const neighborParams = useMemo(() => detailToParams(view), [view])
  const detailQuery = detailQueryString(view)
  // A `mode` scope means the photo was opened from search, so prev/next must page
  // through `GET /search` in the same ranked order the results grid showed rather
  // than the plain library list.
  const searchMode = view.mode !== '' ? toMode(view.mode) : undefined
  const neighbors = usePhotoNeighbors(uid, neighborParams, true, searchMode)

  // The box the stage draws into, for picking the preview's rendition below.
  const viewport = useViewportBox()

  // The warm-image window around the photo on stage. Its readiness is read back
  // below to decide whether the stage still needs a smaller image painted under
  // the full-size one: a photo stepped to from its neighbour is already decoded,
  // and fetching a second, smaller rendition of it would be bytes spent on a
  // frame nobody ever sees.
  const { prime, statusOf } = useImagePreloader()

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

  // Tell the list this photo came from which photograph is on stage. Paging with
  // the arrows *replaces* the history entry rather than adding one, so without
  // this the way back would always land on the photograph first clicked, however
  // far the reader has paged since. The list is named the way its own grid
  // remembers itself — the very URL the Back link reconstructs — and the grid only
  // ever *reveals* what it is told, so naming a list nobody scrolled costs
  // nothing.
  const listScrollKey = useMemo(() => {
    const back = new URL(backHref(view), window.location.origin)
    return gridScrollKey(back.pathname, back.search)
  }, [view])
  useEffect(() => {
    rememberGridPhoto(listScrollKey, uid)
  }, [listScrollKey, uid])

  // The neighbour's detail URL, carrying the originating order/scope so prev/next
  // keeps paging the same list, plus the drawer's open state so it stays open (or
  // shut) as you step through photos.
  const neighborTo = useCallback(
    (neighbor: string): string => {
      const params = new URLSearchParams(detailQuery)
      if (panelOpen) {
        params.set('info', '1')
      }
      const query = params.toString()
      return query === '' ? `/photos/${neighbor}` : `/photos/${neighbor}?${query}`
    },
    [detailQuery, panelOpen],
  )

  // Page to a neighbour (prev/next) preserving the originating list order. Shared
  // by the on-image ‹/› arrows (which pass a known uid) and, through `step`, by
  // the ←/→ keys and the touch swipe/pinch, so all navigate identically (same
  // URL/state, same stop-at-ends semantics). Replace, so paging never grows the
  // history stack the close button pops.
  const goToNeighbor = useCallback(
    (neighbor: NeighborPhoto | null): void => {
      if (neighbor !== null) {
        void navigate(neighborTo(neighbor.uid), { replace: true })
      }
    },
    [navigate, neighborTo],
  )

  // A direction pressed before the list order was known, kept until it lands. A
  // photo opened directly — a shared link, a refresh, Back or Forward — takes a
  // moment to find its place in the list, and an arrow pressed in that window used
  // to be dropped on the floor. The keys then looked dead until the answer
  // happened to arrive between two presses, which read as "you have to click into
  // the page first"; remembering the direction makes them work from the first
  // frame instead.
  const [pendingStep, setPendingStep] = useState<'prev' | 'next' | null>(null)

  // Page one photo in `direction`, or remember the press while the neighbours are
  // still being resolved. At a genuine list end there is nowhere to go and the key
  // stays a no-op, exactly as before.
  const step = (direction: 'prev' | 'next'): void => {
    const target = direction === 'prev' ? neighbors.prev : neighbors.next
    if (target === null && neighbors.pending) {
      setPendingStep(direction)
      return
    }
    goToNeighbor(target)
  }

  // A remembered press is honoured the instant the neighbours arrive, and is
  // re-checked against the same guard the shortcut hook applies before it fires:
  // the wait is long enough for the user to have moved on, and a photo swapping
  // out from under a half-typed comment is what that guard exists to prevent.
  useEffect(() => {
    if (pendingStep === null || neighbors.pending) {
      return
    }
    setPendingStep(null)
    if (isTypingElement(document.activeElement) || isFormModalOpen()) {
      return
    }
    goToNeighbor(pendingStep === 'prev' ? neighbors.prev : neighbors.next)
  }, [pendingStep, neighbors.pending, neighbors.prev, neighbors.next, goToNeighbor])

  // Forgetting a remembered press when the photo changes keeps a step reached
  // another way (the on-image arrow, a swipe) from being paged twice.
  useEffect(() => {
    setPendingStep(null)
  }, [uid])

  // Close the viewer, returning to the originating list. Prefer stepping back
  // through history (`navigate(-1)`) so the browser restores the grid's exact
  // scroll position; only for a directly-opened photo — where there is no grid
  // entry behind us — do we push the reconstructed list URL instead.
  const close = (): void => {
    // Both ways out go through the one morph entry point, which leaves the
    // navigation itself exactly as it was and only wraps it. It pairs back into
    // the tile when the grid is already on screen by the time the browser
    // captures the new state, and otherwise degrades to the photograph fading
    // out over the page change — see `components/morph`.
    morph.morph(uid, () => {
      void (openedDirectlyRef.current ? navigate(backHref(view)) : navigate(-1))
    })
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
  // The stage's frame: the shape of the ONE preview on screen, measured from the
  // image itself once it has loaded (the catalogue row is only the estimate that
  // holds the layout still until then). It sizes the figure the face boxes are
  // positioned against, so it has to be the rendered image and nothing else.
  const stage = useImageFrame({
    source: ready?.uid ?? '',
    width: ready?.file_width ?? 0,
    height: ready?.file_height ?? 0,
    orientation: ready?.file_orientation ?? 0,
  })
  // What the one photo previews: the adjustments in progress while the edit panel
  // is open, otherwise the stored edit.
  const previewEdit = editDraft ?? (state.status === 'ready' ? state.edit : NEUTRAL_EDIT)
  // The overlay is only ever drawn over a still image: a video player's chrome is
  // not a photo, and faces are never detected on clips anyway.
  const isStill = ready !== null && ready.media_type !== 'video' && ready.media_type !== 'live'
  // While a neighbour loads the faces are keyed on the target photo, so they must
  // not be drawn over the still-displayed previous one.
  const loadingNext = ready !== null && ready.uid !== uid
  // A CROP rules the whole face UI out: it leaves a frame the boxes were never
  // measured against, so every rectangle would be off its face — rather than draw
  // them wrong, the faces stand down. A rotation does not: FaceOverlay maps its
  // boxes through it and follows the turned photo. Brightness and contrast move no
  // pixels at all.
  const facesAvailable = isStill && !loadingNext && faces.faces.length > 0 && !hasCrop(previewEdit)
  const showFaces = facesAvailable && sidePanel === 'faces'
  // Edits are for stills only — the backend never re-renders a video edit, and the
  // player carries no preview surface to apply them to.
  const showEdit = canWrite && isStill && sidePanel === 'edit'
  // The drawer is ONE panel showing ONE of three mutually-exclusive views: faces,
  // edits, or the metadata ("Informace"). Faces and edits are their own focused
  // views — the metadata belongs to the info view alone, so activating faces or
  // edits must NOT drag the whole info panel in with them. Info is simply "neither
  // lead is active"; a `sidePanel` stuck on faces with no faces available (e.g.
  // after paging to a photo with none) falls through to info rather than an empty
  // drawer.
  const showInfo = !showFaces && !showEdit
  // The info view is actually on screen only when the drawer is open on it — used
  // for the info toggle's pressed state, so exactly one of the three view toggles
  // (faces / edits / info) reads as active at a time, never the info button lit
  // alongside faces or edits just because the drawer happens to be open.
  const infoActive = panelOpen && showInfo

  // The drawer itself and the three toggles that open its views. A shut drawer is
  // `inert` (see the `<aside>` below), and a browser blurs whatever is focused
  // inside a subtree that goes inert — leaving focus on `<body>`, where the next
  // Tab restarts at the top of the page. So a close that came from INSIDE the
  // drawer hands the keyboard back to the toggle that reopens it.
  const panelRef = useRef<HTMLElement | null>(null)
  const infoToggleRef = useRef<HTMLButtonElement | null>(null)
  const facesToggleRef = useRef<HTMLButtonElement | null>(null)
  const editToggleRef = useRef<HTMLButtonElement | null>(null)
  // Set by `closePanel` when the close came from inside the drawer; read and
  // cleared by the effect below, once the drawer has actually shut.
  const returnFocusRef = useRef<PanelToggle | null>(null)
  const shownToggle: PanelToggle = showFaces ? 'faces' : showEdit ? 'edit' : 'info'

  // The ONE way to shut the drawer — its own ✕, the view toggles, Escape all go
  // through here — so the focus hand-off can never be forgotten by one of them.
  const closePanel = (): void => {
    const inside = panelRef.current?.contains(document.activeElement) ?? false
    returnFocusRef.current = inside ? shownToggle : null
    setPanelOpen(false)
  }

  useEffect(() => {
    const toggle = returnFocusRef.current
    if (panelOpen || toggle === null) {
      return
    }
    returnFocusRef.current = null
    const toggles: Record<PanelToggle, HTMLButtonElement | null> = {
      info: infoToggleRef.current,
      faces: facesToggleRef.current,
      edit: editToggleRef.current,
    }
    // The faces/edit toggle can be gone by the time we get here (a photo with no
    // faces, a viewer's read-only bar); the info toggle is always in the bar, so it
    // is the fallback. `preventScroll`, because scrolling to reveal a control is the
    // very jolt this panel's focus handling exists to prevent.
    const target = toggles[toggle] ?? infoToggleRef.current
    target?.focus({ preventScroll: true })
  }, [panelOpen])

  // Faces and edits share the drawer's lead slot, so showing either one closes the
  // other; opening either opens the drawer (their panels live inside it).
  const openFaces = (): void => {
    setSidePanel('faces')
    writeFaceOverlay(true)
    setEditDraft(null)
    setPanelOpen(true)
  }

  // Hiding the faces closes their view: it drops the selection, the overlay and the
  // remembered preference, and shuts the drawer. Faces are their own view, not a
  // section of the info panel, so turning them off reveals the photo — not the
  // metadata (the info button is how you reach that).
  const toggleFaces = (): void => {
    if (sidePanel === 'faces') {
      setSidePanel(null)
      writeFaceOverlay(false)
      faces.select(null)
      closePanel()
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
  // drawn over an edited preview), so the selection is dropped too. Closing the
  // edits shuts the drawer — like faces, edits are their own view, not part of the
  // info panel — discarding whatever is unsaved so the photo shows exactly what is
  // stored.
  const toggleEdit = (): void => {
    if (sidePanel === 'edit') {
      setSidePanel(null)
      setEditDraft(null)
      closePanel()
      return
    }
    setSidePanel('edit')
    setEditDraft(null)
    faces.select(null)
    setPanelOpen(true)
  }

  // The info button opens the metadata view. From faces or edits it SWITCHES to the
  // info view (drops the lead, and with it the overlay/selection that only made
  // sense there); from the info view already showing, it toggles the drawer shut.
  const togglePanel = (): void => {
    if (panelOpen && sidePanel === null) {
      closePanel()
      return
    }
    setSidePanel(null)
    setEditDraft(null)
    faces.select(null)
    setPanelOpen(true)
  }

  // Honour the remembered "show faces" preference on load: once this photo's faces
  // are known to be available, bring their panel up (the overlay and its naming
  // panel are a pair), so the stored choice opens the panel too — not just the
  // boxes over a shut drawer. It fires once, on the availability edge; the user
  // closing the drawer afterwards is respected (`sidePanel` clears), and paging
  // carries the drawer's open state onward in the URL.
  const facesAutoOpenedRef = useRef(false)
  useEffect(() => {
    if (facesAutoOpenedRef.current || !facesAvailable || sidePanel !== 'faces' || panelOpen) {
      return
    }
    facesAutoOpenedRef.current = true
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('info', '1')
        return next
      },
      { replace: true },
    )
  }, [facesAvailable, sidePanel, panelOpen, setSearchParams])

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
      step(direction === 'next' ? 'next' : 'prev')
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
        step('next')
      } else {
        step('prev')
      }
    },
  })

  // Whether the open photo is currently hidden from the library. Unlike
  // `archived` this is not a step towards deletion: the photo stays in the
  // catalogue and in everything it was filed in, it just leaves the firehose.
  const hidden = state.status === 'ready' && state.photo.hidden_from_library === true

  // Hide the open photo from the library, or bring it back. Like the archive
  // toggle the page stays put and flips the flag locally (both endpoints answer
  // with the refreshed photo, but nothing else on the page depends on it). The
  // success toast names where the photo went and how to find it again — a flag
  // you cannot list is a flag you cannot undo.
  //
  // Unlike its archive twin this one is declared ABOVE the loading/error returns,
  // because the `s` shortcut just below closes over it: a `const` declared after
  // an early return stays in its temporal dead zone on every render that returns
  // early, so the key would throw while the photo was still loading. The eye
  // button further down calls this very function — one implementation, one
  // optimistic flip, one toast, whichever way it is reached.
  const toggleHidden = async (): Promise<void> => {
    if (state.status !== 'ready' || hidePending) {
      return
    }
    const target = state.photo.uid
    const wasHidden = hidden
    setHidePending(true)
    try {
      if (wasHidden) {
        await unhidePhoto(target)
      } else {
        await hidePhoto(target)
      }
      // Only the photo the mutation was aimed at: paging on while it was in
      // flight must not stamp the answer onto whatever is on stage now.
      setState((prev) =>
        prev.status === 'ready' && prev.photo.uid === target
          ? { ...prev, photo: { ...prev.photo, hidden_from_library: !wasHidden } }
          : prev,
      )
      toast.show({
        message: wasHidden ? t('photo.hidden.shown') : t('photo.hidden.hidden'),
        variant: 'success',
      })
    } catch {
      toast.show({ message: t('photo.hidden.error'), variant: 'danger' })
    } finally {
      setHidePending(false)
    }
  }

  // Viewer shortcuts: ←/→ page, `f` favorite, `m` faces, `i` info drawer, `s`
  // hide/unhide, Escape steps back out (a selected face, then the drawer, then
  // the viewer itself).
  // Rating keys (0–5, p/r) are handled by the separate effect below. The hook
  // suppresses these while typing, which keeps `m`/`i` out of the name field.
  useKeyboardShortcuts({
    ArrowLeft: () => {
      step('prev')
    },
    ArrowRight: () => {
      step('next')
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
    // `s` = skrýt: the eye button's own act, on a key — and its own undo, since
    // the toast carries no action button and the view stays on the photo. Bound
    // only for an editor, exactly like the button: for a viewer the key is not
    // in the map at all, so nothing happens and nothing is swallowed.
    ...(canWrite
      ? {
          s: () => {
            void toggleHidden()
          },
        }
      : {}),
    Escape: () => {
      if (faces.selected !== null) {
        faces.select(null)
        return
      }
      if (panelOpen || sidePanel !== null) {
        closePanel()
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
      // The same "a dialog is up" rule the shared hook applies: a rating must not
      // be stamped onto the photo behind an open edit/album modal.
      if (isFormModalOpen()) {
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
        setState({ status: isNotFound(err) ? 'missing' : 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  // The rendition the stage draws: the smallest `fit_*` rung that still covers
  // the photograph as this viewport actually paints it. A 4:3 photograph on a
  // phone is painted the phone's width across, not its height, so sizing for the
  // stage's longest side would fetch nearly twice the pixels the screen can show.
  // The rung set is capped at what the stage used to fetch unconditionally, so
  // this only ever asks for fewer bytes — never more (see `lib/rendition`).
  const previewSize = stageRenditionName(
    viewport,
    state.status === 'ready'
      ? { width: state.photo.file_width, height: state.photo.file_height }
      : null,
    viewport.dpr,
  )

  // The images kept warm around the one on stage: this photo and its immediate
  // neighbours, at preview size, so stepping shows a picture that is already
  // downloaded AND decoded rather than one that starts loading on the keypress.
  // The neighbours' own proportions are not known here — only their UIDs are —
  // so they are warmed at the rung the photo on screen resolved to, which is the
  // one they will almost always resolve to themselves.
  const preloadWindow = useMemo(
    () =>
      preloadUids(uid, neighbors.prev, neighbors.next).map((target) =>
        thumbUrl(target, previewSize, downloadToken ?? undefined),
      ),
    [uid, neighbors.prev, neighbors.next, downloadToken, previewSize],
  )
  useEffect(() => {
    prime(preloadWindow)
  }, [prime, preloadWindow])

  /**
   * What the photo is called, in one string — the same name the chrome's `<h1>`
   * shows below. Declared as a function because the document title has to be set
   * from a hook, and a hook cannot sit after the early returns that the narrowed
   * `photo` lives behind.
   */
  function photoName(shown: PhotoDetail): string {
    const captured =
      shown.taken_at !== undefined ? formatDateTimeMinutes(shown.taken_at, i18n.language) : ''
    return photoTitleText(
      photoDisplayTitle(titleSource(shown, i18n.language), captured),
      t('photo.untitled'),
    )
  }

  // Opening a photo in a second tab is ordinary in a gallery, and browser history
  // is how "the photo I saw last week" is found again — both need the tab to
  // carry the photo's name rather than a fiftieth „Kukátko".
  useDocumentTitle(state.status === 'ready' ? photoName(state.photo) : null)

  if (state.status === 'loading') {
    // The address the grid tile just painted, when it handed one over: those
    // bytes are already decoded, so the viewer can open ON the photograph rather
    // than on a spinner — and it gives the grid → viewer morph a photograph to
    // land on, which an empty stage would not. A photo opened from a bare link
    // hands over nothing and still gets the spinner.
    const handed = handoffPreviewUrl(location.state, uid)
    return (
      <div className="kk-viewer" data-chrome="visible">
        <button
          type="button"
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__back"
          aria-label={t('photo.back')}
          title={t('photo.back')}
          onClick={close}
        >
          <Icon name="arrow-left" />
        </button>
        <div className="kk-viewer__stage d-flex justify-content-center align-items-center">
          {handed === undefined ? (
            <Spinner animation="border" role="status" variant="light">
              <span className="visually-hidden">{t('photo.loading')}</span>
            </Spinner>
          ) : (
            <div className="kk-viewer__figure" {...morphMark}>
              {/* Decorative: it is the tile's own image, and the photo's name is
                  not known yet — the row it would come from is still on the wire.
                  The loading state is announced beside it instead. */}
              <img className="kk-viewer__image" src={handed} alt="" draggable={false} />
              <span className="visually-hidden" role="status">
                {t('photo.loading')}
              </span>
            </div>
          )}
        </div>
      </div>
    )
  }

  if (state.status === 'error' || state.status === 'missing') {
    const gone = state.status === 'missing'
    return (
      <div className="kk-viewer" data-chrome="visible">
        <div className="kk-viewer__stage">
          <ErrorState
            title={gone ? t('photo.missing') : t('photo.error')}
            hint={gone ? t('photo.missingHint') : undefined}
            action={
              <Button variant="outline-light" size="sm" onClick={close}>
                {t('photo.back')}
              </Button>
            }
          />
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
  const displayTitle = photoDisplayTitle(titleSource(photo, i18n.language), captureDate)
  // The one-string form, for alt text, the players' titles and the browser tab.
  const title = photoName(photo)

  const setPhoto = (updated: PhotoDetail): void => {
    setState({ status: 'ready', photo: updated, edit })
  }

  // The conversation's size, as the detail response reported it — and thereafter as
  // the open thread reports it, so posting or deleting a comment re-badges the
  // toggle immediately instead of waiting for the next load of the photo.
  const commentCount = photo.comment_count ?? 0
  const setCommentCount = (count: number): void => {
    if (count !== commentCount) {
      setPhoto({ ...photo, comment_count: count })
    }
  }
  // Stack mutations always refresh the photo being viewed (not the member that was
  // mutated), so the variants strip and the member-count reflect the change.
  const reloadPhoto = async (): Promise<void> => {
    setPhoto(await fetchPhoto(uid))
  }

  // Whether the open photo is currently in the trash (soft-deleted). Drives the
  // header control between Archive (send to trash) and Restore (bring back) — a
  // photo opened from the Trash page arrives already archived.
  const archived = photo.archived_at !== undefined && photo.archived_at !== ''

  // Archive the open photo, or restore it when it is already in the trash. Like
  // every other detail mutation the page stays put and reflects the new state in
  // place: the archived flag flips (so the button swaps Archive ⇄ Restore) rather
  // than navigating away. Both endpoints answer 204, so the flag is toggled
  // locally instead of refetched, and the result is reported with a toast.
  const toggleArchive = async (): Promise<void> => {
    setArchivePending(true)
    try {
      if (archived) {
        await unarchivePhoto(photo.uid)
        setPhoto({ ...photo, archived_at: undefined })
        toast.show({ message: t('photo.archive.restored'), variant: 'success' })
      } else {
        await archivePhoto(photo.uid)
        setPhoto({ ...photo, archived_at: new Date().toISOString() })
        toast.show({ message: t('photo.archive.archived'), variant: 'success' })
      }
    } catch {
      toast.show({ message: t('photo.archive.error'), variant: 'danger' })
    } finally {
      setArchivePending(false)
    }
  }
  // `hidden` and `toggleHidden` live above the early returns (see them there):
  // the `s` shortcut has to close over the toggle, which a declaration down here
  // could not survive on a render that returns early.
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

  // The everyday curation loop: the stars, the personal mark, the favorite heart
  // and — for an editor — archive/restore. Built ONCE here and mounted in exactly
  // one of two places, the top action bar or the phone's bottom dock, so the two
  // layouts cannot drift apart: they are the same element tree driving the same
  // handlers, not two copies kept in sync by hand. Touch gets the larger glyphs
  // (the dock's own CSS lifts each button to the 44px finger floor).
  //
  // The archive glyph does not move with its state (see the note at the button),
  // so the sentence that does is composed once and worn twice: as the accessible
  // name and as the tooltip the mouse gets.
  const archiveLabel = archived ? t('photo.archive.restore') : t('batch.archive')
  // The same once-and-twice for the two chrome toggles whose sentence is not a
  // constant: the faces toggle switches on its own state, the info toggle counts
  // the conversation behind it.
  const facesToggleLabel = showFaces ? t('faces.hide') : t('faces.toggle')
  const infoToggleLabel =
    commentCount > 0
      ? t('photo.viewer.infoWithComments', { count: commentCount })
      : t('photo.viewer.info')
  const curation = (
    <>
      <RatingStars
        rating={rating.rating}
        onRate={rating.setRating}
        disabled={rating.pending}
        size={narrow ? 26 : 22}
      />
      {/* The marks travel as one cluster, so where the dock has to wrap it breaks
          between the stars and them — never leaving the heart stranded alone on a
          line of its own. */}
      <span className="kk-viewer__marks">
        <FlagControl
          flag={rating.flag}
          onFlag={rating.setFlag}
          disabled={rating.pending}
          size={narrow ? 22 : 18}
        />
        <FavoriteToggle
          favorite={favorite.favorite}
          pending={favorite.pending}
          onToggle={() => {
            favorite.toggle()
          }}
        />
        {canWrite && (
          <button
            type="button"
            className={flagBtnClass(archived)}
            aria-label={archiveLabel}
            title={archiveLabel}
            aria-pressed={archived}
            disabled={archivePending}
            onClick={() => {
              void toggleArchive()
            }}
          >
            {/* No glyph exists for "not in the trash", so this one names the flag
                itself and never moves; the on-state marking says whether it is set. */}
            <Icon name="archive" />
          </button>
        )}
        {canWrite && (
          <button
            type="button"
            className={flagBtnClass(hidden)}
            aria-label={hidden ? t('photo.hidden.show') : t('photo.hidden.hide')}
            // The title spells out what the toggle does and how to get back, so
            // the one-glyph control is not the only place the rule is written.
            title={hidden ? t('photo.hidden.showHint') : t('photo.hidden.hideHint')}
            aria-pressed={hidden}
            disabled={hidePending}
            onClick={() => {
              void toggleHidden()
            }}
          >
            {/* State, not action: a struck-through eye means the photo IS hidden. */}
            <Icon name={hidden ? 'eye-slash' : 'eye'} />
          </button>
        )}
      </span>
    </>
  )

  // The share control speaks in selections; on a detail page the selection is the
  // one photo on screen. A fresh array per render costs nothing — the hook reads it
  // by value, not by identity.
  const sharePhotoUids = [photo.uid]

  const basePoster = thumbUrl(photo.uid, previewSize, downloadToken)
  // The thumb URL is built from the UID (stable), so a regenerated thumbnail would
  // otherwise be masked by the browser cache. Append a version once the user
  // regenerates it, so the new image actually shows without a hard reload.
  const poster =
    thumbVersion > 0
      ? `${basePoster}${basePoster.includes('?') ? '&' : '?'}v=${String(thumbVersion)}`
      : basePoster

  // The smaller image the stage paints UNDER the full-size one while that one is
  // still on the wire, so a photograph arrives as itself — softly at first, then
  // sharp — instead of as a grey well or a blur that snaps. In order of
  // preference: the very address the grid tile just painted (handed over in the
  // navigation state, so it is already in the browser's cache and costs nothing
  // at all), then the aspect-preserving rendition this photo's own payload names.
  // Never the square crop: that is a centre cut of the photograph, and painting
  // it under the whole one would show the wrong part of it and then jump.
  const underSrc =
    handoffPreviewUrl(location.state, photo.uid) ??
    photo.preview_url ??
    thumbUrl(photo.uid, GRID_PREVIEW_SIZE, downloadToken)
  // It is worth painting only while the real image is genuinely still coming:
  // never once that image has loaded (`measured`), never when the preloader has
  // already decoded it (stepping to a warmed neighbour — the swap is instant and
  // a second request would be pure waste), never when it IS the image on stage,
  // and never on an unframed figure, which shrink-wraps its image and so has no
  // box to fill. `thumbVersion` deliberately does not defeat this: a regenerated
  // thumbnail changes the full-size address, and the stale smaller one under it
  // is a better first frame than nothing.
  const showUnder =
    !stage.measured && statusOf(poster) !== 'ready' && underSrc !== poster && underSrc !== ''

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
    // Give the figure the photo's display aspect ratio so it fits the stage by
    // "contain" while its box stays exactly the rendered image — the only thing
    // that keeps the percentage face overlay on the faces instead of drifting into
    // a letterbox gap. It comes from the loaded image (`stage`), not from the row:
    // a row with a transposed dimension pair letterboxes the photo inside its own
    // figure and throws every box off its face. Absent dimensions fall back to the
    // bare shrink-wrap (no `data-framed`), which a frameless photo never needs.
    const framed = stage.aspectRatio !== undefined
    return (
      <div
        // Keyed on the DISPLAYED photo (not the route uid): while a neighbour
        // loads, the previous photo stays mounted (no flicker), and the
        // fade/scale replays only once the new photo actually swaps in.
        key={photo.uid}
        className="kk-viewer__figure"
        // The viewer's half of the grid ⇄ viewer morph: the figure is exactly the
        // rendered photograph, so the tile grows into the photograph rather than
        // into the stage's letterbox around it.
        {...morphMark}
        data-framed={framed ? 'true' : undefined}
        style={framed ? { aspectRatio: stage.aspectRatio } : undefined}
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
        {/* The photograph's blurred stand-in, filling exactly the framed figure
            — the same box the image will occupy — so the viewer opens in the
            photo's own colours instead of empty stage. It is dropped the moment
            the image has loaded (`measured`), before any zoom or rotation can
            move the image off it, and never rendered for an unframed figure,
            which shrink-wraps its image and so has no box to fill yet. */}
        {framed && !stage.measured && <BlurPlaceholder hash={photo.blurhash} />}
        {/* The progressive middle step: the grid's own smaller rendition, laid
            over the blur and under the full-size image, filling exactly the same
            framed box so the sharp one lands on it without moving a pixel. It
            carries the same edit/zoom transform as the image above it, so a
            rotated or cropped photograph does not un-rotate for a moment as the
            two swap. Decorative — the image above it owns the alt text — and a
            failure is simply nothing (the blur is still underneath). */}
        {framed && showUnder && (
          <img
            aria-hidden="true"
            className="kk-viewer__image kk-viewer__image--under"
            src={underSrc}
            alt=""
            style={stillStyle}
            draggable={false}
          />
        )}
        <img
          {...stage.imgProps}
          className="kk-viewer__image"
          // The frame stated as the image's INTRINSIC size, before the image is
          // there to state it itself. Without it the figure has nothing in flow
          // to size it while the photograph is on the wire — an `<img>` with a
          // loading `src` and no dimensions is a box the width of its alt text —
          // so it collapsed to a thumbnail-sized sliver and the stand-ins that
          // fill it (the blur, the smaller rendition) collapsed with it, which is
          // exactly the gap they exist to cover. With it, the loading box and the
          // loaded box are measurably identical and nothing moves on arrival.
          width={framed ? stage.frame.width : undefined}
          height={framed ? stage.frame.height : undefined}
          src={poster}
          alt={title}
          style={stillStyle}
          draggable={false}
        />
        {showFaces && (
          <FaceOverlay
            faces={faces.faces}
            // No box until the figure is the measured image: against the row's
            // estimate a box can sit off its face and then jump when the real
            // frame lands. The layer itself stays, so the faces view is up.
            measured={stage.measured}
            // The boxes follow the preview: the image carries the same rotation as
            // a CSS transform, and the ratio is what lets a quarter-turned layer
            // find the box the turned photo actually occupies.
            rotation={previewEdit.rotation}
            frameRatio={stage.ratio}
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
      // The phone's bottom sheet reads this to lift clear of the on-screen
      // keyboard; it is 0px on every desktop browser, so nothing moves there.
      style={{ '--kk-keyboard-inset': `${keyboardInset}px` } as CSSProperties}
    >
      {/* The persistent way out: top-left, never fades with the chrome, so Esc
          always has a visible twin the pointer can find. Returns to the
          originating list.

          A BACK ARROW, never a cross. The drawer carries its own ✕, and on a phone
          the two sit on one screen — as two identical round crosses they read as
          the same control, so a tap meant to shut the panel dropped the whole
          photograph instead. The arrow leaves the photo; the cross closes what is
          over it. */}
      <button
        type="button"
        className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__back"
        aria-label={t('photo.back')}
        title={t('photo.back')}
        onClick={close}
      >
        <Icon name="arrow-left" />
      </button>

      {/* Auto-hiding top action bar: the photo's name, then the view toggles —
          plus, on a pointer-sized screen, the curation loop (on a phone that one
          lives in the bottom dock instead). */}
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
          {/* The flags that hold this photo back from the library, in words —
              the one place the state shows outside the toggle that sets it, and
              the only one a viewer (who gets no toggles) ever sees. */}
          <PhotoFlagBadges hidden={hidden} archived={archived} />
        </div>
        <div className="kk-viewer__actions">
          {!narrow && curation}
          {facesAvailable && (
            <button
              type="button"
              ref={facesToggleRef}
              className="kk-viewer__btn kk-viewer__btn--icon"
              aria-pressed={showFaces}
              aria-label={facesToggleLabel}
              title={facesToggleLabel}
              onClick={toggleFaces}
            >
              <Icon name="person-bounding-box" />
            </button>
          )}
          {canWrite && isStill && (
            <button
              type="button"
              ref={editToggleRef}
              className="kk-viewer__btn kk-viewer__btn--icon"
              aria-pressed={showEdit}
              aria-label={t('photo.edit.title')}
              title={t('photo.edit.title')}
              onClick={toggleEdit}
            >
              <Icon name="sliders" />
            </button>
          )}
          {/* The info toggle carries the conversation's count. A thread nobody can
              see from the outside is a thread nobody joins, so the number rides the
              one control that opens it — and it is in the accessible name too, not
              only in the little disc, which a screen reader would never read out. */}
          <button
            type="button"
            ref={infoToggleRef}
            className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__btn--badged"
            aria-pressed={infoActive}
            aria-label={infoToggleLabel}
            title={infoToggleLabel}
            onClick={togglePanel}
          >
            <Icon name="info-circle" />
            {commentCount > 0 && (
              <span className="kk-viewer__btn-badge" aria-hidden="true">
                {commentCount > 99 ? '99+' : commentCount}
              </span>
            )}
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
          to={neighborTo(neighbors.prev.uid)}
          replace
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__nav kk-viewer__nav--prev"
          aria-label={t('photo.prev')}
          title={t('photo.prev')}
        >
          <Icon name="chevron-left" />
        </Link>
      )}
      {neighbors.next !== null && (
        <Link
          to={neighborTo(neighbors.next.uid)}
          replace
          className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__nav kk-viewer__nav--next"
          aria-label={t('photo.next')}
          title={t('photo.next')}
        >
          <Icon name="chevron-right" />
        </Link>
      )}

      {/* The phone's curation dock: the everyday actions along the bottom edge,
          where the thumb already rests, instead of the top corner it cannot
          reach. It fades with the rest of the chrome, so the photo is never
          permanently boxed in, and stands down while the drawer (which owns the
          whole screen at this width) is open. */}
      {narrow && (
        <div className="kk-viewer__dock" role="group" aria-label={t('photo.viewer.actions')}>
          {curation}
        </div>
      )}

      {/* The metadata drawer: everything the photo carries, on demand. At ≥ md it
          slides in beside the photo; on a phone it rises from the bottom edge as a
          sheet over a little under half the screen. Either way the stage gives up
          exactly that space, so the photo stays visible — and there is no scrim
          over it, so it stays pannable too. */}
      <aside
        ref={panelRef}
        className={`kk-viewer__panel${panelOpen ? ' is-open' : ''}`}
        aria-label={t('photo.viewer.info')}
        aria-hidden={!panelOpen}
        // Shut, the drawer slides off screen — it does not unmount — so every one
        // of its controls stays laid out, and a laid-out control is a tabbable one.
        // Tab walked straight into a panel nobody can see, and the browser scrolled
        // the photograph clean off the screen to reveal it, for 17 stops. `inert`
        // takes the whole subtree out of the tab order AND out of the accessibility
        // tree, which is also what makes the `aria-hidden` above honest: focusable
        // content inside `aria-hidden` is a WCAG 4.1.2 violation, because a screen
        // reader announces nothing while focus sits there. `visibility: hidden` in
        // the stylesheet says the same thing to a browser too old for `inert`.
        // This covers the faces and edit views too — they are views OF this one
        // drawer, not panels of their own.
        inert={!panelOpen}
      >
        {/* The generic drawer header belongs to the info view; the faces and edit
            panels carry their own header + close, so it would only duplicate them. */}
        {showInfo && (
          <div className="kk-viewer__panel-head">
            <h2 className="kk-viewer__panel-title">{t('photo.viewer.info')}</h2>
            <button
              type="button"
              className="kk-viewer__btn kk-viewer__btn--icon"
              aria-label={t('photo.viewer.closeInfo')}
              title={t('photo.viewer.closeInfo')}
              onClick={closePanel}
            >
              <Icon name="x-lg" />
            </button>
          </div>
        )}
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
                photoUid={photo.uid}
                faces={faces}
                canWrite={canWrite}
                takenAt={photo.taken_at}
                hovered={hoveredFace}
                onHover={setHoveredFace}
                onClose={toggleFaces}
              />
            </section>
          )}

          {/* The metadata IS the info view — kept out of the faces/edit views so
              activating those never drags the whole "Informace" panel in with them. */}
          {showInfo && (
            <>
              <section className="kk-viewer__section">
                <p className="kk-text-eyebrow mb-2">{t('photo.sections.caption')}</p>
                <MetadataPanel
                  photo={photo}
                  canWrite={canWrite}
                  onUpdated={setPhoto}
                  footer={panelFoot}
                />
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

              {/* The conversation sits right under the people, because that is what
                  it is usually about ("who is the boy on the left?"). Everybody who
                  is signed in may write here — viewers included — which is the one
                  deliberate exception to the read-only rule.

                  It mounts only while the drawer is actually open, unlike the rest
                  of this view. Paging through a hundred photos with the drawer shut
                  would otherwise fetch a hundred threads nobody asked to read — and
                  the empty result would overwrite the count the detail payload
                  already gave the badge. Shut, the badge speaks for the thread;
                  open, the thread speaks for itself. */}
              {panelOpen && (
                <section className="kk-viewer__section">
                  <CommentsPanel
                    photoUid={photo.uid}
                    currentUserUid={user?.uid ?? null}
                    canModerate={isAdmin}
                    onCountChange={setCommentCount}
                  />
                </section>
              )}

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

              {/* What the library has already computed about this photo. It is a
                  visible block of its own, not a row inside the collapsed
                  technical card: "why does this photo not come up in search?" is
                  a question worth answering without an extra click. */}
              {photo.processing !== undefined && photo.processing.length > 0 && (
                <section className="kk-viewer__section">
                  <p className="kk-text-eyebrow mb-2">{t('photo.sections.processing')}</p>
                  <ProcessingPanel uid={photo.uid} steps={photo.processing} canRun={isMaintainer} />
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
                <Button
                  as="a"
                  href={photo.download_url}
                  variant="outline-secondary"
                  size="sm"
                  download
                >
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
                {/* Straight into Fotky/Photos on a phone, next to the download that
                    is the only answer on a desktop (where it renders nothing). */}
                <SharePhotosButton photoUids={sharePhotoUids} variant="outline-secondary" />
              </section>

              <section className="kk-viewer__section">
                <SimilarPhotos uid={photo.uid} />
              </section>
            </>
          )}
        </div>
        {/* Non-scrolling footer beside the scrolling body: the metadata edit form
            portals its Save/Cancel here so they stay pinned to the drawer's bottom
            at any height. Empty (and zero-height) whenever nothing is editing. */}
        <div className="kk-viewer__panel-foot" ref={setPanelFoot} />
      </aside>
      {/* The viewer lives outside the Layout, so the navbar's shortcuts help is
          not on screen here: mount the triggerless variant so `?` still answers
          on the one screen with the most shortcuts of all. */}
      <KeyboardShortcutsHelp variant="bare" />
    </div>
  )
}
