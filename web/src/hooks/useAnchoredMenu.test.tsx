import { render, screen } from '@testing-library/react'
import { type CSSProperties, useRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAnchoredMenu } from './useAnchoredMenu'

/**
 * The desktop menu is a `position: fixed` box measured off the anchor's viewport
 * rect, which only lands where it was measured while nothing between it and the
 * viewport is a containing block for fixed positioning. The photo viewer's info
 * drawer is one — it slides on `transform` — and it put the album/label
 * suggestion list ~1000px off the right edge of a 1440px screen.
 *
 * jsdom lays nothing out, so every box here is stubbed: the tests assert on the
 * coordinates the hook computes, which is exactly the arithmetic that broke.
 */

/** One stubbed box, keyed by the element's `data-testid`. */
interface Box {
  top: number
  left: number
  width: number
  height: number
}

const boxes = new Map<string, Box>()

/** Answers `getBoundingClientRect` for any element carrying a stubbed box. */
function stubBoxes(): void {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ): DOMRect {
    const box = boxes.get(this.dataset.testid ?? '') ?? {
      top: 0,
      left: 0,
      width: 0,
      height: 0,
    }
    return {
      x: box.left,
      y: box.top,
      top: box.top,
      left: box.left,
      width: box.width,
      height: box.height,
      right: box.left + box.width,
      bottom: box.top + box.height,
      toJSON: () => box,
    }
  })
}

/** Reports the viewport as phone-width, so the hook takes its in-flow branch. */
function mockNarrow(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** An anchor inside a wrapper whose style the test chooses. */
function Harness({ wrapperStyle }: { wrapperStyle?: CSSProperties }) {
  const anchor = useRef<HTMLInputElement>(null)
  const menu = useAnchoredMenu(anchor, true)
  return (
    <div data-testid="wrapper" style={wrapperStyle}>
      <input data-testid="anchor" ref={anchor} />
      <ul data-testid="menu" className={menu.className} style={menu.style} />
    </div>
  )
}

describe('useAnchoredMenu', () => {
  beforeEach(() => {
    boxes.clear()
    // A 1440x900 desktop with the field near the bottom right, as in the viewer.
    window.innerHeight = 900
    window.innerWidth = 1440
    boxes.set('anchor', { top: 662, left: 1041, width: 368, height: 38 })
    stubBoxes()
  })

  it('hangs the menu under the anchor when no ancestor is a containing block', () => {
    render(<Harness />)
    const menu = screen.getByTestId('menu')
    expect(menu.style.position).toBe('fixed')
    expect(menu.style.left).toBe('1041px')
    expect(menu.style.top).toBe('704px')
    expect(menu.style.width).toBe('368px')
  })

  it('offsets the menu by a transformed ancestor, which owns fixed descendants', () => {
    // The viewer's info drawer: `transform: translateX(0)` while open, which makes
    // it — not the viewport — the origin every `top`/`left` below is read against.
    boxes.set('wrapper', { top: 56, left: 1025, width: 400, height: 844 })
    render(<Harness wrapperStyle={{ transform: 'translateX(0)' }} />)
    const menu = screen.getByTestId('menu')
    // Resolved against the drawer, these land the menu back on the input.
    expect(menu.style.left).toBe('16px')
    expect(menu.style.top).toBe('648px')
  })

  it('leaves an untransformed ancestor alone', () => {
    boxes.set('wrapper', { top: 56, left: 1025, width: 400, height: 844 })
    render(<Harness wrapperStyle={{ transform: 'none' }} />)
    expect(screen.getByTestId('menu').style.left).toBe('1041px')
  })

  it('measures from the containing block padding box, not its border box', () => {
    // The drawer carries `border-left: 1px`, and a fixed child is placed against
    // the padding box — without this the menu sits a pixel off.
    boxes.set('wrapper', { top: 56, left: 1025, width: 400, height: 844 })
    render(
      <Harness
        wrapperStyle={{ transform: 'translateX(0)', borderLeftWidth: '1px', borderTopWidth: '2px' }}
      />,
    )
    const menu = screen.getByTestId('menu')
    expect(menu.style.left).toBe('15px')
    expect(menu.style.top).toBe('646px')
  })

  it('caps the menu at the room below the field, measured in the viewport', () => {
    boxes.set('wrapper', { top: 56, left: 1025, width: 400, height: 844 })
    render(<Harness wrapperStyle={{ transform: 'translateX(0)' }} />)
    // 900 - 700 - 4 - 8 = 188: a length, so the containing block does not enter it.
    expect(screen.getByTestId('menu').style.maxHeight).toBe('188px')
  })

  it('keeps the phone in-flow branch free of coordinates', () => {
    mockNarrow(true)
    boxes.set('wrapper', { top: 56, left: 1025, width: 400, height: 844 })
    render(<Harness wrapperStyle={{ transform: 'translateX(0)' }} />)
    const menu = screen.getByTestId('menu')
    expect(menu.className).toContain('position-static')
    expect(menu.style.position).toBe('')
    expect(menu.style.left).toBe('')
    expect(menu.style.maxHeight).toBe('50vh')
  })
})
