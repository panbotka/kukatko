import { useEffect, useRef, useState } from 'react'

import { type Box, currentRenditionDpr } from '../lib/rendition'

/** The viewport as a box plus the screen's capped device-pixel ratio. */
export interface ViewportBox extends Box {
  /** The screen's device-pixel ratio, capped for rendition picking. */
  dpr: number
}

/**
 * A sensible box for a browser-less environment (a unit test rendering into
 * jsdom before it has laid anything out). It is deliberately desktop-sized, so a
 * component that never sees a real viewport asks for the rendition it would have
 * asked for before it measured anything.
 */
const FALLBACK: ViewportBox = { width: 1280, height: 800, dpr: 1 }

/** Reads the viewport now, falling back outside a browser. */
function readViewport(): ViewportBox {
  if (typeof window === 'undefined') return FALLBACK
  const width = window.innerWidth
  const height = window.innerHeight
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return FALLBACK
  }
  return { width, height, dpr: currentRenditionDpr() }
}

/**
 * The viewport's size, kept up to date across resizes and rotation, for
 * components that choose an image rendition from the box they draw into.
 *
 * It is deliberately *monotonic*: the box only ever grows over the hook's life.
 * Shrinking it would step the chosen rendition back down, and a smaller
 * rendition is a different URL — so narrowing a window would make the browser
 * download the same photograph a second time, smaller, to replace one it already
 * has. Growing is the only direction worth paying for, and it is the one where
 * the image would otherwise go soft.
 */
export function useViewportBox(): ViewportBox {
  const [box, setBox] = useState<ViewportBox>(readViewport)
  const seen = useRef(box)

  useEffect(() => {
    const update = () => {
      const now = readViewport()
      const grown: ViewportBox = {
        width: Math.max(seen.current.width, now.width),
        height: Math.max(seen.current.height, now.height),
        dpr: Math.max(seen.current.dpr, now.dpr),
      }
      if (
        grown.width === seen.current.width &&
        grown.height === seen.current.height &&
        grown.dpr === seen.current.dpr
      ) {
        return
      }
      seen.current = grown
      setBox(grown)
    }
    window.addEventListener('resize', update)
    window.addEventListener('orientationchange', update)
    // Re-read on subscribe: the viewport may have changed between the first
    // render and this effect.
    update()
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('orientationchange', update)
    }
  }, [])

  return box
}
