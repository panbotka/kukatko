import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { SearchQueryHelp } from './SearchQueryHelp'

function renderHelp(fullSearchHref?: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <SearchQueryHelp fullSearchHref={fullSearchHref} />
      </MemoryRouter>
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

  it('offers ranked search at the foot of the dialog when given a destination', async () => {
    // The library's quick filter passes it: the link used to be the tail of a
    // helper sentence under the field, a row of chrome above the photographs.
    const user = userEvent.setup()
    renderHelp('/search?q=sunset')
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    const link = within(dialog).getByRole('link', { name: /Full-text & semantic search/ })
    expect(link).toHaveAttribute('href', '/search?q=sunset')
    expect(link.closest('.modal-footer')).not.toBeNull()

    // Following it closes the dialog rather than leaving it over the search page.
    await user.click(link)
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('has no footer without a destination (the search page is the destination)', async () => {
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Search query language help' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).queryByRole('link')).not.toBeInTheDocument()
    expect(dialog.querySelector('.modal-footer')).toBeNull()
  })

  it('renders the ? trigger as the standard icon button', () => {
    // The app's icon-only button: a quiet `outline-secondary` `btn-sm` (~32px),
    // squared up to 44px on coarse pointers by the touch-scoped helper — no
    // longer a bare 16px link glyph in the muted grey.
    renderHelp()
    const trigger = screen.getByRole('button', { name: 'Search query language help' })
    expect(trigger).toHaveClass('btn-outline-secondary', 'btn-sm', 'kukatko-tap-target-touch')
    expect(trigger).not.toHaveClass('btn-link')
    expect(trigger).toHaveAttribute('title', 'Search query language help')
    expect(trigger.querySelector('.bi-question-circle')).toHaveAttribute('aria-hidden', 'true')
  })

  it('names the ? trigger in Czech by default', async () => {
    await i18n.changeLanguage('cs')
    renderHelp()
    const trigger = screen.getByRole('button', { name: 'Nápověda k vyhledávacímu jazyku' })
    expect(trigger).toHaveAttribute('title', 'Nápověda k vyhledávacímu jazyku')
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
