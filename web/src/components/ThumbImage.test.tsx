import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { ThumbImage } from './ThumbImage'
import { ThumbUnavailable } from './ThumbUnavailable'

describe('ThumbImage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('cs')
  })

  it('swaps a failed image for the "no preview" placeholder in a box of its size', () => {
    const onError = vi.fn()
    render(
      <I18nextProvider i18n={i18n}>
        <ThumbImage
          src="/thumb/a"
          alt="Pláž"
          width={56}
          height={56}
          className="rounded"
          onError={onError}
        />
      </I18nextProvider>,
    )

    fireEvent.error(screen.getByRole('img', { name: 'Pláž' }))

    expect(onError).toHaveBeenCalledOnce()
    expect(screen.queryByRole('img', { name: 'Pláž' })).not.toBeInTheDocument()
    const placeholder = screen.getByRole('img', { name: 'Náhled není dostupný' })
    const box = placeholder.parentElement
    expect(box).toHaveClass('kk-thumb-unavailable-box', 'rounded')
    expect(box).toHaveStyle({ width: '56px', height: '56px' })
    // No <img> is left anywhere to show the browser's broken-image icon.
    expect(document.querySelector('img')).toBeNull()
  })

  it('tries a new address afresh', () => {
    const { rerender } = render(
      <I18nextProvider i18n={i18n}>
        <ThumbImage src="/thumb/a" alt="A" />
      </I18nextProvider>,
    )
    fireEvent.error(screen.getByRole('img', { name: 'A' }))

    rerender(
      <I18nextProvider i18n={i18n}>
        <ThumbImage src="/thumb/b" alt="A" />
      </I18nextProvider>,
    )

    expect(screen.getByRole('img', { name: 'A' })).toHaveAttribute('src', '/thumb/b')
  })
})

describe('ThumbUnavailable', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('cs')
  })

  it("draws the library tile's own fallback in the tile variant", () => {
    render(
      <I18nextProvider i18n={i18n}>
        <ThumbUnavailable variant="tile" />
      </I18nextProvider>,
    )

    const placeholder = screen.getByRole('img', { name: 'Náhled není dostupný' })
    expect(placeholder).toHaveClass('kk-tile__unavailable')
    expect(placeholder.querySelector('.kk-tile__unavailable-text')).toHaveTextContent(
      'Náhled není dostupný',
    )
  })

  it('centres the glyph and words in any box by default', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <ThumbUnavailable />
      </I18nextProvider>,
    )

    const placeholder = screen.getByRole('img', { name: 'Náhled není dostupný' })
    expect(placeholder).toHaveClass('kk-thumb-unavailable')
    expect(placeholder.querySelector('.bi-image')).toHaveAttribute('aria-hidden', 'true')
  })
})
