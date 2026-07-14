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
import { Icon } from '../components/Icon'
import { FavoriteToggle } from '../components/library/FavoriteButton'
import { FlagControl } from '../components/library/FlagControl'
import { RatingStars } from '../components/library/RatingStars'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { EditPanel } from '../components/photo/EditPanel'
import { Lightbox } from '../components/photo/Lightbox'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PeoplePanel } from '../components/photo/PeoplePanel'
import { PrivacyToggle } from '../components/photo/PrivacyToggle'
import { TechnicalDetails } from '../components/photo/TechnicalDetails'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { useFaces } from '../hooks/useFaces'
import { useFavorite } from '../hooks/useFavorite'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { useRating } from '../hooks/useRating'
import { useSwipeNavigation } from '../hooks/useSwipeNavigation'
import { backHref, DETAIL_DEFAULTS, detailQueryString, detailToParams } from '../lib/detailView'
import { readFaceOverlay, writeFaceOverlay } from '../lib/faceOverlayPref'
import { editPreviewStyle, isIdentityEdit } from '../lib/photoEdit'
import { isTypingElement, ratingHotkey } from '../lib/ratingHotkeys'
import { toMode } from '../lib/searchView'
import { readUrlState } from '../lib/urlState'
import {
  downloadUrl,
  fetchEdit,
  fetchPhoto,
  type PhotoDetail,
  type PhotoEdit,
  thumbUrl,
} from '../services/photos'

/** Fetch lifecycle of the photo detail (the photo and its stored edit). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; photo: PhotoDetail; edit: PhotoEdit }

/**
 * The rich photo detail page: exactly ONE preview of the photo, reflecting the
 * saved non-destructive edit, with the detected faces drawn as a toggleable
 * overlay on top of it (never a second copy of the image) and prev/next
 * navigation that respects the originating list order. The photo spans the full
 * width of the content area; the control/info panels sit BELOW it in a strict
 * edit-first priority order:
 *   1. Organize (albums, tags, people) — the everyday action, always-on inline
 *      editing with no separate "edit mode";
 *   2. Caption & place (title, description, AI description, notes, taken-at,
 *      location) — read-only until an editor clicks a field to reveal the form;
 *   3. Technical details (camera/lens/EXIF, file facts, uploader) — reference
 *      only, collapsed by default;
 *   4. Photo editing (crop/rotate/brightness/contrast) — rare, editor-only,
 *      collapsed at the very bottom so it never competes with Organize.
 * From the `lg` breakpoint up, (1) and (2) share one row — Organize is the
 * narrow 25 % rail beside the 75 % text-heavy Caption & place — and below it
 * every panel stacks full width in exactly that order, so the same reading
 * priority holds on a phone.
 * Every mutation is role-gated; viewers see a read-only page. The whole view is
 * deep-linkable and Back returns to the prior list view (the order/scope is
 * carried in the URL query).
 */
export function PhotoDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [lightboxOpen, setLightboxOpen] = useState(false)
  // Bumped after a thumbnail is regenerated: the derived image changed under a
  // stable URL, so appending this counter forces the browser to refetch it.
  const [thumbVersion, setThumbVersion] = useState(0)
  // The edits card carries its own preview image, so — like the old
  // `mountOnEnter` edit tab — it stays unmounted until the user opens it, keeping
  // the detail page at exactly one copy of the photo on first render.
  const [editOpen, setEditOpen] = useState(false)
  // The face-overlay choice is read once from localStorage and written back on
  // every toggle, so it carries across photos and reloads.
  const [facesVisible, setFacesVisible] = useState(readFaceOverlay)
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

  // Detail shortcuts: ←/→ page to the previous/next photo, `f` toggles favorite,
  // Escape returns to the originating list view. Rating keys (0–5, p/r) are handled
  // by the separate effect above. The hook suppresses these while typing. They are
  // disabled while the lightbox is open, which owns ←/→ (page the viewer) and Esc
  // (close it).
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
      Escape: () => {
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
  const title = photo.title !== '' ? photo.title : photo.file_name
  // While paging to a neighbour we keep the current photo (and its edit) mounted
  // and fetch the next one in the background; the displayed photo only matches the
  // route `uid` once that fetch resolves. Until then a subtle overlay marks the
  // load, and the face UI — keyed on the target `uid`, not the shown photo — is
  // held back so photo A never shows photo B's boxes. The prev/next arrows do
  // track the target `uid` so rapid paging stays responsive (latest target wins).
  const loadingNext = photo.uid !== uid

  const setPhoto = (updated: PhotoDetail) => {
    setState({ status: 'ready', photo: updated, edit })
  }
  const setEdit = (updated: PhotoEdit) => {
    setState({ status: 'ready', photo, edit: updated })
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
  const isStill = photo.media_type !== 'video' && photo.media_type !== 'live'
  // The overlay is only ever drawn over the still image: it positions its boxes
  // from normalised bboxes relative to its parent, and a video player's chrome is
  // not the photo. Faces are never detected on clips anyway. While a neighbour
  // loads (`loadingNext`) the boxes are keyed on the target photo, so they must
  // not be drawn over the still-displayed previous one.
  const showFaceBoxes = isStill && facesVisible && faces.faces.length > 0 && !loadingNext

  // Hiding the overlay also drops the selection: a naming panel for a box the user
  // can no longer see would be orphaned UI.
  const toggleFaces = () => {
    const next = !facesVisible
    setFacesVisible(next)
    writeFaceOverlay(next)
    if (!next) {
      faces.select(null)
    }
  }

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
            style={{ maxHeight: '80vh', objectFit: 'contain', ...editPreviewStyle(edit) }}
          />
        </button>
        {showFaceBoxes && (
          <FaceOverlay
            faces={faces.faces}
            selected={faces.selected?.face_index ?? null}
            onSelect={faces.select}
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
          <h1 className="kk-page-title mb-0 text-truncate">{title}</h1>
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
          {canWrite && <PrivacyToggle photo={photo} onUpdated={setPhoto} />}
        </div>
      </div>

      {/* The photo spans the full width of the content area; the control/info
          panels sit below it (see the grid further down). */}
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
        {isStill && faces.faces.length > 0 && !loadingNext && (
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
      </div>

      {/* Control/info panels below the photo, in a strict edit-first priority
          order (a single readable column, centred on wide screens): Organize
          first, then Caption & place, Technical details, and finally Photo
          editing. From `lg` up the first two share one row (Organize 25 %,
          Caption & place 75 %); below it they stack full width in the same
          order, so the page stays usable on a phone. */}
      <Row className="justify-content-center mt-3">
        <Col xs={12} xl={9} xxl={8}>
          {/* `align-items-start` keeps both cards at their natural height: the
              row must not stretch the shorter one into a tall empty box. */}
          <Row className="align-items-start">
            {/* 1. Organize — the primary block: albums, tags and people, always
                visible and directly editable for an editor (no separate edit mode). */}
            <Col xs={12} lg={3}>
              <Card className="mb-3">
                <Card.Header>{t('photo.sections.organize')}</Card.Header>
                <Card.Body>
                  <OrganizePanel photo={photo} canWrite={canWrite} onChanged={setPhoto} />
                  <hr />
                  <PeoplePanel faces={faces} canWrite={canWrite} loading={loadingNext} />
                </Card.Body>
              </Card>
            </Col>

            {/* 2. Caption & place — title/description/AI description/notes/taken-at/
                location, read-only until an editor clicks a field to edit it. */}
            <Col xs={12} lg={9}>
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

          {/* 4. Photo editing — last, editor only, collapsed by default. The edits
              card owns its own preview <img>; keeping it collapsed until opened
              means the page still carries exactly one copy of the photo. */}
          {canWrite && (
            <Card className="mb-3">
              <Card.Header className="p-0">
                <Button
                  variant="link"
                  className="w-100 d-flex align-items-center justify-content-between text-decoration-none text-reset px-3 py-2"
                  aria-expanded={editOpen}
                  aria-controls="photo-edit-region"
                  onClick={() => {
                    setEditOpen(!editOpen)
                  }}
                >
                  <span>{t('photo.edit.title')}</span>
                  <Icon name={editOpen ? 'chevron-down' : 'chevron-right'} />
                </Button>
              </Card.Header>
              {editOpen && (
                <Card.Body id="photo-edit-region">
                  <EditPanel uid={photo.uid} edit={edit} onSaved={setEdit} />
                </Card.Body>
              )}
            </Card>
          )}
        </Col>
      </Row>

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
