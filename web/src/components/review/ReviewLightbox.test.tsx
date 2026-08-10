import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { ReviewLightbox, type ReviewLightboxProps } from './ReviewLightbox'
import { type ReviewStageProps } from './ReviewStage'

const STAGE: ReviewStageProps = {
  photoUid: 'ph1',
  fileWidth: 4000,
  fileHeight: 3000,
  orientation: 1,
  size: 'fit_1280',
  href: '/photos/ph1',
  alt: 'a photo',
}

function renderLightbox(props: Partial<ReviewLightboxProps> = {}) {
  const onClose = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <ReviewLightbox stage={STAGE} onClose={onClose} {...props} />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return { onClose }
}

/** The overlay itself — it is a portal, so everything is queried through it. */
function dialog(): HTMLElement {
  return screen.getByRole('dialog')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ReviewLightbox', () => {
  it('shows the photo big, with the way out to its page', () => {
    renderLightbox()

    expect(within(dialog()).getByRole('img', { name: 'a photo' })).toBeInTheDocument()
    expect(within(dialog()).getByTestId('review-open-photo')).toHaveAttribute('href', '/photos/ph1')
  })

  it('closes on Escape', () => {
    const { onClose } = renderLightbox()

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('does not close when the photo itself is clicked', async () => {
    // The photo is what you came to look at: clicking it must not take it away.
    const user = userEvent.setup()
    const { onClose } = renderLightbox()

    await user.click(within(dialog()).getByRole('img', { name: 'a photo' }))

    expect(onClose).not.toHaveBeenCalled()
  })

  it('steps with the arrow keys, and only where there is somewhere to go', () => {
    const onNext = vi.fn()
    const onPrev = vi.fn()
    renderLightbox({ onNext, onPrev, hasNext: true, hasPrev: false })

    fireEvent.keyDown(document, { key: 'ArrowRight' })
    fireEvent.keyDown(document, { key: 'ArrowLeft' })

    expect(onNext).toHaveBeenCalledTimes(1)
    expect(onPrev).not.toHaveBeenCalled()
  })

  it('opens the photo in a new tab on o, matching the corner anchor', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderLightbox()

    fireEvent.keyDown(document, { key: 'o' })

    expect(open).toHaveBeenCalledWith('/photos/ph1', '_blank', 'noopener,noreferrer')
    open.mockRestore()
  })

  it('renders the caller’s own actions and lets them fire', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn()
    renderLightbox({
      children: (
        <button type="button" onClick={onConfirm}>
          Yes
        </button>
      ),
    })

    await user.click(within(dialog()).getByRole('button', { name: 'Yes' }))

    expect(onConfirm).toHaveBeenCalled()
  })
})
