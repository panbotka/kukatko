import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PersonAvatar } from './PersonAvatar'

/**
 * The rendered photo, if there is one. An `alt=""` image maps to the
 * `presentation` role and is `aria-hidden`, so it takes the hidden query — which
 * is exactly the point: the name is always written out beside the avatar.
 */
function photo(): HTMLElement | null {
  return screen.queryByRole('presentation', { hidden: true })
}

describe('PersonAvatar', () => {
  it('draws the linked person’s cover photo when there is one', () => {
    render(<PersonAvatar name="Jarmila" photoUid="ph_1" />)

    const img = screen.getByRole('presentation', { hidden: true })
    expect(img.getAttribute('src')).toBe('/api/v1/photos/ph_1/thumb/tile_224')
    expect(img).toHaveAttribute('aria-hidden', 'true')
    expect(img).toHaveAttribute('alt', '')
  })

  it('falls back to the coloured initial without a cover photo', () => {
    render(<PersonAvatar name="Jarmila" />)

    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })

  it('treats an empty photo uid as no photo', () => {
    render(<PersonAvatar name="Jarmila" photoUid="" />)

    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })

  it('falls back to the initial when the photo fails to load', () => {
    render(<PersonAvatar name="Jarmila" photoUid="ph_gone" />)

    fireEvent.error(screen.getByRole('presentation', { hidden: true }))

    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })
})
