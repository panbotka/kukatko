import { afterEach, describe, expect, it } from 'vitest'

import { revealScrollTop, revealWithin, scrollContainerOf } from './scrollWithin'

/** jsdom has no layout: give an element the box a browser would have measured. */
function measure(
  element: HTMLElement,
  box: { top?: number; height?: number; scrollHeight?: number; clientHeight?: number },
): void {
  const { top = 0, height = 0, scrollHeight = height, clientHeight = height } = box
  element.getBoundingClientRect = () => new DOMRect(0, top, 100, height)
  Object.defineProperty(element, 'scrollHeight', { configurable: true, value: scrollHeight })
  Object.defineProperty(element, 'clientHeight', { configurable: true, value: clientHeight })
}

/**
 * The photo viewer in miniature: a fixed, clipping root holding a drawer whose
 * body is the scroller, with the target row inside it.
 */
function viewer(): { root: HTMLElement; body: HTMLElement; target: HTMLElement } {
  const root = document.createElement('div')
  root.style.position = 'fixed'
  root.style.overflow = 'hidden'
  const body = document.createElement('div')
  body.style.overflowY = 'auto'
  const target = document.createElement('div')
  body.append(target)
  root.append(body)
  document.body.append(root)
  return { root, body, target }
}

afterEach(() => {
  document.body.replaceChildren()
})

describe('revealScrollTop', () => {
  it('centres the target in the viewport', () => {
    expect(revealScrollTop('center', { top: 1000, height: 100 }, 400, 0)).toBe(850)
  })

  it('never asks for a negative scroll when centring near the start', () => {
    expect(revealScrollTop('center', { top: 50, height: 20 }, 400, 300)).toBe(0)
  })

  it('leaves a fully visible target where it is for "nearest"', () => {
    expect(revealScrollTop('nearest', { top: 120, height: 40 }, 400, 100)).toBe(100)
  })

  it('scrolls just far enough to show a target below the fold', () => {
    expect(revealScrollTop('nearest', { top: 500, height: 40 }, 400, 100)).toBe(140)
  })

  it('scrolls back up to a target above the viewport', () => {
    expect(revealScrollTop('nearest', { top: 30, height: 40 }, 400, 100)).toBe(30)
  })

  it('shows the start of a target taller than the viewport', () => {
    expect(revealScrollTop('nearest', { top: 600, height: 900 }, 400, 0)).toBe(600)
  })
})

describe('scrollContainerOf', () => {
  it('picks the overflowing scroller, never the clipping fixed root around it', () => {
    const { body, target } = viewer()
    measure(body, { height: 400, scrollHeight: 2000 })
    expect(scrollContainerOf(target)).toBe(body)
  })

  it('stops at a fixed layer instead of falling through to the page', () => {
    const { body, target } = viewer()
    // The thread fits: the drawer body has nothing to scroll.
    measure(body, { height: 400, scrollHeight: 400 })
    expect(scrollContainerOf(target)).toBeNull()
  })

  it('falls back to the page when the element sits in its normal flow', () => {
    const target = document.createElement('div')
    document.body.append(target)
    expect(scrollContainerOf(target)).toBe(document.documentElement)
  })
})

describe('revealWithin', () => {
  it('scrolls only the drawer body, leaving the viewer root at zero', () => {
    const { root, body, target } = viewer()
    measure(root, { top: 0, height: 900 })
    measure(body, { top: 100, height: 400, scrollHeight: 3000 })
    measure(target, { top: 1600, height: 80 })

    revealWithin(target, 'center')

    // 1600 - 100 = 1500 into the content; centred in 400 → 1500 - 160.
    expect(body.scrollTop).toBe(1340)
    expect(root.scrollTop).toBe(0)
    expect(document.documentElement.scrollTop).toBe(0)
  })

  it('moves nothing for "nearest" when the target is already in view', () => {
    const { body, target } = viewer()
    measure(body, { top: 100, height: 400, scrollHeight: 3000 })
    measure(target, { top: 200, height: 80 })

    revealWithin(target, 'nearest')

    expect(body.scrollTop).toBe(0)
  })
})
