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
  it('reflects the active flag via aria-pressed', () => {
    renderControl('pick')
    expect(screen.getByRole('button', { name: 'Pick' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Reject' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('sets the reject flag when reject is clicked', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onFlag)

    await user.click(screen.getByRole('button', { name: 'Reject' }))
    expect(onFlag).toHaveBeenCalledWith('reject')
  })

  it('clears the flag when the active flag is clicked again', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('pick', onFlag)

    await user.click(screen.getByRole('button', { name: 'Pick' }))
    expect(onFlag).toHaveBeenCalledWith('none')
  })

  it('disables its buttons when read-only', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <FlagControl flag="none" />
      </I18nextProvider>,
    )
    expect(screen.getByRole('button', { name: 'Pick' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeDisabled()
  })
})
