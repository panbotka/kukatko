import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { LibraryActionsMenu, type LibraryActionsMenuProps } from './LibraryActionsMenu'

/**
 * Renders the menu with its open state held outside, exactly as the viewer holds
 * it (so the auto-hiding chrome can be pinned while it is up).
 */
function renderMenu(props: Partial<LibraryActionsMenuProps> = {}) {
  function Host() {
    const [open, setOpen] = useState(false)
    return (
      <LibraryActionsMenu
        archived={false}
        hidden={false}
        archivePending={false}
        hidePending={false}
        onToggleArchive={vi.fn()}
        onToggleHidden={vi.fn()}
        {...props}
        open={open}
        onOpenChange={setOpen}
      />
    )
  }
  render(
    <I18nextProvider i18n={i18n}>
      <Host />
    </I18nextProvider>,
  )
}

/** Opens the menu and waits for its items. */
async function open(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Library actions' }))
  await screen.findByText('Library')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('LibraryActionsMenu', () => {
  it('names both operations in words, under a heading that says what they change', async () => {
    // The whole point of the menu: an act that takes a photograph out of the
    // library is read, not guessed from a glyph.
    const user = userEvent.setup()
    renderMenu()

    await open(user)
    expect(screen.getByText('Library')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Hide from the library' })).toHaveTextContent(
      'Hide from the library',
    )
    expect(screen.getByRole('button', { name: 'Archive' })).toHaveTextContent('Archive')
  })

  it('names the act available now, which is how it reports the state', async () => {
    const user = userEvent.setup()
    renderMenu({ hidden: true, archived: true })

    await open(user)
    expect(screen.getByRole('button', { name: 'Show in the library' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Hide from the library' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Archive' })).toBeNull()
  })

  it('runs the act the item names', async () => {
    const user = userEvent.setup()
    const onToggleHidden = vi.fn()
    renderMenu({ onToggleHidden })

    await open(user)
    await user.click(screen.getByRole('button', { name: 'Hide from the library' }))
    expect(onToggleHidden).toHaveBeenCalledTimes(1)
  })

  it('cannot be fired twice while its call is in flight', async () => {
    // Both operations are mutations of the catalogue; a double tap must not send
    // a second archive on the way to the first one's answer.
    const user = userEvent.setup()
    const onToggleArchive = vi.fn()
    renderMenu({ archivePending: true, hidePending: true, onToggleArchive })

    await open(user)
    // A menu item reports it the way a menu item does — `aria-disabled` plus the
    // marking, with the click swallowed — not with the `disabled` attribute a
    // toggle button would carry.
    expect(screen.getByRole('button', { name: 'Archive' })).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('button', { name: 'Hide from the library' })).toHaveAttribute(
      'aria-disabled',
      'true',
    )
    await user.click(screen.getByRole('button', { name: 'Archive' }))
    expect(onToggleArchive).not.toHaveBeenCalled()
  })

  it('keeps the trigger an icon with a name and a tooltip', () => {
    // It is the one control here that stays a glyph, so it carries the sentence
    // twice: for a screen reader and for a hovering mouse.
    renderMenu()

    const toggle = screen.getByRole('button', { name: 'Library actions' })
    expect(toggle).toHaveAttribute('title', 'Library actions')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })
})
