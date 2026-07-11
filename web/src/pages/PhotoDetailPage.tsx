import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import Tab from 'react-bootstrap/Tab'
import Tabs from 'react-bootstrap/Tabs'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { FavoriteToggle } from '../components/library/FavoriteButton'
import { FlagControl } from '../components/library/FlagControl'
import { RatingStars } from '../components/library/RatingStars'
import { SimilarPhotos } from '../components/library/SimilarPhotos'
import { EditPanel } from '../components/photo/EditPanel'
import { Lightbox } from '../components/photo/Lightbox'
import { LivePhoto } from '../components/photo/LivePhoto'
import { MetadataPanel } from '../components/photo/MetadataPanel'
import { OrganizePanel } from '../components/photo/OrganizePanel'
import { PhotoLocation } from '../components/photo/PhotoLocation'
import { TechnicalDetails } from '../components/photo/TechnicalDetails'
import { VideoPlayer } from '../components/photo/VideoPlayer'
import { FaceAssignPanel } from '../components/people/FaceAssignPanel'
import { FaceOverlay } from '../components/people/FaceOverlay'
import { useFaces } from '../hooks/useFaces'
import { useFavorite } from '../hooks/useFavorite'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { usePhotoNeighbors } from '../hooks/usePhotoNeighbors'
import { useRating } from '../hooks/useRating'
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
 * navigation that respects the originating list order. The right-hand panel
 * leads with what matters — title, description, albums and labels — and demotes
 * camera/lens/EXIF and the file facts to a collapsed expander; location and the
 * edit tools stay on their own tabs. Every mutation is role-gated; viewers see a
 * read-only page. The whole view is deep-linkable and Back returns to the prior
 * list view (the order/scope is carried in the URL query).
 */
export function PhotoDetailPage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite, downloadToken } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [lightboxOpen, setLightboxOpen] = useState(false)
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
        if (neighbors.prev !== null) {
          void navigate(neighborTo(neighbors.prev), { replace: true })
        }
      },
      ArrowRight: () => {
        if (neighbors.next !== null) {
          void navigate(neighborTo(neighbors.next), { replace: true })
        }
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
      <Alert variant="danger">
        {t('photo.error')} <Link to={backHref(view)}>{t('photo.back')}</Link>
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

  const poster = thumbUrl(photo.uid, 'fit_1920', downloadToken)
  const selectedFace = faces.selected
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
      <div className="position-relative d-inline-flex mw-100">
        <button
          type="button"
          className="border-0 bg-transparent p-0 d-inline-flex"
          style={{ cursor: 'zoom-in' }}
          aria-label={t('photo.lightbox.open')}
          onClick={() => {
            setLightboxOpen(true)
          }}
        >
          <img
            src={poster}
            alt={title}
            className="mw-100"
            style={{ maxHeight: '70vh', objectFit: 'contain', ...editPreviewStyle(edit) }}
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
        <Link to={backHref(view)} className="text-decoration-none">
          ← {t('photo.back')}
        </Link>
        <h1 className="kk-page-title mb-0 text-truncate">{title}</h1>
        <div className="ms-auto d-flex align-items-center gap-2 flex-wrap">
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

      <Row className="g-3">
        <Col xs={12} lg={7}>
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
            {/* Paging to a neighbour keeps the current photo visible; a small
                corner spinner marks the background load instead of blanking the
                whole page to a full-screen spinner. */}
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

          {/* Faces never get an image of their own: they are the overlay above.
              A photo with none says so in one line, and the naming panel opens
              below the preview when a box is picked. */}
          {faces.status === 'ready' && faces.faces.length === 0 && !loadingNext && (
            <p className="text-secondary small mt-2 mb-0">{t('faces.none')}</p>
          )}
          {faces.actionError && (
            <Alert variant="danger" className="mt-2 py-2 small">
              {t('faces.assignError')}
            </Alert>
          )}
          {selectedFace !== null && canWrite && (
            <FaceAssignPanel
              face={selectedFace}
              busy={faces.busy}
              onAcceptSuggestion={(suggestion) => {
                faces.acceptSuggestion(selectedFace, suggestion)
              }}
              onAssignName={(name) => {
                faces.assignName(selectedFace, name)
              }}
              onUnassign={() => {
                faces.unassign(selectedFace)
              }}
              onClose={() => {
                faces.select(null)
              }}
            />
          )}
        </Col>

        <Col xs={12} lg={5}>
          {/* `mountOnEnter` keeps the Edit tab's own preview image out of the DOM
              until an editor opens it, so the page carries exactly one image of
              the photo on first render. */}
          <Tabs defaultActiveKey="info" className="mb-3" mountOnEnter>
            <Tab eventKey="info" title={t('photo.tabs.info')}>
              <MetadataPanel photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
              <hr />
              <OrganizePanel photo={photo} canWrite={canWrite} onChanged={setPhoto} />
              <hr />
              <TechnicalDetails photo={photo} />
            </Tab>
            <Tab eventKey="location" title={t('photo.tabs.location')}>
              <PhotoLocation photo={photo} canWrite={canWrite} onUpdated={setPhoto} />
            </Tab>
            {canWrite && (
              <Tab eventKey="edit" title={t('photo.tabs.edit')}>
                <EditPanel uid={photo.uid} edit={edit} onSaved={setEdit} />
              </Tab>
            )}
          </Tabs>
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
