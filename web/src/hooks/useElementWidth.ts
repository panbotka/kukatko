import { type RefObject, useEffect, useState } from 'react'

/**
 * The width to assume before an element has been measured, or where it cannot be
 * measured at all — a test environment with no layout, chiefly. The viewport is
 * the honest guess for a grid that spans the page column, and it keeps a
 * width-driven layout producing something rather than nothing.
 */
export const FALLBACK_WIDTH_PX = 1024

/**
 * The live content width of an element, in CSS pixels.
 *
 * The justified photo wall is laid out in JavaScript, so it needs a real number
 * for the width it fills — not a percentage, and not the viewport (the page
 * column is narrower, and the timeline rail takes a lane out of it). The value
 * follows the element: a window resize, a phone rotating, the filter row
 * re-wrapping and the sidebar opening all move it.
 *
 * Measurement is by `ResizeObserver` where there is one, with a one-off
 * measurement on mount so the first paint is already laid out. jsdom has neither
 * a layout nor a `ResizeObserver`, so an unmeasurable element reports
 * {@link FALLBACK_WIDTH_PX} — a component that renders nothing until it has been
 * measured would otherwise render nothing at all in every test.
 */
export function useElementWidth(ref: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const element = ref.current
    if (element === null) {
      return
    }
    const measure = () => {
      // `getBoundingClientRect` is the width actually painted (fractional, and
      // after any transform), which is what the row arithmetic has to add up to.
      const next = element.getBoundingClientRect().width
      setWidth((current) => (Math.abs(current - next) < 0.5 ? current : next))
    }
    measure()
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null
    observer?.observe(element)
    // Without a ResizeObserver the window's own resize is the only signal there
    // is; it misses a layout change that leaves the window alone, but it is
    // strictly better than a width frozen at mount.
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

  return width > 0 ? width : FALLBACK_WIDTH_PX
}
