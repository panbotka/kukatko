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
  it('renders one toggle button per personal-marking state', () => {
    renderControl('none')
    expect(screen.getByRole('button', { name: 'Look at later' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Pick' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument()
  })

  it('names the buttons after the act, never after the glyph', () => {
    renderControl('none')
    // A button called "Thumbs up" teaches nobody what it does; the whole point
    // of the name is that it says what the mark is for.
    for (const shape of [/thumbs/i, /^Eye$/]) {
      expect(screen.queryByRole('button', { name: shape })).not.toBeInTheDocument()
    }
  })

  it('explains each mark in a one-sentence tooltip', () => {
    renderControl('none')
    for (const name of ['Look at later', 'Pick', 'Reject']) {
      const title = screen.getByRole('button', { name }).getAttribute('title')
      // A sentence about the mark, not a second copy of the button's name.
      expect(title).toMatch(/\.$/)
      expect(title).not.toBe(name)
    }
  })

  it('names the marks in Czech too', async () => {
    await i18n.changeLanguage('cs')
    renderControl('none')
    expect(screen.getByRole('button', { name: 'Prohlédnout později' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Vybrat' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Zamítnout' })).toBeInTheDocument()
  })

  it('reflects the active flag via aria-pressed', () => {
    renderControl('pick')
    expect(screen.getByRole('button', { name: 'Pick' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Reject' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Look at later' })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
  })

  it('marks the eye state as active when the eye flag is set', () => {
    renderControl('eye')
    expect(screen.getByRole('button', { name: 'Look at later' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('sets the eye flag when eye is clicked', async () => {
    const onFlag = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onFlag)

    await user.click(screen.getByRole('button', { name: 'Look at later' }))
    expect(onFlag).toHaveBeenCalledWith('eye')
  })

  it('sets the reject flag when the reject button is clicked', async () => {
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
    expect(screen.getByRole('button', { name: 'Look at later' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Pick' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeDisabled()
  })
})
