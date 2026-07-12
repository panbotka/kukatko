import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { usePhotoNeighbors } from '../../hooks/usePhotoNeighbors'
import { usePinchZoom } from '../../hooks/usePinchZoom'
import { editPreviewStyle, editTransform, NEUTRAL_EDIT } from '../../lib/photoEdit'
import {
  fetchEdit,
  fetchPhoto,
  type PhotoEdit,
  type PhotoListParams,
  type SearchMode,
  thumbUrl,
} from '../../services/photos'

import './lightbox.css'

/** Preview size for the lightbox stage: a large fit-to-box preview, not a tile. */
const PREVIEW_SIZE = 'fit_1920'

/** Props for {@link Lightbox}. */
export interface LightboxProps {
  /** UID of the photo the viewer opens on. */
  initialUid: string
  /** Title of the initial photo (shown until a navigated photo loads). */
  initialTitle: string
  /** Saved edit of the initial photo, applied to the preview via CSS. */
  initialEdit: PhotoEdit
  /**
   * List params describing the originating scope + filters/sort, so prev/next
   * follows the same order the detail page uses ({@link usePhotoNeighbors}).
   */
  params: PhotoListParams
  /**
   * Search ranking mode when the photo was opened from a search, so the viewer
   * pages through `GET /search` in the same order the detail page does. Omitted
   * for library/album/label/favorites, which page the plain list endpoint.
   */
  mode?: SearchMode
  /** Download token appended to the media URL for cookie-less contexts. */
  token?: string | null
  /**
   * Close the viewer. Receives the UID currently on screen so the caller can
   * restore the detail URL to the last-viewed photo ("Back always works").
   */
  onClose: (finalUid: string) => void
}

/**
 * A fullscreen single-photo lightbox: the image as large as possible on a dark
 * backdrop, honoring the saved non-destructive edit, with large previous/next
 * arrows, a close button and backdrop-to-close. It owns the displayed photo and
 * pages through the originating list via {@link usePhotoNeighbors} (same order and
 * scope as the detail page's prev/next, stopping at the ends), fetching each
 * photo's title and edit on navigation and preloading the neighbours so stepping
 * feels instant. Keyboard (← → Esc), touch swipe, and pinch/double-tap zoom with
 * drag-to-pan ({@link usePinchZoom}) are wired; closing hands the currently shown
 * UID back so the caller can keep the URL correct. This is the quick single-photo
 * viewer, distinct from the `/slideshow` auto-advance stage.
 */
export function Lightbox({
  initialUid,
  initialTitle,
  initialEdit,
  params,
  mode,
  token,
  onClose,
}: LightboxProps) {
  const { t } = useTranslation()
  const [uid, setUid] = useState(initialUid)
  const [title, setTitle] = useState(initialTitle)
  const [edit, setEdit] = useState<PhotoEdit>(initialEdit)
  // The initial photo's title/edit arrive via props, so skip the redundant fetch
  // for the photo we opened on; later navigation fetches the shown photo's data.
  const skipInitialFetch = useRef(true)

  const neighbors = usePhotoNeighbors(uid, params, true, mode)

  // Fetch the shown photo's title and saved edit when navigating to a neighbour,
  // so the caption and the applied edit match the image on screen.
  useEffect(() => {
    if (skipInitialFetch.current) {
      skipInitialFetch.current = false
      return
    }
    const controller = new AbortController()
    Promise.all([fetchPhoto(uid, controller.signal), fetchEdit(uid, controller.signal)])
      .then(([photo, photoEdit]) => {
        setTitle(photo.title !== '' ? photo.title : photo.file_name)
        setEdit(photoEdit)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        // On a failed lookup keep showing the image with a neutral edit rather
        // than surfacing an error inside the immersive viewer.
        setEdit(NEUTRAL_EDIT)
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  const goPrev = useCallback(() => {
    if (neighbors.prev !== null) {
      setUid(neighbors.prev)
    }
  }, [neighbors.prev])

  const goNext = useCallback(() => {
    if (neighbors.next !== null) {
      setUid(neighbors.next)
    }
  }, [neighbors.next])

  const close = useCallback(() => {
    onClose(uid)
  }, [onClose, uid])

  // Keyboard controls: ←/→ page, Esc closes (restoring the URL to the shown photo).
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      switch (event.key) {
        case 'ArrowLeft':
          event.preventDefault()
          goPrev()
          break
        case 'ArrowRight':
          event.preventDefault()
          goNext()
          break
        case 'Escape':
          event.preventDefault()
          close()
          break
        default:
          break
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [goPrev, goNext, close])

  // Preload the adjacent photos at preview size so stepping feels instant.
  useEffect(() => {
    for (const neighbor of [neighbors.prev, neighbors.next]) {
      if (neighbor !== null) {
        const img = new Image()
        img.src = thumbUrl(neighbor, PREVIEW_SIZE, token ?? undefined)
      }
    }
  }, [neighbors.prev, neighbors.next, token])

  // Touch gestures: pinch/double-tap to zoom with drag-to-pan while zoomed, and
  // a horizontal swipe to page while at rest. The zoom resets on every photo
  // change (`resetKey={uid}`) and, because the lightbox unmounts on close, when
  // it closes too. Desktop mouse/keyboard input never reaches these handlers.
  const zoom = usePinchZoom({
    resetKey: uid,
    onSwipe: (direction) => {
      if (direction === 'next') {
        goNext()
      } else {
        goPrev()
      }
    },
  })

  // Close only when the backdrop itself is clicked, not the image or a control
  // (those are children, so the target is never the backdrop element).
  const onBackdropClick = useCallback(
    (event: React.MouseEvent): void => {
      if (event.target === event.currentTarget) {
        close()
      }
    },
    [close],
  )

  return (
    <div
      className="lightbox"
      role="dialog"
      aria-modal="true"
      aria-label={t('photo.lightbox.label')}
      onClick={onBackdropClick}
      onTouchStart={zoom.handlers.onTouchStart}
      onTouchMove={zoom.handlers.onTouchMove}
      onTouchEnd={zoom.handlers.onTouchEnd}
    >
      <Button
        variant="dark"
        className="lightbox__close"
        aria-label={t('photo.lightbox.close')}
        onClick={close}
      >
        ✕
      </Button>

      <img
        key={uid}
        className="lightbox__image"
        src={thumbUrl(uid, PREVIEW_SIZE, token ?? undefined)}
        alt={title}
        style={{
          ...editPreviewStyle(edit),
          // Compose the zoom/pan transform with the saved rotation (rotate
          // first, then scale/translate the rotated image); editPreviewStyle
          // already set `transform` for rotation, so re-append it here.
          transform: `translate(${String(zoom.translateX)}px, ${String(zoom.translateY)}px) scale(${String(zoom.scale)})${
            editTransform(edit) === 'none' ? '' : ` ${editTransform(edit)}`
          }`,
          transition: zoom.gesturing ? 'none' : 'transform 0.18s ease-out',
          cursor: zoom.isZoomed ? 'grab' : 'zoom-in',
        }}
        draggable={false}
      />

      {neighbors.prev !== null && (
        <Button
          variant="dark"
          className="lightbox__nav lightbox__nav--prev"
          aria-label={t('photo.prev')}
          onClick={goPrev}
        >
          ‹
        </Button>
      )}
      {neighbors.next !== null && (
        <Button
          variant="dark"
          className="lightbox__nav lightbox__nav--next"
          aria-label={t('photo.next')}
          onClick={goNext}
        >
          ›
        </Button>
      )}
    </div>
  )
}
