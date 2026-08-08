import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { loadImageAs } from '../test/imageFrame'

import { type ImageFrameOptions, useImageFrame } from './useImageFrame'

/**
 * A stand-in for the real callers: a wrapper sized from the frame with the image
 * it measures inside it. The frame's state is mirrored onto data attributes so a
 * test can read it without reaching into the hook.
 */
function Probe({ source, width, height, orientation }: ImageFrameOptions) {
  const frame = useImageFrame({ source, width, height, orientation })
  return (
    <div
      data-testid="wrap"
      data-measured={String(frame.measured)}
      data-ratio={frame.ratio === undefined ? 'none' : frame.ratio.toFixed(4)}
      style={{ aspectRatio: frame.aspectRatio }}
    >
      <img {...frame.imgProps} src={`/thumb/${source}`} alt="probe" />
    </div>
  )
}

/** The probe's wrapper — the element a face box would be positioned against. */
function wrap(): HTMLElement {
  return screen.getByTestId('wrap')
}

/** The probe's image, the one the frame is measured from. */
function preview(): HTMLImageElement {
  return screen.getByAltText<HTMLImageElement>('probe')
}

/**
 * Makes every image in the test behave like one served from cache: already
 * `complete`, with its natural size known, before React can attach an `onLoad`.
 * jsdom fetches nothing, so all three have to be faked together — `complete` is
 * what the hook's ref demands before it trusts a size it was not told about by a
 * load event. Returns the undo; leaving the accessors replaced would make every
 * later test see a loaded image.
 */
function withCachedImages(width: number, height: number): () => void {
  const proto = HTMLImageElement.prototype
  const saved = new Map<string, PropertyDescriptor | undefined>()
  const fake: Record<string, unknown> = {
    complete: true,
    naturalWidth: width,
    naturalHeight: height,
  }
  for (const [name, value] of Object.entries(fake)) {
    saved.set(name, Object.getOwnPropertyDescriptor(proto, name))
    Object.defineProperty(proto, name, { configurable: true, value })
  }
  return () => {
    for (const [name, descriptor] of saved) {
      if (descriptor === undefined) {
        // jsdom never had it; leaving a fake behind would outlive this test.
        Reflect.deleteProperty(proto, name)
        continue
      }
      Object.defineProperty(proto, name, descriptor)
    }
  }
}

describe('useImageFrame', () => {
  it('holds the row’s frame as the estimate and hands over to the loaded image', () => {
    render(<Probe source="p1" width={4000} height={3000} orientation={1} />)

    // The estimate: enough to hold the layout still, not enough to place a box on.
    expect(wrap()).toHaveAttribute('data-measured', 'false')
    expect(wrap()).toHaveStyle({ aspectRatio: '4000 / 3000' })

    loadImageAs(preview(), 1440, 1920)
    expect(wrap()).toHaveAttribute('data-measured', 'true')
    expect(wrap()).toHaveStyle({ aspectRatio: '1440 / 1920' })
    expect(wrap()).toHaveAttribute('data-ratio', (1440 / 1920).toFixed(4))
  })

  it('applies the EXIF quarter turn to the estimate, as the row is pre-rotation', () => {
    // Orientations 5–8 swap the sides; the loaded image needs no such correction,
    // its natural size is already post-orientation.
    render(<Probe source="p1" width={4000} height={3000} orientation={6} />)

    expect(wrap()).toHaveStyle({ aspectRatio: '3000 / 4000' })
  })

  it('discards the measurement when the photo changes', () => {
    const { rerender } = render(<Probe source="p1" width={4000} height={3000} orientation={1} />)
    loadImageAs(preview(), 1440, 1920)
    expect(wrap()).toHaveAttribute('data-measured', 'true')

    // The next photo is portrait per its row. Were the previous measurement kept,
    // the new photo would be framed by the old one's shape — and the element does
    // keep reporting the old image's natural size until the new `src` arrives, so
    // this is the case that makes the hook's `complete` check load-bearing.
    rerender(<Probe source="p2" width={3000} height={4000} orientation={1} />)
    expect(wrap()).toHaveAttribute('data-measured', 'false')
    expect(wrap()).toHaveStyle({ aspectRatio: '3000 / 4000' })

    loadImageAs(preview(), 900, 1200)
    expect(wrap()).toHaveStyle({ aspectRatio: '900 / 1200' })
  })

  it('keeps the estimate when a broken image reports no dimensions', () => {
    render(<Probe source="p1" width={4000} height={3000} orientation={1} />)

    loadImageAs(preview(), 0, 0)
    expect(wrap()).toHaveAttribute('data-measured', 'false')
    expect(wrap()).toHaveStyle({ aspectRatio: '4000 / 3000' })
  })

  it('reports no frame at all when neither the row nor the image gives one', () => {
    // The caller then falls back to a shape of its own choosing — and, with no
    // frame, has nothing to position a box against anyway.
    render(<Probe source="p1" width={0} height={0} orientation={1} />)

    expect(wrap()).toHaveAttribute('data-ratio', 'none')
    expect(wrap().style.aspectRatio).toBe('')
  })

  it('measures a cached image that was complete before the handler existed', () => {
    // No load event is fired here at all: the ref catches it, which is the whole
    // reason the hook takes a ref as well as an onLoad.
    const restore = withCachedImages(1000, 500)
    try {
      render(<Probe source="p1" width={3000} height={4000} orientation={1} />)

      expect(wrap()).toHaveAttribute('data-measured', 'true')
      expect(wrap()).toHaveStyle({ aspectRatio: '1000 / 500' })
    } finally {
      restore()
    }
  })
})
