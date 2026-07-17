import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { FavoriteToggle } from '../components/library/FavoriteButton'
import { FlagControl } from '../components/library/FlagControl'
import { RatingStars } from '../components/library/RatingStars'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { EditPanel } from '../components/photo/EditPanel'
import { Lightbox } from '../components/photo/Lightbox'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizeBadges } from '../components/photo/OrganizeBadges'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PeoplePanel } from '../components/photo/PeoplePanel'
import { StackStrip } from '../components/photo/StackStrip'
import { TechnicalDetails } from '../components/photo/TechnicalDetails'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { FacesPanel } from '../components/people/FacesPanel'
import { useFaces } from '../hooks/useFaces'
import { useFavorite } from '../hooks/useFavorite'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { useRating } from '../hooks/useRating'
import { useSwipeNavigation } from '../hooks/useSwipeNavigation'
import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from '../lib/detailView'
import { readFaceOverlay, writeFaceOverlay } from '../lib/faceOverlayPref'
import { formatDateTimeMinutes } from '../lib/format'
import { editPreviewStyle, isIdentityEdit, NEUTRAL_EDIT } from '../lib/photoEdit'
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

/** Fetch lifecycle of the photo detail (the photo and its stored edit). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; photo: PhotoDetail; edit: PhotoEdit }

/**
 * The panel showing beside the photo, or null for none. Faces and edits both want
 * that one column, so this single choice — rather than a boolean each — is what
 * makes two panels fighting over it unrepresentable: the photo has exactly two
 * layouts (full width, or `lg={8}` with a panel beside it), never a third.
 */
type SidePanel = 'faces' | 'edit' | null

/**
 * The rich photo detail page: exactly ONE preview of the photo, reflecting the
 * saved non-destructive edit (or, while the edit panel is open, the adjustments
 * in progress), with the detected faces drawn as a toggleable overlay on top of
 * it (never a second copy of the image) and prev/next navigation that respects
 * the originating list order. The photo spans the full width of the content area
 * until a side panel opens beside it — the faces list or the edit controls, one
 * at a time.
 * The control/info panels sit BELOW it in a strict edit-first priority order:
 *   1. Organize (albums, tags, people) — the everyday action, always-on inline
 *      editing with no separate "edit mode";
 *   2. Caption & place (title, description, AI description, notes, taken-at,
 *      location) — read-only until an editor clicks a field to reveal the form;
 *   3. Technical details (camera/lens/EXIF, file facts, uploader) — reference
 *      only, collapsed by default.
 * From the `lg` breakpoint up, (1) and (2) share one row — Organize is the
 * narrow 25 % rail beside the 75 % text-heavy Caption & place — and below it
 * every panel stacks full width in exactly that order, so the same reading
 * priority holds on a phone.
 * Every mutation is role-gated; viewers see a read-only page. The whole view is
 * deep-linkable and Back returns to the prior list view (the order/scope is
 * carried in the URL query).
 */
export function PhotoDetailPage() {
  const { t, i18n } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [lightboxOpen, setLightboxOpen] = useState(false)
  // Bumped after a thumbnail is regenerated: the derived image changed under a
  // stable URL, so appending this counter forces the browser to refetch it.
  const [thumbVersion, setThumbVersion] = useState(0)
  // Which panel — if any — is beside the photo. Seeded from the stored
  // face-overlay choice, which is read once from localStorage and written back on
  // every faces toggle, so it carries across photos and reloads. Faces are off by
  // default: the photo is the content, the boxes and their panel are opt-in.
  const [sidePanel, setSidePanel] = useState<SidePanel>(() => (readFaceOverlay() ? 'faces' : null))
  // The adjustments the edit panel is working on, or null for "nothing unsaved".
  // The page owns them because the preview surface is the ONE photo at the top:
  // the panel reports every slider move up here, and the photo re-renders with it.
  const [editDraft, setEditDraft] = useState<PhotoEdit | null>(null)
  // The face hovered on either side of the photo/panel pair, so hovering a box
  // highlights its row and hovering a row highlights its box.
  const [hoveredFace, setHoveredFace] = useState<number | null>(null)
  const faces = useFaces(uid)

  const view = useMemo(() => readUrlState(searchParams, DETAIL_DEFAULTS), [searchParams])
  const neighborParams = useMemo(() => detailToParams(view), [view])
  const detailQuery = detailQueryString(view)
  // A `mode` scope means the photo was opened from search, so prev/next (and the
  // lightbox) must page through `GET /search` in the same ranked order the results
  // grid showed rather than the plain library list.
  const searchMode = view.mode !== '' ? toMode(view.mode) : undefined
  const neighbors = usePhotoNeighbors(uid, neighborParams, true, searchMode)

  // The neighbour's detail URL, carrying the originating order/scope so prev/next
  // keeps paging the same list. Shared by the arrow-key shortcut and the on-image
  // prev/next links.
  const neighborTo = (neighbor: string) =>
    detailQuery === '' ? `/photos/${neighbor}` : `/photos/${neighbor}?${detailQuery}`

  // Page to a neighbour (prev/next) preserving the originating list order. Shared
  // by the on-image ‹/› arrows, the ←/→ keys and the touch swipe so all three
  // navigate identically (same URL/state, same stop-at-ends semantics).
  const goToNeighbor = (neighbor: string | null): void => {
    if (neighbor !== null) {
      void navigate(neighborTo(neighbor), { replace: true })
    }
  }

  // Touch: a horizontal swipe on the image pages to the next/previous photo via
  // the very same navigation as the arrows/keys. A mostly-vertical drag falls
  // through to native page scrolling, and the gesture is ignored when it starts
  // on the face boxes or the ‹/› arrows (see useSwipeNavigation).
  const swipe = useSwipeNavigation({
    onSwipe: (direction) => {
      goToNeighbor(direction === 'next' ? neighbors.next : neighbors.prev)
    },
  })

  // The favorite is lifted here so the header heart and the `f` shortcut share one
  // optimistic toggle. It resyncs to the photo's stored flag once it loads.
  const favorite = useFavorite(
    uid,
    state.status === 'ready' ? (state.photo.is_favorite ?? false) : false,
  )

  // The face UI is derived here, above the loading/error guards below, because the
  // `m` shortcut is registered before them and must see the same booleans the
  // render does. `state` is read without destructuring so it stays legal up here.
  const ready = state.status === 'ready' ? state.photo : null
  // What the one photo previews: the adjustments in progress while the edit panel
  // is open, otherwise the stored edit. Derived up here with the rest of the face
  // UI, which reads it (see `facesAvailable`) and must see what the render does.
  const previewEdit = editDraft ?? (state.status === 'ready' ? state.edit : NEUTRAL_EDIT)
  // The overlay is only ever drawn over the still image: it positions its boxes from
  // normalised bboxes relative to its parent, and a video player's chrome is not the
  // photo. Faces are never detected on clips anyway.
  const isStill = ready !== null && ready.media_type !== 'video' && ready.media_type !== 'live'
  // While a neighbour loads the faces are keyed on the target photo, so they must
  // not be drawn over the still-displayed previous one.
  const loadingNext = ready !== null && ready.uid !== uid
  // A non-identity preview rules the whole face UI out. `FaceOverlay` places its
  // boxes in percentages of the wrapper that shrink-wraps the <img>, while the
  // preview's `clip-path`/`transform` move the rendered pixels underneath them —
  // so the boxes would simply miss the faces. Rather than draw them wrong, the
  // faces stand down (toggle and `m` included) until the preview is neutral again.
  const facesAvailable =
    isStill && !loadingNext && faces.faces.length > 0 && isIdentityEdit(previewEdit)
  const facesVisible = sidePanel === 'faces'
  // One value drives the boxes and the panel: they are the same feature and can
  // never disagree.
  const showFaces = facesAvailable && facesVisible
  // Edits are for stills only — the backend never re-renders a video edit, and the
  // player carries no preview surface to apply them to.
  const showEdit = canWrite && isStill && sidePanel === 'edit'

  // Faces and edits share the one column beside the photo, so showing either one
  // closes the other. The unsaved draft goes with it: the boxes are positioned for
  // the stored photo, so they may only ever be drawn over it.
  const openFaces = () => {
    setSidePanel('faces')
    writeFaceOverlay(true)
    setEditDraft(null)
  }

  // Hiding the overlay also drops the selection: a naming panel for a box the user
  // can no longer see would be orphaned UI.
  const toggleFaces = () => {
    if (facesVisible) {
      setSidePanel(null)
      writeFaceOverlay(false)
      faces.select(null)
      return
    }
    openFaces()
  }

  // Opens the faces panel at a given face — how the Organize person-chips reach the
  // one place people are named.
  const editFace = (faceIndex: number) => {
    openFaces()
    faces.select(faceIndex)
  }

  // Opening the edits takes the column from the faces panel and the boxes with it
  // (they cannot be drawn over an edited preview), so the selection is dropped too.
  // The stored overlay preference is deliberately NOT written: hiding the faces
  // here is a consequence of opening the edits, not a choice about faces, so it
  // survives to the next photo. Closing discards whatever is unsaved, returning
  // the photo to showing exactly what is stored.
  const toggleEdit = () => {
    setSidePanel((prev) => (prev === 'edit' ? null : 'edit'))
    setEditDraft(null)
    faces.select(null)
  }

  // Detail shortcuts: ←/→ page to the previous/next photo, `f` toggles favorite,
  // `m` shows/hides the faces, Escape steps back out (first the selected face, then
  // to the originating list view). Rating keys (0–5, p/r) are handled by the separate
  // effect above. The hook suppresses these while typing, which is what keeps `m`
  // from firing into the name field. They are disabled while the lightbox is open,
  // which owns ←/→ (page the viewer) and Esc (close it).
  useKeyboardShortcuts(
    {
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
      Escape: () => {
        if (faces.selected !== null) {
          faces.select(null)
          return
        }
        void navigate(backHref(view))
      },
    },
    { enabled: !lightboxOpen },
  )

  // The optimistic rating hook (stars + flag) drives both the header controls and
  // the number/p/r hotkeys. It is instantiated before the loading/error guards
  // (hook rules) and resyncs to the photo's stored values once it loads.
  const initialRating = state.status === 'ready' ? (state.photo.rating ?? 0) : 0
  const initialFlag = state.status === 'ready' ? (state.photo.flag ?? 'none') : 'none'
  const rating = useRating(uid, initialRating, initialFlag)
  const { setRating, setFlag } = rating

  // Number keys 0–5 set the rating, p = pick, r = reject — but never while the
  // user is typing in an input/textarea/contenteditable.
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (
        lightboxOpen ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        isTypingElement(event.target)
      ) {
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
  }, [setRating, setFlag, lightboxOpen])

  useEffect(() => {
    const controller = new AbortController()
    // Only blank to the full-page spinner on the very first load. When a photo is
    // already on screen (prev/next navigation), keep it mounted and fetch the next
    // one in the background, then swap in place — no full-page flicker. The abort
    // on `uid` change still cancels the superseded request, so the latest target
    // always wins.
    setState((prev) => (prev.status === 'ready' ? prev : { status: 'loading' }))
    // A draft belongs to the photo it was made on: paging to a neighbour drops it,
    // so the incoming photo is never previewed through the previous one's edit.
    setEditDraft(null)
    Promise.all([fetchPhoto(uid, controller.signal), fetchEdit(uid, controller.signal)])
      .then(([photo, edit]) => {
        setState({ status: 'ready', photo, edit })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        // A failed neighbour fetch surfaces the error instead of silently leaving
        // the previous photo on screen as if navigation had not happened.
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-5">
        <Spinner animation="border" role="status">
          <span className="visually-hidden">{t('photo.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <Alert variant="danger" className="d-flex align-items-center gap-3 flex-wrap">
        <span>{t('photo.error')}</span>
        <BackLink to={backHref(view)} label={t('photo.back')} />
      </Alert>
    )
  }

  const { photo, edit } = state
  // What the photo is called, as a person would say it: its title, or the facts it
  // carries (when, and where). Never the filename — that is the camera's name for
  // it and lives in the technical details. See photoDisplayTitle for the rule.
  const captureDate =
    photo.taken_at !== undefined ? formatDateTimeMinutes(photo.taken_at, i18n.language) : ''
  const displayTitle = photoDisplayTitle(titleSource(photo), captureDate)
  // The one-string form, for the places only text will do: alt text, the video and
  // live-photo players' titles, the lightbox caption.
  const title = photoTitleText(displayTitle, t('photo.untitled'))
  // While paging to a neighbour we keep the current photo (and its edit) mounted
  // and fetch the next one in the background; the displayed photo only matches the
  // route `uid` once that fetch resolves. Until then a subtle overlay marks the
  // load, and the face UI — keyed on the target `uid`, not the shown photo — is
  // held back so photo A never shows photo B's boxes (see `loadingNext` above).
  // The prev/next arrows do track the target `uid` so rapid paging stays responsive
  // (latest target wins).

  const setPhoto = (updated: PhotoDetail) => {
    setState({ status: 'ready', photo: updated, edit })
  }
  // Stack mutations always refresh the photo being viewed (not the member that was
  // mutated), so the variants strip and the member-count reflect the change even
  // when the acting member was a different variant in the strip.
  const reloadPhoto = async () => {
    setPhoto(await fetchPhoto(uid))
  }
  const handleSetStackPrimary = async (memberUid: string) => {
    await setStackPrimary(memberUid)
    await reloadPhoto()
  }
  const handleUnstackMember = async (memberUid: string) => {
    await unstackMember(memberUid)
    await reloadPhoto()
  }
  const handleUnstackAll = async () => {
    await unstackAll(uid)
    await reloadPhoto()
  }
  // A saved edit becomes the stored one and clears the draft, so the photo keeps
  // previewing the very same adjustments — now from `state` rather than in flight.
  const onEditSaved = (saved: PhotoEdit) => {
    setState({ status: 'ready', photo, edit: saved })
    setEditDraft(null)
  }
  // The panel reports an updater, not a finished edit, so adjustments made in the
  // same React batch compose instead of overwriting each other (see EditPanelProps
  // `onChange`). The first one has no draft to build on yet, so it starts from the
  // stored edit — the very thing the photo is previewing at that moment.
  const applyEdit = (update: (prev: PhotoEdit) => PhotoEdit) => {
    setEditDraft((prev) => update(prev ?? edit))
  }
  const onThumbnailRegenerated = () => {
    setThumbVersion((v) => v + 1)
  }

  const basePoster = thumbUrl(photo.uid, 'fit_1920', downloadToken)
  // The thumb URL is built from the UID (stable), so a regenerated thumbnail
  // would otherwise be masked by the browser cache. Append a version once the
  // user regenerates it, so the new image actually shows without a hard reload.
  const poster =
    thumbVersion > 0
      ? `${basePoster}${basePoster.includes('?') ? '&' : '?'}v=${String(thumbVersion)}`
      : basePoster
  // Render the main media by kind: a range-streaming player for videos, a
  // hover/hold motion preview for live photos, and the edit-reflecting still for
  // images. Non-destructive edits apply to images only (the backend never
  // re-renders video edits), so the video/live branches do not carry edit CSS.
  const renderMedia = () => {
    if (photo.media_type === 'video') {
      return (
        <VideoPlayer
          uid={photo.uid}
          title={title}
          poster={poster}
          downloadHref={photo.download_url}
          token={downloadToken}
        />
      )
    }
    if (photo.media_type === 'live') {
      return <LivePhoto uid={photo.uid} title={title} poster={poster} token={downloadToken} />
    }
    // Clicking the still image opens the fullscreen lightbox. Videos/live photos
    // keep their own native fullscreen (the range player / motion clip) and never
    // reach this branch, so they never open the image lightbox. The face boxes are
    // siblings of the button (never nested inside it) and sit in a wrapper that
    // shrink-wraps the image, so their percentage geometry lands on the faces.
    return (
      <div
        className="position-relative d-inline-flex mw-100"
        // pan-y hands vertical drags to native scrolling while reserving
        // horizontal ones for the swipe (and keeps the browser back-swipe off
        // the image); the handlers only read start/end, never preventDefault.
        style={{ touchAction: 'pan-y' }}
        onTouchStart={swipe.onTouchStart}
        onTouchMove={swipe.onTouchMove}
        onTouchEnd={swipe.onTouchEnd}
      >
        <button
          type="button"
          className="border-0 bg-transparent p-0 d-inline-flex"
          style={{ cursor: 'zoom-in' }}
          aria-label={t('photo.lightbox.open')}
          // Marks the image as a valid swipe surface: the swipe is otherwise
          // suppressed on interactive descendants (the face boxes), but the
          // image itself must page.
          data-swipe-surface=""
          onClick={() => {
            setLightboxOpen(true)
          }}
        >
          <img
            src={poster}
            alt={title}
            className="mw-100"
            style={{ maxHeight: '80vh', objectFit: 'contain', ...editPreviewStyle(previewEdit) }}
          />
        </button>
        {showFaces && (
          <FaceOverlay
            faces={faces.faces}
            selected={faces.selected?.face_index ?? null}
            hovered={hoveredFace}
            onSelect={faces.select}
            onHover={setHoveredFace}
            readOnly={!canWrite}
          />
        )}
      </div>
    )
  }

  // Close the lightbox, restoring the detail URL to whichever photo is on screen
  // (the viewer pages the list internally without touching the URL), so browser
  // Back still returns to the originating list view.
  const closeLightbox = (finalUid: string) => {
    setLightboxOpen(false)
    if (finalUid !== photo.uid) {
      void navigate(neighborTo(finalUid), { replace: true })
    }
  }

  return (
    <>
      <div className="d-flex align-items-center gap-2 mb-3 flex-wrap">
        {/* Back link + title stay grouped (min-width:0 lets the title truncate
            instead of pushing the controls off-screen); the controls wrap below
            as a block on narrow widths, so the title is never hidden. */}
        <div className="d-flex align-items-center gap-2 me-auto" style={{ minWidth: 0 }}>
          <BackLink to={backHref(view)} label={t('photo.back')} className="flex-shrink-0" />
          {/* A titled photo is its title. An untitled one is its facts — the date
              carries the line and the place trails it, quieter, because "when"
              identifies a photo before "where" does. Both are one <h1>: it is one
              name for one thing, however many parts it is assembled from. */}
          <h1 className="kk-page-title mb-0 text-truncate">
            {displayTitle.kind === 'facts' ? (
              <>
                {displayTitle.date !== '' && <span>{displayTitle.date}</span>}
                {displayTitle.place !== '' && (
                  <span className="text-secondary">
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
        <div className="d-flex align-items-center gap-2 flex-wrap">
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
        </div>
      </div>

      {/* What the photo is filed under, right under the title: the album/label
          badges answer it at a glance instead of making the reader scroll to the
          Organize card. Read-only and fed from the very same `photo` state the
          Organize panel edits, so a change down there shows here at once. */}
      <OrganizeBadges albums={photo.albums} labels={photo.labels} />

      {/* The photo spans the full width of the content area — until a side panel is
          shown, when it yields a third of it to the faces list or the edit controls
          beside it (below it on a phone). Both panels reflow the same one grid,
          with no animation, and `sidePanel` lets only one of them have it.
          `align-items-start` keeps both columns at their natural height; crucially,
          nothing here may stretch the wrapper that `renderMedia` shrink-wraps around
          the <img>, or the overlay's percentage-positioned boxes would drift off the
          faces. The control/info panels sit below both (see the grid further down). */}
      <Row className="align-items-start g-3">
        <Col xs={12} lg={showFaces || showEdit ? 8 : 12}>
          <div className="position-relative bg-dark rounded overflow-hidden d-flex justify-content-center">
            {renderMedia()}
            {neighbors.prev !== null && (
              <Link
                to={neighborTo(neighbors.prev)}
                replace
                aria-label={t('photo.prev')}
                className="btn btn-dark opacity-75 position-absolute top-50 start-0 translate-middle-y ms-2"
              >
                ‹
              </Link>
            )}
            {neighbors.next !== null && (
              <Link
                to={neighborTo(neighbors.next)}
                replace
                aria-label={t('photo.next')}
                className="btn btn-dark opacity-75 position-absolute top-50 end-0 translate-middle-y me-2"
              >
                ›
              </Link>
            )}
            {/* Paging to a neighbour keeps the current photo visible; a small corner
                spinner marks the background load instead of blanking the whole page
                to a full-screen spinner. */}
            {loadingNext && (
              <div className="position-absolute top-0 end-0 m-2">
                <Spinner animation="border" size="sm" variant="light" role="status">
                  <span className="visually-hidden">{t('photo.loadingNext')}</span>
                </Spinner>
              </div>
            )}
          </div>

          <div className="d-flex gap-2 mt-2 flex-wrap">
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
            {facesAvailable && (
              <Button
                type="button"
                variant={facesVisible ? 'secondary' : 'outline-secondary'}
                size="sm"
                aria-pressed={facesVisible}
                onClick={toggleFaces}
              >
                {facesVisible ? t('faces.hide') : t('faces.toggle')}
              </Button>
            )}
            {/* The edits open right here, beside the photo they edit — the whole
                point of the panel: the original stays in view and previews every
                adjustment live. */}
            {canWrite && isStill && (
              <Button
                type="button"
                variant={showEdit ? 'secondary' : 'outline-secondary'}
                size="sm"
                aria-pressed={showEdit}
                onClick={toggleEdit}
              >
                {t('photo.edit.title')}
              </Button>
            )}
          </div>
        </Col>

        {showFaces && (
          <Col xs={12} lg={4}>
            <FacesPanel
              faces={faces}
              canWrite={canWrite}
              hovered={hoveredFace}
              onHover={setHoveredFace}
              onClose={toggleFaces}
            />
          </Col>
        )}

        {showEdit && (
          <Col xs={12} lg={4}>
            <EditPanel
              uid={photo.uid}
              edit={previewEdit}
              onChange={applyEdit}
              onSaved={onEditSaved}
              onClose={toggleEdit}
            />
          </Col>
        )}
      </Row>

      {/* The variants strip: the several files of one shot grouped into a stack.
          It renders nothing for an unstacked photo (fewer than two members). */}
      {photo.stack_members !== undefined && photo.stack_members.length > 1 && (
        <StackStrip
          members={photo.stack_members}
          currentUid={photo.uid}
          canWrite={canWrite}
          onSetPrimary={handleSetStackPrimary}
          onUnstackMember={handleUnstackMember}
          onUnstackAll={handleUnstackAll}
          detailQuery={detailQuery}
        />
      )}

      {/* Control/info panels below the photo, spanning the full page width, in a
          strict edit-first priority order: Organize first, then Caption & place,
          and finally Technical details. (Photo editing is no longer among them: it
          belongs beside the photo it edits, so it opens as a side panel above.)
          From `lg` up the first two share one row (Organize 4/12, Caption & place
          8/12); below it they stack full width in the same order, so the page stays
          usable on a phone. */}
      {/* `align-items-start` keeps both cards at their natural height: the row
          must not stretch the shorter one into a tall empty box. */}
      <Row className="align-items-start mt-3">
        {/* 1. Organize — the primary block: albums, tags and people, always
            visible and directly editable for an editor (no separate edit mode). */}
        <Col xs={12} lg={4}>
          <Card className="mb-3">
            <Card.Header>{t('photo.sections.organize')}</Card.Header>
            <Card.Body>
              <OrganizePanel photo={photo} canWrite={canWrite} onChanged={setPhoto} />
              <hr />
              <PeoplePanel
                photoUid={photo.uid}
                faces={faces}
                canWrite={canWrite}
                loading={loadingNext}
                onEditFace={editFace}
              />
            </Card.Body>
          </Card>
        </Col>

        {/* 2. Caption & place — title/description/AI description/notes/taken-at/
            location, read-only until an editor clicks a field to edit it. */}
        <Col xs={12} lg={8}>
          <Card className="mb-3">
            <Card.Header>{t('photo.sections.caption')}</Card.Header>
            <Card.Body>
              <MetadataPanel photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* 3. Technical details — reference only, collapsed by default (the
          component owns its own expander). */}
      <Card className="mb-3">
        <Card.Body>
          <TechnicalDetails
            photo={photo}
            canWrite={canWrite}
            onThumbnailRegenerated={onThumbnailRegenerated}
          />
        </Card.Body>
      </Card>

      <div className="mt-4">
        <SimilarPhotos uid={photo.uid} />
      </div>

      {lightboxOpen && photo.media_type !== 'video' && photo.media_type !== 'live' && (
        <Lightbox
          initialUid={photo.uid}
          initialTitle={title}
          initialEdit={edit}
          params={neighborParams}
          mode={searchMode}
          token={downloadToken}
          onClose={closeLightbox}
        />
      )}
    </>
  )
}
