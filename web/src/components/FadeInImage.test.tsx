import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FadeInImage } from './FadeInImage'

describe('FadeInImage', () => {
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
})
