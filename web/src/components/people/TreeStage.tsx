import {
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { AVATAR_CIRCLE } from '../../lib/familyLayout'
import {
  centreOn,
  fitView,
  panBy,
  type Size,
  type TreeView,
  ZOOM_STEP,
  zoomAt,
} from '../../lib/treeView'
import { Icon } from '../Icon'

import { AVATAR_CLIP_ID } from './TreePersonCard'

import './familyTree.css'

/** How far a pointer may travel during a click before it counts as a drag. */
const DRAG_SLOP_PX = 4

/** The stage size assumed before the container has been measured (jsdom, chiefly). */
const FALLBACK_STAGE = { width: 1024, height: 640 }

/** Props for {@link TreeStage}. */
export interface TreeStageProps {
  /** The drawing's own size in layout units, which is what the view is fitted to. */
  content: Size
  /**
   * What the drawing is *of*. When it changes the reader's pan and zoom are
   * dropped, because a new drawing is not a new view of the old one; folding a
   * branch or opening a dialog must leave the view exactly where it was.
   */
  resetKey: string
  /** The point (in layout units) the "back to the person" button returns to. */
  focus?: { x: number; y: number } | null
  /** The accessible name of that button, which differs per direction. */
  focusLabel?: string
  /** The drawing itself, in layout units, painted inside the transformed group. */
  children: ReactNode
}

/** The live size of the stage element, in CSS pixels. */
function useStageSize(ref: React.RefObject<HTMLElement | null>): Size {
  const [size, setSize] = useState<Size>(FALLBACK_STAGE)

  useEffect(() => {
    const element = ref.current
    if (element === null) {
      return
    }
    const measure = () => {
      const rect = element.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) {
        return
      }
      setSize((current) =>
        Math.abs(current.width - rect.width) < 0.5 && Math.abs(current.height - rect.height) < 0.5
          ? current
          : { width: rect.width, height: rect.height },
      )
    }
    measure()
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null
    observer?.observe(element)
    if (observer === null) {
      window.addEventListener('resize', measure)
    }
    return () => {
      observer?.disconnect()
      if (observer === null) {
        window.removeEventListener('resize', measure)
      }
    }
  }, [ref])

  return size
}

/**
 * The pannable, zoomable sheet of paper both family drawings are painted on:
 * drag to pan, wheel or buttons to zoom, one button to fit the whole thing and
 * one to come back to the person it is about.
 *
 * It is shared by {@link FamilyTreeCanvas} and {@link FamilyPedigreeCanvas}
 * because the two directions are two *layouts*, not two pages: the design splits
 * the tree by direction precisely because a tidy tree and a binary pedigree are
 * different shapes — but the sheet they are drawn on, and everything a reader
 * does to move around it, is the same in both, and a second copy of this would
 * be a second thing to keep in step.
 *
 * Everything inside the transformed `<g>` is in **layout units**, font sizes
 * included, so type shrinks with the drawing rather than staying stubbornly
 * 13 px while the tree gets smaller around it.
 *
 * The view itself (pan and zoom) is deliberately **not** in the URL: it is where
 * the reader's eye is rather than what they are looking at, and a history entry
 * per wheel notch would bury the states that matter.
 */
export function TreeStage({ content, resetKey, focus, focusLabel, children }: TreeStageProps) {
  const { t } = useTranslation()
  const stageRef = useRef<HTMLDivElement>(null)
  const size = useStageSize(stageRef)
  const [moved, setMoved] = useState<TreeView | null>(null)
  const dragRef = useRef<{ x: number; y: number; travelled: number; captured: boolean } | null>(
    null,
  )
  const draggedRef = useRef(false)

  const fitted = useMemo(() => fitView(content, size), [content, size])
  const view = moved ?? fitted

  useEffect(() => {
    setMoved(null)
  }, [resetKey])

  // The wheel listener is attached by hand because React's own is passive, and
  // a passive listener cannot preventDefault — without which the wheel scrolls
  // the page instead of zooming the tree.
  useEffect(() => {
    const element = stageRef.current
    if (element === null) {
      return
    }
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = element.getBoundingClientRect()
      const factor = event.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP
      setMoved((current) =>
        zoomAt(current ?? fitted, factor, event.clientX - rect.left, event.clientY - rect.top),
      )
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      element.removeEventListener('wheel', onWheel)
    }
  }, [fitted])

  const onPointerDown = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.button !== 0) {
      return
    }
    // The pointer is deliberately *not* captured here. Capture retargets every
    // mouse event derived from this pointer to the capturing element, so a
    // capture taken before the reader has moved at all sends the `mouseup` —
    // and with it the `click` — to this `<svg>` instead of to whatever was
    // pressed. Every control inside the drawing (a person, a fold circle, an
    // empty slot) would be dead to a mouse while still working under a finger,
    // which is exactly the kind of bug no jsdom test can see. It is taken in
    // onPointerMove instead, once the travel says this is a drag.
    dragRef.current = { x: event.clientX, y: event.clientY, travelled: 0, captured: false }
    draggedRef.current = false
  }

  const onPointerMove = (event: ReactPointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current
    if (drag === null) {
      return
    }
    // Until the capture below is taken a release outside the stage is never
    // heard, so a button that is no longer held ends the drag here — otherwise
    // the drawing would follow the bare cursor around.
    if (event.buttons === 0) {
      dragRef.current = null
      return
    }
    const dx = event.clientX - drag.x
    const dy = event.clientY - drag.y
    drag.x = event.clientX
    drag.y = event.clientY
    drag.travelled += Math.abs(dx) + Math.abs(dy)
    if (drag.travelled > DRAG_SLOP_PX) {
      draggedRef.current = true
      if (!drag.captured) {
        // Now that this is a drag, capture the pointer so it keeps panning
        // outside the stage's bounds — and so the click this gesture would
        // otherwise deliver to a person never reaches them.
        drag.captured = true
        event.currentTarget.setPointerCapture(event.pointerId)
      }
    }
    setMoved((current) => panBy(current ?? fitted, dx, dy))
  }

  const endDrag = (event: ReactPointerEvent<SVGSVGElement>) => {
    dragRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
  }

  const zoom = (factor: number) => {
    setMoved((current) => zoomAt(current ?? fitted, factor, size.width / 2, size.height / 2))
  }

  return (
    <div className="kk-tree-stage" ref={stageRef}>
      <svg
        className="kk-tree-stage__svg"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        // A drag that ends over a person must not also follow that person's
        // link: panning the stage is one gesture, not a gesture and a click.
        onClickCapture={(event) => {
          if (draggedRef.current) {
            event.preventDefault()
            event.stopPropagation()
            draggedRef.current = false
          }
        }}
      >
        <defs>
          <clipPath id={AVATAR_CLIP_ID}>
            <circle cx={AVATAR_CIRCLE.cx} cy={AVATAR_CIRCLE.cy} r={AVATAR_CIRCLE.r} />
          </clipPath>
        </defs>
        <g transform={`translate(${view.x}, ${view.y}) scale(${view.scale})`}>{children}</g>
      </svg>
      <div className="kk-tree-stage__controls">
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.zoomIn')}
          title={t('familyTree.zoomIn')}
          onClick={() => {
            zoom(ZOOM_STEP)
          }}
        >
          <Icon name="zoom-in" />
        </Button>
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.zoomOut')}
          title={t('familyTree.zoomOut')}
          onClick={() => {
            zoom(1 / ZOOM_STEP)
          }}
        >
          <Icon name="zoom-out" />
        </Button>
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.fit')}
          title={t('familyTree.fit')}
          onClick={() => {
            setMoved(null)
          }}
        >
          <Icon name="arrows-angle-contract" />
        </Button>
        {focus != null && (
          <Button
            variant="secondary"
            size="sm"
            aria-label={focusLabel ?? t('familyTree.centreRoot')}
            title={focusLabel ?? t('familyTree.centreRoot')}
            onClick={() => {
              setMoved((current) => centreOn(current ?? fitted, focus, size))
            }}
          >
            <Icon name="crosshair" />
          </Button>
        )}
      </div>
    </div>
  )
}
