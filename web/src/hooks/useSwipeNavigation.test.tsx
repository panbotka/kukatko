import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { type SwipeDirection } from '../lib/gestures'
import { useSwipeNavigation } from './useSwipeNavigation'

/** A point in the shape a TouchEvent's touch list carries. */
function pt(x: number, y: number): { clientX: number; clientY: number } {
  return { clientX: x, clientY: y }
}

/**
 * Renders the swipe surface with a marked image (a valid swipe target) and an
 * unmarked "face box" button (which must suppress the swipe).
 */
function Harness({
  onSwipe,
  enabled,
}: {
  onSwipe: (direction: SwipeDirection) => void
  enabled?: boolean
}) {
  const swipe = useSwipeNavigation({ onSwipe, enabled })
  return (
    <div
      data-testid="surface"
      onTouchStart={swipe.onTouchStart}
      onTouchMove={swipe.onTouchMove}
      onTouchEnd={swipe.onTouchEnd}
    >
      <button type="button" data-testid="image" data-swipe-surface="">
        image
      </button>
      <button type="button" data-testid="facebox">
        face
      </button>
    </div>
  )
}

/** Fires a start→end touch on `el` with the given single-finger travel. */
function swipeOn(
  el: Element,
  from: { clientX: number; clientY: number },
  to: { clientX: number; clientY: number },
): void {
  fireEvent.touchStart(el, { touches: [from], changedTouches: [from] })
  fireEvent.touchEnd(el, { touches: [], changedTouches: [to] })
}

describe('useSwipeNavigation', () => {
  it('pages next on a leftward horizontal swipe over the image', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    swipeOn(getByTestId('image'), pt(240, 100), pt(120, 108))
    expect(onSwipe).toHaveBeenCalledTimes(1)
    expect(onSwipe).toHaveBeenCalledWith('next')
  })

  it('pages prev on a rightward horizontal swipe over the image', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    swipeOn(getByTestId('image'), pt(120, 100), pt(240, 92))
    expect(onSwipe).toHaveBeenCalledTimes(1)
    expect(onSwipe).toHaveBeenCalledWith('prev')
  })

  it('does not navigate on a mostly-vertical drag (page scroll)', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    swipeOn(getByTestId('image'), pt(120, 80), pt(150, 320))
    expect(onSwipe).not.toHaveBeenCalled()
  })

  it('does not navigate on a short drag below the threshold', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    swipeOn(getByTestId('image'), pt(120, 100), pt(140, 100))
    expect(onSwipe).not.toHaveBeenCalled()
  })

  it('ignores a swipe that starts on an interactive overlay (a face box)', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    swipeOn(getByTestId('facebox'), pt(240, 100), pt(120, 100))
    expect(onSwipe).not.toHaveBeenCalled()
  })

  it('abandons the gesture when a second finger joins (a pinch, not a swipe)', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} />)
    const image = getByTestId('image')
    fireEvent.touchStart(image, { touches: [pt(240, 100)], changedTouches: [pt(240, 100)] })
    fireEvent.touchMove(image, {
      touches: [pt(240, 100), pt(160, 100)],
      changedTouches: [pt(160, 100)],
    })
    fireEvent.touchEnd(image, { touches: [], changedTouches: [pt(120, 100)] })
    expect(onSwipe).not.toHaveBeenCalled()
  })

  it('is inert when disabled', () => {
    const onSwipe = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={onSwipe} enabled={false} />)
    swipeOn(getByTestId('image'), pt(240, 100), pt(120, 100))
    expect(onSwipe).not.toHaveBeenCalled()
  })
})
