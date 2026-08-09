import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { useKeyboardInset } from './useKeyboardInset'

/** A minimal stand-in for the visual viewport, with the listeners tests fire. */
class FakeVisualViewport extends EventTarget {
  height: number
  offsetTop = 0
  scale = 1

  constructor(height: number) {
    super()
    this.height = height
  }

  /** Applies a new geometry and notifies listeners, as the browser would. */
  set(next: { height?: number; offsetTop?: number; scale?: number }): void {
    Object.assign(this, next)
    this.dispatchEvent(new Event('resize'))
  }
}

function install(height: number): FakeVisualViewport {
  const vv = new FakeVisualViewport(height)
  Object.defineProperty(window, 'visualViewport', {
    writable: true,
    configurable: true,
    value: vv,
  })
  Object.defineProperty(window, 'innerHeight', { writable: true, configurable: true, value: 800 })
  return vv
}

function Probe() {
  return <output>{useKeyboardInset()}</output>
}

afterEach(() => {
  Object.defineProperty(window, 'visualViewport', {
    writable: true,
    configurable: true,
    value: null,
  })
})

describe('useKeyboardInset', () => {
  it('is zero while no keyboard is up', () => {
    install(800)
    render(<Probe />)

    expect(screen.getByRole('status')).toHaveTextContent('0')
  })

  it('reports the keyboard height once it covers the bottom of the window', () => {
    const vv = install(800)
    render(<Probe />)

    act(() => {
      vv.set({ height: 480 })
    })

    expect(screen.getByRole('status')).toHaveTextContent('320')
  })

  it('ignores a difference too small to be a keyboard', () => {
    const vv = install(800)
    render(<Probe />)

    // A collapsing browser toolbar, not a keyboard.
    act(() => {
      vv.set({ height: 760 })
    })

    expect(screen.getByRole('status')).toHaveTextContent('0')
  })

  it('stands down while the reader is pinch-zoomed into the photo', () => {
    const vv = install(800)
    render(<Probe />)

    act(() => {
      vv.set({ height: 400, scale: 2 })
    })

    expect(screen.getByRole('status')).toHaveTextContent('0')
  })

  it('counts the viewport offset iOS uses to report the keyboard', () => {
    const vv = install(800)
    render(<Probe />)

    act(() => {
      vv.set({ height: 500, offsetTop: 100 })
    })

    expect(screen.getByRole('status')).toHaveTextContent('200')
  })

  it('is inert in a browser without a visual viewport', () => {
    Object.defineProperty(window, 'visualViewport', {
      writable: true,
      configurable: true,
      value: null,
    })
    render(<Probe />)

    expect(screen.getByRole('status')).toHaveTextContent('0')
  })
})
