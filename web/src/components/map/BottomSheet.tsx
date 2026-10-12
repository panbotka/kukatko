import {
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useLayoutEffect,
  useRef,
  useState,
} from 'react'

import {
  nextSheetState,
  sheetHeight,
  sheetStops,
  snapSheet,
  type SheetStops,
} from '../../lib/bottomSheet'
import { type SheetState } from '../../lib/mapView'

/** Movement (px) before a press on the grip becomes a drag rather than a tap. */
const DRAG_THRESHOLD_PX = 6

/** Props for {@link BottomSheet}. */
export interface BottomSheetProps {
  /** The stop the sheet rests at. Controlled: the URL owns it. */
  state: SheetState
  /** Called with the stop the user moved the sheet to. */
  onStateChange: (state: SheetState) => void
  /** Accessible name of the sheet region. */
  label: string
  /** Accessible name of the handle when pressing it raises the sheet. */
  growLabel: string
  /** Accessible name of the handle when pressing it lowers the sheet. */
  shrinkLabel: string
  /** The line beside the handle (the count), always visible and draggable. */
  header: ReactNode
  /** What the sheet shows while it peeks (a strip of the first photos). */
  peek: ReactNode
  /** What the raised sheet shows (the whole grid). */
  children: ReactNode
}

/** An in-progress drag of the grip. */
interface Drag {
  pointerId: number
  startY: number
  startHeight: number
  lastY: number
  lastTime: number
  velocity: number
  active: boolean
}

/**
 * The phone's bottom sheet over the map: **peek** (a header and a strip of
 * photos), **half** and **full**. It lives inside the map stage (absolutely
 * positioned, so the page itself never scrolls) and measures the stage to place
 * its stops.
 *
 * The grip — the handle and the header beside it — drags the sheet: the height
 * follows the finger directly (no React render per frame) and on release snaps
 * to a stop, a flick going one stop further (`lib/bottomSheet`). Only the grip
 * claims the gesture (`touch-action: none` there alone), so the grid below
 * still scrolls with a finger, and the pointer is captured only once the press
 * has moved past a few pixels — capturing on `pointerdown` would swallow a
 * plain click on the handle. The handle is also a real button that steps
 * through the stops, which is the whole interface for a keyboard or a screen
 * reader.
 */
export function BottomSheet({
  state,
  onStateChange,
  label,
  growLabel,
  shrinkLabel,
  header,
  peek,
  children,
}: BottomSheetProps) {
  const sheetRef = useRef<HTMLElement | null>(null)
  const dragRef = useRef<Drag | null>(null)
  // A drag that ends over the handle is followed by a click on it; that click
  // must not also step the sheet.
  const swallowClickRef = useRef(false)
  const [stops, setStops] = useState<SheetStops>(() => sheetStops(0))
  const [dragging, setDragging] = useState(false)

  // Measure the stage (the sheet's offset parent) so the stops track rotations,
  // a collapsing URL bar and the keyboard.
  useLayoutEffect(() => {
    const stage = sheetRef.current?.parentElement
    if (stage === null || stage === undefined) {
      return
    }
    const measure = () => {
      setStops(sheetStops(stage.clientHeight))
    }
    measure()
    if (typeof ResizeObserver !== 'function') {
      return
    }
    const observer = new ResizeObserver(measure)
    observer.observe(stage)
    return () => {
      observer.disconnect()
    }
  }, [])

  const height = sheetHeight(state, stops)

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return
    }
    dragRef.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      startHeight: sheetRef.current?.getBoundingClientRect().height ?? height,
      lastY: event.clientY,
      lastTime: event.timeStamp,
      velocity: 0,
      active: false,
    }
  }

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    const sheet = sheetRef.current
    if (drag?.pointerId !== event.pointerId || sheet === null) {
      return
    }
    const dy = event.clientY - drag.startY
    if (!drag.active) {
      if (Math.abs(dy) < DRAG_THRESHOLD_PX) {
        return
      }
      drag.active = true
      setDragging(true)
      try {
        event.currentTarget.setPointerCapture(event.pointerId)
      } catch {
        // A pointer that is no longer active cannot be captured; the drag
        // still follows the events that reach the grip.
      }
    }
    const dt = event.timeStamp - drag.lastTime
    if (dt > 0) {
      // Upward movement grows the sheet, so the sign flips.
      drag.velocity = (drag.lastY - event.clientY) / dt
    }
    drag.lastY = event.clientY
    drag.lastTime = event.timeStamp
    const next = Math.min(stops.full, Math.max(stops.peek, drag.startHeight - dy))
    sheet.style.height = `${String(next)}px`
  }

  const endDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (drag?.pointerId !== event.pointerId) {
      return
    }
    dragRef.current = null
    if (!drag.active) {
      return
    }
    swallowClickRef.current = true
    setDragging(false)
    const sheet = sheetRef.current
    const released = sheet?.getBoundingClientRect().height ?? height
    // Hand the height back to React: the controlled stop sets it from here on.
    if (sheet !== null) {
      sheet.style.height = ''
    }
    const target = snapSheet(released, drag.velocity, stops)
    if (target !== state) {
      onStateChange(target)
    }
  }

  const onHandleClick = () => {
    if (swallowClickRef.current) {
      swallowClickRef.current = false
      return
    }
    onStateChange(nextSheetState(state))
  }

  const raised = state !== 'peek'
  return (
    <section
      ref={sheetRef}
      className={dragging ? 'kk-map-sheet is-dragging' : 'kk-map-sheet'}
      data-state={state}
      aria-label={label}
      style={{ height: `${String(height)}px` }}
    >
      <div
        className="kk-map-sheet-grip"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <button
          type="button"
          className="kk-map-sheet-handle"
          aria-label={state === 'full' ? shrinkLabel : growLabel}
          aria-expanded={raised}
          onClick={onHandleClick}
        >
          <span className="kk-map-sheet-bar" aria-hidden="true" />
        </button>
        <div className="kk-map-sheet-header">{header}</div>
      </div>
      <div className="kk-map-sheet-body">{raised ? children : peek}</div>
    </section>
  )
}
