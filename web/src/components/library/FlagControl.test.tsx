import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type RatingFlag } from '../../services/photos'

import { FlagControl } from './FlagControl'

function renderControl(
  flag: RatingFlag,
  onToggle: ((value: RatingFlag) => void) | undefined = vi.fn(),
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FlagControl flag={flag} onToggle={onToggle} />
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

  it('reports the eye mark when eye is pressed', async () => {
    const onToggle = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onToggle)

    await user.click(screen.getByRole('button', { name: 'Look at later' }))
    expect(onToggle).toHaveBeenCalledWith('eye')
  })

  it('reports the reject mark when the reject button is pressed', async () => {
    const onToggle = vi.fn()
    const user = userEvent.setup()
    renderControl('none', onToggle)

    await user.click(screen.getByRole('button', { name: 'Reject' }))
    expect(onToggle).toHaveBeenCalledWith('reject')
  })

  it('reports the active mark again rather than deciding the clear itself', async () => {
    // Pressing the mark that is already set means "clear it" — but the rule lives
    // in `useRating.toggleFlag`, which the p/r/v keys go through too, so this
    // control reports the press and nothing more.
    const onToggle = vi.fn()
    const user = userEvent.setup()
    renderControl('pick', onToggle)

    await user.click(screen.getByRole('button', { name: 'Pick' }))
    expect(onToggle).toHaveBeenCalledWith('pick')
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
