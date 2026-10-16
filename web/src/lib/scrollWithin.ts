/**
 * Where a revealed element should land in its scroller: in the middle, or
 * moved just far enough to be fully in view (nothing moves if it already is).
 * The same words `scrollIntoView` uses for its `block` option.
 */
export type RevealBlock = 'center' | 'nearest'

/**
 * The element that scrolls the page itself. `scrollingElement` is the standard
 * answer; `<html>` stands in where it is missing (jsdom, very old engines).
 */
function pageScroller(): Element {
  return document.scrollingElement ?? document.documentElement
}

/** An overflow value that lets an element scroll for the reader. */
function scrollsForTheReader(overflowY: string): boolean {
  return overflowY === 'auto' || overflowY === 'scroll' || overflowY === 'overlay'
}

/**
 * The one container to scroll so `element` comes into view: the nearest
 * ancestor that both scrolls for the reader (`overflow-y: auto|scroll`) and
 * actually has more content than room. A clipping ancestor (`overflow: hidden`)
 * is never chosen — a layer that hides its overflow is not meant to move.
 *
 * The walk stops at a `position: fixed` ancestor: a fixed layer (the photo
 * viewer) does not move when the page scrolls, so nothing beyond it can bring
 * its content into view, and scrolling the page under it would only disturb
 * whatever the viewer was opened over. With no fixed layer and no scroller in
 * between, the page's own scrolling element is the answer. Null = nothing to
 * scroll.
 */
export function scrollContainerOf(element: Element): Element | null {
  for (let node = element.parentElement; node !== null; node = node.parentElement) {
    if (node === document.body || node === document.documentElement) {
      break
    }
    const style = window.getComputedStyle(node)
    if (scrollsForTheReader(style.overflowY) && node.scrollHeight > node.clientHeight) {
      return node
    }
    if (style.position === 'fixed') {
      return null
    }
  }
  return pageScroller()
}

/**
 * The `scrollTop` that shows a target spanning `top`..`top + height` (measured
 * from the start of the scroller's content) in a viewport `viewport` tall that
 * is currently scrolled to `current`. Pure, so the arithmetic is tested without
 * layout; the browser clamps the result to the content it has.
 */
export function revealScrollTop(
  block: RevealBlock,
  target: { top: number; height: number },
  viewport: number,
  current: number,
): number {
  if (block === 'center') {
    return Math.max(0, Math.round(target.top - (viewport - target.height) / 2))
  }
  if (target.top < current) {
    return Math.max(0, Math.round(target.top))
  }
  const bottom = target.top + target.height
  if (bottom > current + viewport) {
    // A target taller than the viewport shows its start rather than its end.
    return Math.round(Math.min(target.top, bottom - viewport))
  }
  return current
}

/**
 * Brings `element` into view by scrolling exactly one container — the one
 * {@link scrollContainerOf} names — and nothing else.
 *
 * `element.scrollIntoView()` is the obvious call and the wrong one inside the
 * photo viewer: it scrolls EVERY scrollable ancestor, `overflow: hidden` ones
 * included, so revealing a comment in the info drawer also scrolled the
 * fixed full-screen viewer itself and pushed its toolbar off the top of the
 * screen. Setting the one scroller's `scrollTop` leaves every other ancestor
 * where it was.
 */
export function revealWithin(element: Element, block: RevealBlock): void {
  const container = scrollContainerOf(element)
  if (container === null) {
    return
  }
  // The scroller's visible area starts inside its border, not at its outer edge.
  const page = container === pageScroller()
  const viewportTop = page ? 0 : container.getBoundingClientRect().top + container.clientTop
  const viewport = page ? window.innerHeight : container.clientHeight
  const rect = element.getBoundingClientRect()
  const current = container.scrollTop
  const top = rect.top - viewportTop + current
  const next = revealScrollTop(block, { top, height: rect.height }, viewport, current)
  if (next !== current) {
    container.scrollTop = next
  }
}
