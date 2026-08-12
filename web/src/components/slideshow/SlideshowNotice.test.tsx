import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { SlideshowNotice } from './SlideshowNotice'

function setup() {
  const onClose = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <SlideshowNotice onClose={onClose}>
        <p>Loading photos…</p>
      </SlideshowNotice>
    </I18nextProvider>,
  )
  return onClose
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SlideshowNotice', () => {
  it('shows what it was given', () => {
    setup()
    expect(screen.getByText('Loading photos…')).toBeInTheDocument()
  })

  it('closes on the button', async () => {
    const user = userEvent.setup()
    const onClose = setup()

    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on Escape, like the running player', () => {
    // Waiting for photos must not be the one state of the slideshow the key
    // that leaves it does not work in.
    const onClose = setup()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('ignores other keys', () => {
    const onClose = setup()

    fireEvent.keyDown(window, { key: 'ArrowRight' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
