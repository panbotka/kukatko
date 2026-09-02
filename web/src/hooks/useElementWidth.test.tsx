import { render, screen } from '@testing-library/react'
import { useRef } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { FALLBACK_WIDTH_PX, useElementWidth } from './useElementWidth'

/** A probe that renders the width its own box reports. */
function Probe() {
  const ref = useRef<HTMLDivElement>(null)
  const width = useElementWidth(ref)
  return (
    <div ref={ref} data-testid="box">
      {width}
    </div>
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useElementWidth', () => {
  it('falls back to a usable width where nothing can be measured', () => {
    // jsdom lays nothing out, so every box measures 0 — exactly the case the
    // fallback exists for.
    render(<Probe />)
    expect(screen.getByTestId('box')).toHaveTextContent(String(FALLBACK_WIDTH_PX))
  })

  it('reports the measured width and watches the element for changes', () => {
    const observe = vi.fn()
    const disconnect = vi.fn()
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = observe
        disconnect = disconnect
      },
    )
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 640,
      height: 0,
      top: 0,
      left: 0,
      bottom: 0,
      right: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    const { unmount } = render(<Probe />)

    expect(screen.getByTestId('box')).toHaveTextContent('640')
    expect(observe).toHaveBeenCalledTimes(1)
    unmount()
    expect(disconnect).toHaveBeenCalledTimes(1)
  })
})
