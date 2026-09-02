import { act, fireEvent, render, screen } from '@testing-library/react'
import { useRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LONG_PRESS_MS, LONG_PRESS_SLOP } from '../lib/longPressSelect'

import { supportsTouch, useLongPressSelect } from './useLongPressSelect'

const TILES = ['a', 'b', 'c']

/** A point in the shape a TouchEvent's touch list carries. */
function pt(x: number, y: number): { clientX: number; clientY: number } {
  return { clientX: x, clientY: y }
}

/** Pretends the device has a touch screen (jsdom reports none). */
function withTouchScreen(): void {
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 1, configurable: true })
}

/**
 * Stands in for the layout jsdom does not do: the tiles are laid out left to
 * right, 100 px apart, with anything past them counting as the gutter.
 */
function stubHitTest(): void {
  const at = (x: number, _y: number): Element | null => {
    const uid = TILES.at(Math.floor(x / 100))
    return uid === undefined ? null : screen.getByTestId(`img-${uid}`)
  }
  Object.defineProperty(document, 'elementFromPoint', {
    value: at,
    configurable: true,
    writable: true,
  })
}

/** A grid of three tiles wired to the gesture, each with an image inside it. */
function Harness({
  onSelect,
  enabled = true,
}: {
  onSelect: (uids: string[]) => void
  enabled?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const { dragging } = useLongPressSelect({ target: ref, enabled, onSelect })
  return (
    <div ref={ref} data-testid="grid" data-dragging={dragging ? 'true' : undefined}>
      {TILES.map((uid) => (
        <div key={uid} data-photo-uid={uid}>
          <img data-testid={`img-${uid}`} alt={uid} src={`/thumb/${uid}`} />
        </div>
      ))}
    </div>
  )
}

/** Presses a tile and lets the hold timer fire, engaging the gesture. */
function pressAndHold(uid: string, at = pt(50, 10)): void {
  fireEvent.touchStart(screen.getByTestId(`img-${uid}`), { touches: [at], changedTouches: [at] })
  act(() => {
    vi.advanceTimersByTime(LONG_PRESS_MS)
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  withTouchScreen()
  stubHitTest()
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(document, 'elementFromPoint')
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 0, configurable: true })
})

describe('supportsTouch', () => {
  it('detects the touch screen by feature, never by user agent', () => {
    expect(supportsTouch()).toBe(true)
    Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 0, configurable: true })
    expect(supportsTouch()).toBe(false)
  })
})

describe('useLongPressSelect', () => {
  it('selects the pressed photo once the hold timer fires', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    fireEvent.touchStart(screen.getByTestId('img-a'), { touches: [pt(50, 10)] })
    // Nothing yet: a shorter press is an ordinary tap and must stay one.
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS - 1)
    })
    expect(onSelect).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(onSelect).toHaveBeenCalledWith(['a'])
    expect(screen.getByTestId('grid')).toHaveAttribute('data-dragging', 'true')
  })

  it('leaves a plain tap alone', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    fireEvent.touchEnd(tile, { touches: [], changedTouches: [pt(50, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('lets a scroll through: movement past the threshold cancels the press for good', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    // Not prevented — the page has to go on scrolling under the finger.
    const scrolled = fireEvent.touchMove(tile, {
      touches: [pt(50, 10 + LONG_PRESS_SLOP + 5)],
    })
    expect(scrolled).toBe(true)

    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
    expect(screen.getByTestId('grid')).not.toHaveAttribute('data-dragging')
  })

  it('tolerates a jitter smaller than the threshold', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    fireEvent.touchMove(tile, { touches: [pt(50 + LONG_PRESS_SLOP - 1, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS)
    })
    expect(onSelect).toHaveBeenCalledWith(['a'])
  })

  it('adds every tile the engaged drag crosses, once each, and swallows the scroll', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)
    pressAndHold('a')

    const tile = screen.getByTestId('img-a')
    // Each move is hit-tested against the point under the finger, because the
    // touch itself keeps reporting the element the gesture started on.
    const prevented = fireEvent.touchMove(tile, { touches: [pt(150, 10)] })
    expect(prevented).toBe(false)
    fireEvent.touchMove(tile, { touches: [pt(160, 10)] })
    fireEvent.touchMove(tile, { touches: [pt(250, 10)] })
    // Back over a tile already in the batch: additive, so nothing changes.
    fireEvent.touchMove(tile, { touches: [pt(150, 10)] })
    // Over the gutter past the last tile: nothing to add.
    fireEvent.touchMove(tile, { touches: [pt(900, 10)] })

    expect(onSelect.mock.calls).toEqual([[['a']], [['b']], [['c']]])
  })

  it('suppresses the click the lift would fire after a drag', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)
    pressAndHold('a')

    const tile = screen.getByTestId('img-a')
    const lifted = fireEvent.touchEnd(tile, { touches: [], changedTouches: [pt(150, 10)] })
    expect(lifted).toBe(false)
    expect(screen.getByTestId('grid')).not.toHaveAttribute('data-dragging')

    // …but the lift ending a plain tap is left to fire its click as ever.
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    expect(fireEvent.touchEnd(tile, { touches: [], changedTouches: [pt(50, 10)] })).toBe(true)
  })

  it('suppresses the platform long-press menu while a finger is down, and only then', () => {
    render(<Harness onSelect={vi.fn()} />)
    const tile = screen.getByTestId('img-a')

    // A right-click with no touch in flight (the desktop) keeps its menu.
    expect(fireEvent.contextMenu(tile)).toBe(true)

    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    expect(fireEvent.contextMenu(tile)).toBe(false)
  })

  it('abandons the gesture when a second finger joins', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10), pt(80, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('drops an engaged drag when a second finger joins mid-stroke', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)
    pressAndHold('a')

    const tile = screen.getByTestId('img-a')
    fireEvent.touchMove(tile, { touches: [pt(150, 10), pt(220, 10)] })
    fireEvent.touchMove(tile, { touches: [pt(250, 10)] })
    expect(onSelect.mock.calls).toEqual([[['a']]])
  })

  it('ignores a press that starts off any tile', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    fireEvent.touchStart(screen.getByTestId('grid'), { touches: [pt(50, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('attaches nothing while disabled', () => {
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} enabled={false} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
    expect(fireEvent.contextMenu(tile)).toBe(true)
  })

  it('attaches nothing on a device with no touch screen', () => {
    Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 0, configurable: true })
    const onSelect = vi.fn()
    render(<Harness onSelect={onSelect} />)

    const tile = screen.getByTestId('img-a')
    fireEvent.touchStart(tile, { touches: [pt(50, 10)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(onSelect).not.toHaveBeenCalled()
  })
})
