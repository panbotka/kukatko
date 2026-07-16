import { useCallback, useEffect, useRef, useState } from 'react'

import {
  type Box,
  IDENTITY_VIEW,
  MIN_SCALE,
  ZOOM_STEP,
  type ZoomView,
  isZoomed,
  panBy,
  zoomAt,
  zoomCentre,
} from '../lib/compareZoom'

/** Options for {@link useSyncZoom}. */
export interface UseSyncZoomOptions {
  /** Changing this resets the zoom — pass the pair's id, so a new pair starts fit-to-pane. */
  resetKey: string
}

/** The synchronised zoom state plus the handlers that drive it. */
export interface SyncZoom {
  /** The one view both panes render. */
  view: ZoomView
  /** Whether the images are magnified (so a drag pans rather than doing nothing). */
  zoomed: boolean
  /** True while a drag is in progress, so the panes can drop their CSS transition. */
  dragging: boolean
  /** Handlers to spread onto each pane element. Both panes get the same ones. */
  handlers: {
    onWheel: (event: React.WheelEvent<HTMLElement>) => void
    onPointerDown: (event: React.PointerEvent<HTMLElement>) => void
    onPointerMove: (event: React.PointerEvent<HTMLElement>) => void
    onPointerUp: (event: React.PointerEvent<HTMLElement>) => void
    onPointerCancel: (event: React.PointerEvent<HTMLElement>) => void
    onDoubleClick: (event: React.MouseEvent<HTMLElement>) => void
  }
  /** Zooms in about the pane centre (the +/keyboard control). */
  zoomIn: () => void
  /** Zooms out about the pane centre (the −/keyboard control). */
  zoomOut: () => void
  /** Returns both images to fit-to-pane. */
  reset: () => void
}

/**
 * Drives the duplicate compare view's two images from a single zoom/pan state, so
 * they are synchronised by construction: there is one {@link ZoomView}, both panes
 * render it, and any gesture on either pane updates it. Nothing has to be copied
 * from one pane to the other, so the two cannot drift apart — which is the whole
 * value of the feature, since a comparison of two images at different zooms is
 * worse than no comparison at all.
 *
 * Mouse wheel zooms about the cursor, drag pans while zoomed, double-click toggles
 * between fit and a close look. The zoom resets whenever `resetKey` changes, so
 * advancing to the next pair never inherits the last one's magnification.
 *
 * The gesture math lives in `lib/compareZoom.ts`; this hook is only the effects —
 * pointer bookkeeping, the measured pane box, and the reset.
 */
export function useSyncZoom({ resetKey }: UseSyncZoomOptions): SyncZoom {
  const [view, setView] = useState<ZoomView>(IDENTITY_VIEW)
  const [dragging, setDragging] = useState(false)
  // The last pointer position, in client coordinates: a drag is a stream of deltas
  // and only the previous point matters, so it never needs to trigger a render.
  const lastPoint = useRef<{ x: number; y: number } | null>(null)
  // The pane the gesture is happening in. Both panes are the same size, so either
  // one measures the box the math needs.
  const boxRef = useRef<Box>({ width: 0, height: 0 })

  useEffect(() => {
    setView(IDENTITY_VIEW)
  }, [resetKey])

  // paneBox measures the element the event landed on and remembers it, so the
  // zoom/pan clamps use the pane's real size rather than a guess.
  const paneBox = useCallback((element: HTMLElement): Box => {
    const rect = element.getBoundingClientRect()
    const box = { width: rect.width, height: rect.height }
    boxRef.current = box
    return box
  }, [])

  /** Clears the drag latch. Shared by pointerup, pointercancel and the buttons-up guard. */
  const endDrag = useCallback(() => {
    lastPoint.current = null
    setDragging(false)
  }, [])

  const onWheel = useCallback(
    (event: React.WheelEvent<HTMLElement>) => {
      const element = event.currentTarget
      const rect = element.getBoundingClientRect()
      const box = paneBox(element)
      // Scrolling up (negative deltaY) zooms in, matching every map and image
      // viewer the user already has muscle memory for.
      const factor = event.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP
      setView((prev) =>
        zoomAt(prev, factor, event.clientX - rect.left, event.clientY - rect.top, box),
      )
    },
    [paneBox],
  )

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      // Panning is only meaningful once magnified; at fit-to-pane a drag would move
      // nothing, so leave the pointer alone (text selection, etc. keep working).
      if (!isZoomed(view)) {
        return
      }
      paneBox(event.currentTarget)
      event.currentTarget.setPointerCapture(event.pointerId)
      lastPoint.current = { x: event.clientX, y: event.clientY }
      setDragging(true)
    },
    [paneBox, view],
  )

  const onPointerMove = useCallback((event: React.PointerEvent<HTMLElement>) => {
    const last = lastPoint.current
    if (last === null) {
      return
    }
    // A drag can end without a pointerup — a touch cancelled by the OS, a context
    // menu stealing the gesture — which would leave the drag latched and pan the
    // image on every later button-less move. `buttons === 0` means nothing is held,
    // so the drag is over whether or not we were told.
    if (event.buttons === 0) {
      endDrag()
      return
    }
    const dx = event.clientX - last.x
    const dy = event.clientY - last.y
    lastPoint.current = { x: event.clientX, y: event.clientY }
    setView((prev) => panBy(prev, dx, dy, boxRef.current))
  }, [])

  const onPointerUp = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (lastPoint.current === null) {
        return
      }
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId)
      }
      endDrag()
    },
    [endDrag],
  )

  const onDoubleClick = useCallback(
    (event: React.MouseEvent<HTMLElement>) => {
      const element = event.currentTarget
      const rect = element.getBoundingClientRect()
      const box = paneBox(element)
      setView((prev) =>
        // Already magnified: a double-click means "back out", so go to fit rather
        // than zooming further and trapping the user in a deeper zoom.
        isZoomed(prev)
          ? IDENTITY_VIEW
          : zoomAt(
              prev,
              DOUBLE_CLICK_SCALE,
              event.clientX - rect.left,
              event.clientY - rect.top,
              box,
            ),
      )
    },
    [paneBox],
  )

  const zoomIn = useCallback(() => {
    setView((prev) => zoomCentre(prev, ZOOM_STEP, boxRef.current))
  }, [])

  const zoomOut = useCallback(() => {
    setView((prev) => zoomCentre(prev, 1 / ZOOM_STEP, boxRef.current))
  }, [])

  const reset = useCallback(() => {
    setView(IDENTITY_VIEW)
  }, [])

  return {
    view,
    zoomed: view.scale > MIN_SCALE,
    dragging,
    handlers: {
      onWheel,
      onPointerDown,
      onPointerMove,
      onPointerUp,
      // A cancelled gesture ends the drag exactly like a release does.
      onPointerCancel: onPointerUp,
      onDoubleClick,
    },
    zoomIn,
    zoomOut,
    reset,
  }
}

/**
 * Where a double-click lands. Close enough to judge sharpness and compression on a
 * typical pane, without the disorientation of jumping straight to the pixel level.
 */
const DOUBLE_CLICK_SCALE = 3
