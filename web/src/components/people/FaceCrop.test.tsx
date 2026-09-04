import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { type Bbox } from '../../services/people'

import { FaceCrop } from './FaceCrop'

/** A face box a third of the way into the frame, a fifth of it across. */
const BBOX: Bbox = [0.3125, 0.25, 0.2, 0.2]

describe('FaceCrop', () => {
  it('asks the backend for the face, not for the photograph', () => {
    // This is the regression the component exists for: cropping in CSS meant
    // fetching a whole-frame `fit_*` rendition to paint a 96px square.
    render(<FaceCrop photoUid="p1" bbox={BBOX} label="Anna" size={96} />)

    const img = screen.getByRole('img', { name: 'Anna' })
    const src = img.getAttribute('src') ?? ''
    expect(src).toBe('/api/v1/photos/p1/face?box=0.3125%2C0.2500%2C0.2000%2C0.2000')
    expect(src).not.toContain('/thumb/')
  })

  it('loads lazily, so a section of hundreds of faces does not request them all', () => {
    render(<FaceCrop photoUid="p1" bbox={BBOX} label="Anna" />)
    expect(screen.getByRole('img', { name: 'Anna' })).toHaveAttribute('loading', 'lazy')
  })

  it('hides the crop from a screen reader when the label is empty', () => {
    // The callers that pass "" put the crop beside a name that already says it.
    const { container } = render(<FaceCrop photoUid="p1" bbox={BBOX} label="" />)
    const img = container.querySelector('img')
    expect(img).toHaveAttribute('aria-hidden', 'true')
  })

  it('leaves an empty well rather than a broken image when no crop can be cut', () => {
    // A photograph with no usable preview yet answers with an error; the reader
    // should see nothing there, not the browser's torn-page glyph.
    const { container } = render(<FaceCrop photoUid="p1" bbox={BBOX} label="Anna" />)
    fireEvent.error(screen.getByRole('img', { name: 'Anna' }))
    expect(container.querySelector('img')).toBeNull()
  })

  it('keeps a square box, because the rendition it shows is square', () => {
    const { container } = render(<FaceCrop photoUid="p1" bbox={BBOX} label="Anna" size={44} />)
    const box = container.firstElementChild as HTMLElement
    expect(box.style.aspectRatio).toBe('1')
    expect(box.style.width).toBe('44px')
  })
})
