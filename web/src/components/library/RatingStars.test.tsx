import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { RatingStars } from './RatingStars'

function renderStars(rating: number, onRate: ((value: number) => void) | undefined = vi.fn()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <RatingStars rating={rating} onRate={onRate} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('RatingStars', () => {
  it('renders five clickable stars reflecting the current rating', () => {
    renderStars(3)
    const stars = screen.getAllByRole('button')
    expect(stars).toHaveLength(5)
    // Stars up to the rating are pressed; the rest are not.
    expect(stars[0]).toHaveAttribute('aria-pressed', 'true')
    expect(stars[2]).toHaveAttribute('aria-pressed', 'true')
    expect(stars[3]).toHaveAttribute('aria-pressed', 'false')
  })

  it('sets the clicked rating', async () => {
    const onRate = vi.fn()
    const user = userEvent.setup()
    renderStars(1, onRate)

    await user.click(screen.getByRole('button', { name: 'Rate 4 of 5' }))
    expect(onRate).toHaveBeenCalledWith(4)
  })

  it('clears to 0 when the current rating star is clicked again', async () => {
    const onRate = vi.fn()
    const user = userEvent.setup()
    renderStars(3, onRate)

    await user.click(screen.getByRole('button', { name: 'Rate 3 of 5' }))
    expect(onRate).toHaveBeenCalledWith(0)
  })

  it('names the digit key that sets each star in its tooltip', () => {
    renderStars(0)
    for (const n of [1, 2, 3, 4, 5]) {
      const star = screen.getByRole('button', { name: `Rate ${n} of 5` })
      // The accessible name stays the short sentence; only the tooltip adds the key.
      expect(star).toHaveAttribute('title', `Rate ${n} of 5 (${n})`)
    }
  })

  it('names the key in Czech too', async () => {
    await i18n.changeLanguage('cs')
    renderStars(0)
    expect(screen.getByRole('button', { name: 'Ohodnotit 3 z 5' })).toHaveAttribute(
      'title',
      'Ohodnotit 3 z 5 (3)',
    )
  })

  it('drops the key from the tooltip when the host says the digits are taken', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <RatingStars rating={0} onRate={vi.fn()} shortcutHints={false} />
      </I18nextProvider>,
    )
    expect(screen.getByRole('button', { name: 'Rate 3 of 5' })).toHaveAttribute(
      'title',
      'Rate 3 of 5',
    )
  })

  it('puts the host’s hit-area class on every star', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <RatingStars rating={2} onRate={vi.fn()} buttonClassName="kk-viewer__target" />
      </I18nextProvider>,
    )
    const stars = screen.getAllByRole('button')
    expect(stars).toHaveLength(5)
    for (const star of stars) {
      expect(star).toHaveClass('kk-viewer__target')
    }
  })

  it('renders read-only (no buttons) when onRate is omitted', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <RatingStars rating={2} />
      </I18nextProvider>,
    )
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Rated 2 of 5' })).toBeInTheDocument()
  })
})
