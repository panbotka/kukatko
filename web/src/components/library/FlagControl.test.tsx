import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type RatingFlag } from '../../services/photos'

import { FlagControl } from './FlagControl'

function renderControl(
  flag: RatingFlag,
  onFlag: ((value: RatingFlag) => void) | undefined = vi.fn(),
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FlagControl flag={flag} onFlag={onFlag} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FlagControl', () => {
  it('renders one toggle button per personal mark', () => {
    renderControl('none')
    expect(screen.getByRole('button', { name: 'Eye' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Thumbs up' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Thumbs down' })).toBeInTheDocument()
  })

  it('reflects the active mark via aria-pressed', () => {
    renderControl('pick')
    expect(screen.getByRole('button', { name: 'Thumbs up' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(screen.getByRole('button', { name: 'Thumbs down' })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
    expect(screen.getByRole('button', { name: 'Eye' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('sets the eye mark when the eye is clicked', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onFlag)

    await user.click(screen.getByRole('button', { name: 'Eye' }))
    expect(onFlag).toHaveBeenCalledWith('eye')
  })

  it('sets the reject mark when thumbs-down is clicked', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onFlag)

    await user.click(screen.getByRole('button', { name: 'Thumbs down' }))
    expect(onFlag).toHaveBeenCalledWith('reject')
  })

  it('clears the mark when the active mark is clicked again', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('eye', onFlag)

    await user.click(screen.getByRole('button', { name: 'Eye' }))
    expect(onFlag).toHaveBeenCalledWith('none')
  })

  it('disables its buttons when read-only', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <FlagControl flag="none" />
      </I18nextProvider>,
    )
    expect(screen.getByRole('button', { name: 'Eye' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Thumbs up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Thumbs down' })).toBeDisabled()
  })
})
