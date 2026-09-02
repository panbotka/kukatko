import { act, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useViewportBox } from './useViewportBox'

/** A probe that renders the viewport box the hook reports. */
function Probe() {
  const { width, height, dpr } = useViewportBox()
  return <div data-testid="box">{`${String(width)}x${String(height)}@${String(dpr)}`}</div>
}

/** Resizes the jsdom window and fires the event the hook listens for. */
function resizeTo(width: number, height: number): void {
  act(() => {
    window.innerWidth = width
    window.innerHeight = height
    window.dispatchEvent(new Event('resize'))
  })
}

describe('useViewportBox', () => {
  it('reports the current viewport', () => {
    resizeTo(1024, 768)
    render(<Probe />)
    expect(screen.getByTestId('box')).toHaveTextContent('1024x768@1')
  })

  it('grows with the viewport', () => {
    resizeTo(500, 400)
    render(<Probe />)
    resizeTo(1600, 900)
    expect(screen.getByTestId('box')).toHaveTextContent('1600x900@1')
  })

  it('never shrinks, so a narrowed window re-fetches no smaller image', () => {
    resizeTo(1600, 900)
    render(<Probe />)
    resizeTo(400, 300)
    expect(screen.getByTestId('box')).toHaveTextContent('1600x900@1')
  })

  it('caps the device pixel ratio it reports', () => {
    resizeTo(800, 600)
    window.devicePixelRatio = 3
    render(<Probe />)
    expect(screen.getByTestId('box')).toHaveTextContent('800x600@2')
    window.devicePixelRatio = 1
  })
})
