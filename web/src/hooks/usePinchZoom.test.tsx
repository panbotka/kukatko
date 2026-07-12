import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { type SwipeDirection } from '../lib/gestures'
import { usePinchZoom } from './usePinchZoom'

/** A point in the shape a TouchEvent's touch list carries. */
function pt(x: number, y: number): { clientX: number; clientY: number } {
  return { clientX: x, clientY: y }
}

/** Renders the zoom surface and surfaces the zoom state as text for assertions. */
function Harness({
  onSwipe,
  resetKey,
}: {
  onSwipe: (direction: SwipeDirection) => void
  resetKey: string
}) {
  const zoom = usePinchZoom({ onSwipe, resetKey })
  return (
    <div
      data-testid="stage"
      onTouchStart={zoom.handlers.onTouchStart}
      onTouchMove={zoom.handlers.onTouchMove}
      onTouchEnd={zoom.handlers.onTouchEnd}
    >
      <span data-testid="zoomed">{String(zoom.isZoomed)}</span>
      <span data-testid="scale">{String(zoom.scale)}</span>
    </div>
  )
}

/** Fires a single stationary tap on `el` (a start→end at the same point). */
function tap(el: Element, at: { clientX: number; clientY: number } = pt(100, 100)): void {
  fireEvent.touchStart(el, { touches: [at], changedTouches: [at] })
  fireEvent.touchEnd(el, { touches: [], changedTouches: [at] })
}

describe('usePinchZoom', () => {
  it('double-tap toggles zoom in and back out', () => {
    const { getByTestId } = render(<Harness onSwipe={vi.fn()} resetKey="a" />)
    const stage = getByTestId('stage')
    expect(getByTestId('zoomed').textContent).toBe('false')

    // Two quick taps zoom in.
    tap(stage)
    tap(stage)
    expect(getByTestId('zoomed').textContent).toBe('true')

    // Two more taps zoom back out to fit.
    tap(stage)
    tap(stage)
    expect(getByTestId('zoomed').textContent).toBe('false')
  })

  it('resets the zoom when the shown photo (resetKey) changes', () => {
    const { getByTestId, rerender } = render(<Harness onSwipe={vi.fn()} resetKey="a" />)
    const stage = getByTestId('stage')
    tap(stage)
    tap(stage)
    expect(getByTestId('zoomed').textContent).toBe('true')

    rerender(<Harness onSwipe={vi.fn()} resetKey="b" />)
    expect(getByTestId('zoomed').textContent).toBe('false')
    expect(getByTestId('scale').textContent).toBe('1')
  })

  it('pages on a horizontal swipe while not zoomed', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} resetKey="a" />)
    const stage = getByTestId('stage')
    fireEvent.touchStart(stage, { touches: [pt(240, 100)], changedTouches: [pt(240, 100)] })
    fireEvent.touchEnd(stage, { touches: [], changedTouches: [pt(110, 104)] })
    expect(onSwipe).toHaveBeenCalledTimes(1)
    expect(onSwipe).toHaveBeenCalledWith('next')
  })

  it('does not page while zoomed — a drag pans instead of swiping', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} resetKey="a" />)
    const stage = getByTestId('stage')
    // Zoom in first.
    tap(stage)
    tap(stage)
    expect(getByTestId('zoomed').textContent).toBe('true')
    // A horizontal drag now pans; it must NOT page.
    fireEvent.touchStart(stage, { touches: [pt(240, 100)], changedTouches: [pt(240, 100)] })
    fireEvent.touchMove(stage, { touches: [pt(180, 100)], changedTouches: [pt(180, 100)] })
    fireEvent.touchEnd(stage, { touches: [], changedTouches: [pt(110, 100)] })
    expect(onSwipe).not.toHaveBeenCalled()
  })

  it('pinches the scale in proportion to the finger spread', () => {
    const { getByTestId } = render(<Harness onSwipe={vi.fn()} resetKey="a" />)
    const stage = getByTestId('stage')
    // Fingers 100px apart at start, 200px apart after the move → 2× zoom.
    fireEvent.touchStart(stage, {
      touches: [pt(0, 0), pt(100, 0)],
      changedTouches: [pt(0, 0)],
    })
    fireEvent.touchMove(stage, {
      touches: [pt(0, 0), pt(200, 0)],
      changedTouches: [pt(0, 0)],
    })
    fireEvent.touchEnd(stage, { touches: [], changedTouches: [pt(0, 0)] })
    expect(getByTestId('scale').textContent).toBe('2')
    expect(getByTestId('zoomed').textContent).toBe('true')
  })
})
