import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { SearchQueryHelp } from './SearchQueryHelp'

function renderHelp() {
  return render(
    <I18nextProvider i18n={i18n}>
      <SearchQueryHelp />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SearchQueryHelp', () => {
  it('opens the help modal from the ? trigger', async () => {
    const user = userEvent.setup()
    renderHelp()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent('Operators')
    expect(dialog).toHaveTextContent('Filters')
  })

  it('gives the ? trigger a finger-sized hit area without growing on desktop', () => {
    // Icon-only and `p-0`, so on touch its width would otherwise be the glyph.
    // The touch-scoped helper squares it up to 44px on coarse pointers only.
    renderHelp()
    const trigger = screen.getByRole('button', { name: 'Search query language help' })
    expect(trigger).toHaveClass('kukatko-tap-target-touch')
    expect(trigger.querySelector('.bi-question-circle')).toHaveAttribute('aria-hidden', 'true')
  })

  it('keeps the tables inside the modal on a phone', async () => {
    // The columns are code, wider together than a 320px viewport: the modal goes
    // fullscreen below `sm` and each table gets its own scroll wrapper, so the
    // dialog itself never overflows sideways.
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    expect(dialog.querySelector('.modal-dialog')).toHaveClass('modal-fullscreen-sm-down')
    const tables = dialog.querySelectorAll('table')
    expect(tables).toHaveLength(2)
    for (const table of tables) {
      expect(table.parentElement).toHaveClass('table-responsive')
    }
  })

  it('renders every query fragment as code, never as a backtick', async () => {
    // The descriptions used to spell `person:me` in the translated sentence, and
    // a plain-text table cell printed the backticks as characters. The fragments
    // are data now, so the dialog holds real <code> and no backtick anywhere.
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).not.toContain('`')

    const codes = [...dialog.querySelectorAll('code')].map((code) => code.textContent)
    expect(codes).toContain('person:me')
    expect(codes).toContain('uploader:me')
    expect(codes).toContain('uploader:none')
    expect(codes).toContain('dated:no')
    // The fragment a note already showed is not repeated as the row's example.
    expect(codes.filter((code) => code === 'uploader:none')).toHaveLength(1)
  })

  it('renders the query fragments as code in Czech too', async () => {
    // Both locales carried the same backticks, so both are checked.
    await i18n.changeLanguage('cs')
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Nápověda k vyhledávacímu jazyku' }))

    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).not.toContain('`')
    expect([...dialog.querySelectorAll('code')].map((code) => code.textContent)).toContain(
      'person:me',
    )
  })

  it('lets a multi-key row wrap between its keys, never inside one', async () => {
    // `favorite: private: archived:` on one unbreakable line was what made the
    // filter table wider than a phone; each key is now its own nowrap chunk.
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    const cell = within(dialog).getByText('favorite:').closest('td')
    expect(cell).not.toHaveClass('text-nowrap')
    const keys = cell?.querySelectorAll('code') ?? []
    expect([...keys].map((code) => code.textContent)).toEqual([
      'favorite:',
      'private:',
      'archived:',
    ])
    for (const code of keys) {
      expect(code).toHaveClass('text-nowrap')
    }
  })
})
