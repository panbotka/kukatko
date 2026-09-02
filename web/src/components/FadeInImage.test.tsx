import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { clearBlurPlaceholderCache } from '../lib/blurPlaceholder'
import { STUB_CANVAS_DATA_URL, stubBlurCanvas } from '../test/canvas'

import { FadeInImage } from './FadeInImage'

/** A real BlurHash of a real photograph (the woltapp reference string). */
const HASH = 'LEHV6nWB2yk8pyo0adR*.7kCMdnj'

describe('FadeInImage', () => {
  beforeEach(() => {
    clearBlurPlaceholderCache()
  })

  it('renders the image with the load-in class and lazy async defaults', () => {
    render(<FadeInImage src="/a.jpg" alt="A" />)

    const img = screen.getByRole('img', { name: 'A' })
    expect(img).toHaveClass('kk-media-img')
    expect(img).toHaveAttribute('loading', 'lazy')
    expect(img).toHaveAttribute('decoding', 'async')
  })

  it('starts hidden and reveals the image once it decodes', () => {
    render(<FadeInImage src="/a.jpg" alt="A" />)

    const img = screen.getByRole('img', { name: 'A' })
    // jsdom leaves the image with no decoded pixels, so it starts un-loaded.
    expect(img).not.toHaveClass('is-loaded')

    fireEvent.load(img)
    expect(img).toHaveClass('is-loaded')
  })

  it('merges the caller className and forwards onError', () => {
    const onError = vi.fn()
    render(<FadeInImage src="/a.jpg" alt="A" className="w-100 h-100" onError={onError} />)

    const img = screen.getByRole('img', { name: 'A' })
    expect(img).toHaveClass('kk-media-img', 'w-100', 'h-100')

    fireEvent.error(img)
    expect(onError).toHaveBeenCalledTimes(1)
  })

  it('lets the caller override the loading strategy', () => {
    render(<FadeInImage src="/a.jpg" alt="A" loading="eager" />)

    expect(screen.getByRole('img', { name: 'A' })).toHaveAttribute('loading', 'eager')
  })

  it("paints the photo's blurred stand-in behind the image while it loads", () => {
    stubBlurCanvas()

    render(<FadeInImage src="/a.jpg" alt="A" blurhash={HASH} />)

    const blur = document.querySelector('.kk-media-blur')
    expect(blur).toHaveStyle({ backgroundImage: `url("${STUB_CANVAS_DATA_URL}")` })
    // The image fades in over it, so the blur stays put once the image lands —
    // removing it would flash the empty box through the fade.
    fireEvent.load(screen.getByRole('img', { name: 'A' }))
    expect(document.querySelector('.kk-media-blur')).toBeInTheDocument()
  })

  it('leaves the neutral box showing for a photo with no hash', () => {
    stubBlurCanvas()

    render(<FadeInImage src="/a.jpg" alt="A" />)

    expect(document.querySelector('.kk-media-blur')).not.toBeInTheDocument()
  })

  it('shows the shimmer only where there is no blur to paint', () => {
    stubBlurCanvas()

    const { rerender } = render(<FadeInImage src="/a.jpg" alt="A" skeleton />)
    expect(document.querySelector('.kk-skeleton')).toBeInTheDocument()

    // A photograph's own colours say more than a shimmer, so the hash wins.
    rerender(<FadeInImage src="/a.jpg" alt="A" skeleton blurhash={HASH} />)
    expect(document.querySelector('.kk-media-blur')).toBeInTheDocument()
    expect(document.querySelector('.kk-skeleton')).not.toBeInTheDocument()
  })
})
