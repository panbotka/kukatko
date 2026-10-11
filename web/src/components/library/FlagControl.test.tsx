import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ratingHotkey } from '../../lib/ratingHotkeys'
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

  it('names each mark and its key on the tooltip’s first line', () => {
    renderControl('none')
    const firstLine = (name: string) =>
      screen.getByRole('button', { name }).getAttribute('title')?.split('\n')[0]
    expect(firstLine('Look at later')).toBe('Look at later (v)')
    expect(firstLine('Pick')).toBe('Pick (p)')
    expect(firstLine('Reject')).toBe('Reject (r)')
  })

  it('names the key that really presses the mark', () => {
    // The key in the tooltip and the key the page listens for come from two
    // places; this is what keeps them from drifting apart.
    renderControl('none')
    const marks = [
      ['Look at later', 'eye'],
      ['Pick', 'pick'],
      ['Reject', 'reject'],
    ] as const
    for (const [name, value] of marks) {
      const title = screen.getByRole('button', { name }).getAttribute('title') ?? ''
      const key = /\((.)\)/.exec(title)?.[1] ?? ''
      expect(ratingHotkey(key)).toEqual({ kind: 'flag', value })
    }
  })

  it('names the marks and their keys in Czech', async () => {
    await i18n.changeLanguage('cs')
    renderControl('none')
    const firstLine = (name: string) =>
      screen.getByRole('button', { name }).getAttribute('title')?.split('\n')[0]
    expect(firstLine('Prohlédnout později')).toBe('Prohlédnout později (v)')
    expect(firstLine('Vybrat')).toBe('Vybrat (p)')
    expect(firstLine('Zamítnout')).toBe('Zamítnout (r)')
  })

  it('puts the host’s hit-area class on every mark, and lets the host space them', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <FlagControl
          flag="none"
          onToggle={vi.fn()}
          className="kk-viewer__marks"
          buttonClassName="kk-viewer__target"
        />
      </I18nextProvider>,
    )
    for (const name of ['Look at later', 'Pick', 'Reject']) {
      expect(screen.getByRole('button', { name })).toHaveClass('kk-viewer__target')
    }
    // The host's class carries the spacing; Bootstrap's `!important` gap would
    // otherwise beat whatever the host's stylesheet sets.
    const group = screen.getByRole('group', { name: 'Personal mark' })
    expect(group).toHaveClass('kk-viewer__marks')
    expect(group).not.toHaveClass('gap-1')
  })

  it('tells the chosen mark apart by its glyph, not by colour alone', () => {
    const { container } = renderControl('pick')
    expect(container.querySelector('[aria-pressed="true"] .bi-hand-thumbs-up-fill')).not.toBeNull()
    for (const outline of ['.bi-eye', '.bi-hand-thumbs-down']) {
      expect(container.querySelector(`[aria-pressed="false"] ${outline}`)).not.toBeNull()
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
