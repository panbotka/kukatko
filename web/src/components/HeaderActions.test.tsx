import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Button from 'react-bootstrap/Button'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { type HeaderAction, HeaderActions } from './HeaderActions'

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; a phone-width test
 * overrides it so the group takes its collapsed branch.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** A labelled button standing in for a page's real primary action. */
function primaryButton(label: string) {
  return (
    <Button key={label} size="sm" variant="outline-secondary">
      {label}
    </Button>
  )
}

/** A described header action standing in for a page's real one. */
function action(label: string, onSelect?: () => void): HeaderAction {
  return { key: label, label, onSelect }
}

function renderGroup(onDelete?: () => void) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <HeaderActions
          primary={[primaryButton('Slideshow')]}
          secondary={[action('Edit')]}
          overflow={[action('Download'), { key: 'share', label: 'Share', to: '/share' }]}
          destructive={[action('Delete', onDelete)]}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/**
 * The overflow menu, once it has been opened. react-bootstrap renders it only
 * after the first open and then keeps it mounted, toggling the `show` class —
 * jsdom loads no Bootstrap CSS, so "closed" is that class, not a missing node.
 */
function overflowMenu(): HTMLElement {
  const menu = document.querySelector<HTMLElement>('.dropdown-menu')
  if (menu === null) {
    throw new Error('the overflow menu has not been rendered')
  }
  return menu
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(() => {
  // Restore the shared desktop default so later tests never inherit a phone.
  mockViewport(false)
})

/** Opens the "…" menu and waits until it is shown. */
async function openMenu(user: ReturnType<typeof userEvent.setup>): Promise<HTMLElement> {
  await user.click(screen.getByRole('button', { name: 'More actions' }))
  await waitFor(() => {
    expect(overflowMenu()).toHaveClass('show')
  })
  return overflowMenu()
}

describe('HeaderActions on a wide screen', () => {
  it('keeps the primary and secondary actions inline and the rest in the menu', async () => {
    const user = userEvent.setup()
    renderGroup()

    // The header row is the one or two real actions plus the "…" toggle — not
    // a row of equal buttons nothing stands out from.
    const labels = screen.getAllByRole('button').map((button) => button.textContent)
    expect(labels).toEqual(['Slideshow', 'Edit', ''])
    expect(screen.queryByRole('button', { name: 'Download' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    const menu = await openMenu(user)
    const items = within(menu)
      .getAllByRole('menuitem')
      .map((item) => item.textContent)
    expect(items).toEqual(['Download', 'Share', 'Delete'])
    // The secondary action is not duplicated into the menu on a wide screen.
    expect(within(menu).queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
  })

  it('puts the destructive action last in the menu, behind a divider', async () => {
    const user = userEvent.setup()
    renderGroup()

    const menu = await openMenu(user)
    const divider = menu.querySelector('.dropdown-divider')
    expect(divider).not.toBeNull()
    const remove = within(menu).getByRole('menuitem', { name: 'Delete' })
    expect(remove).toHaveClass('dropdown-item', 'text-danger')
    expect(divider?.compareDocumentPosition(remove)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('folds a lone destructive action into the menu without a divider', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <HeaderActions primary={[primaryButton('Slideshow')]} destructive={[action('Delete')]} />
      </I18nextProvider>,
    )

    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    const menu = await openMenu(user)
    expect(within(menu).getByRole('menuitem', { name: 'Delete' })).toBeInTheDocument()
    expect(menu.querySelector('.dropdown-divider')).toBeNull()
  })

  it('raises no toggle when every action is inline', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <HeaderActions
          primary={[primaryButton('Slideshow')]}
          secondary={[action('Edit')]}
          overflow={[false]}
          destructive={[false]}
        />
      </I18nextProvider>,
    )

    expect(screen.getByRole('button', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
  })

  it('is operable from the keyboard alone', async () => {
    const onDelete = vi.fn()
    const user = userEvent.setup()
    renderGroup(onDelete)

    // The toggle is a labelled button in the tab order, after the inline ones.
    const toggle = screen.getByRole('button', { name: 'More actions' })
    await user.tab()
    await user.tab()
    await user.tab()
    expect(toggle).toHaveFocus()
    expect(toggle).toHaveAttribute('aria-expanded', 'false')

    await user.keyboard('{Enter}')
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    // Opened from the keyboard, the menu hands focus to its first item and is
    // walked with the arrow keys from there, like any menu.
    expect(within(overflowMenu()).getByRole('menuitem', { name: 'Download' })).toHaveFocus()
    await user.keyboard('{ArrowDown}')
    expect(within(overflowMenu()).getByRole('menuitem', { name: 'Share' })).toHaveFocus()

    // Escape closes it again and hands focus back to the toggle.
    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(overflowMenu()).not.toHaveClass('show')
    })
    expect(toggle).toHaveFocus()

    // And an item is activated with Enter, which closes the menu behind it.
    await user.keyboard('{Enter}')
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.keyboard('{ArrowDown}{ArrowDown}')
    expect(within(overflowMenu()).getByRole('menuitem', { name: 'Delete' })).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(onDelete).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(overflowMenu()).not.toHaveClass('show')
    })
  })

  it('renders the menu as a menu of items, not a stack of buttons', async () => {
    const user = userEvent.setup()
    renderGroup()

    const menu = await openMenu(user)
    expect(menu).toHaveAttribute('role', 'menu')
    expect(menu).toHaveAttribute('aria-labelledby', 'header-overflow')
    expect(menu.querySelector('.btn')).toBeNull()
    for (const item of within(menu).getAllByRole('menuitem')) {
      expect(item).toHaveClass('dropdown-item')
    }
    // An action with an address stays a real link, openable in a new tab.
    expect(within(menu).getByRole('menuitem', { name: 'Share' })).toHaveAttribute('href', '/share')
  })

  it('renders a secondary action as a button of its own variant on the row', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <HeaderActions secondary={[{ key: 'edit', label: 'Edit', inlineVariant: 'primary' }]} />
        </MemoryRouter>
      </I18nextProvider>,
    )

    expect(screen.getByRole('button', { name: 'Edit' })).toHaveClass('btn-primary', 'btn-sm')
  })

  it('skips a disabled item when walking the menu', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <HeaderActions
            overflow={[{ key: 'busy', label: 'Busy', disabled: true }, action('Share')]}
          />
        </MemoryRouter>
      </I18nextProvider>,
    )

    const menu = await openMenu(user)
    // react-bootstrap marks a disabled item with `aria-disabled` and the
    // `disabled` class, which is also what keeps the arrow keys off it.
    expect(within(menu).getByRole('menuitem', { name: 'Busy' })).toHaveAttribute(
      'aria-disabled',
      'true',
    )
    within(menu).getByRole('menuitem', { name: 'Share' }).focus()
    await user.keyboard('{ArrowUp}')
    expect(within(menu).getByRole('menuitem', { name: 'Share' })).toHaveFocus()
  })
})

describe('HeaderActions on a narrow (phone) screen', () => {
  it('keeps the primary action inline and folds the rest into the overflow menu', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderGroup()

    // The header stays a single compact row: only the primary action and the
    // "…" toggle are on it.
    expect(screen.getByRole('button', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Download' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'More actions' }))

    // Nothing is lost — the collapsed actions all live in the menu.
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    // The secondary action leads, then the occasional ones, the destructive last.
    const items = within(overflowMenu())
      .getAllByRole('menuitem')
      .map((item) => item.textContent)
    expect(items).toEqual(['Edit', 'Download', 'Share', 'Delete'])
  })

  it('separates the destructive action from the neutral ones by a divider', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderGroup()

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    const menu = overflowMenu()

    // A mis-tap must not land on "Delete" from the action above it: a divider
    // sits between them, and the button itself is styled as destructive.
    const divider = menu.querySelector('.dropdown-divider')
    expect(divider).not.toBeNull()
    const remove = within(menu).getByRole('menuitem', { name: 'Delete' })
    expect(remove).toHaveClass('dropdown-item', 'text-danger')
    expect(divider?.compareDocumentPosition(remove)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('runs the chosen action and closes the menu behind it', async () => {
    const onDelete = vi.fn()
    mockViewport(true)
    const user = userEvent.setup()
    renderGroup(onDelete)

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.click(within(overflowMenu()).getByRole('menuitem', { name: 'Delete' }))

    expect(onDelete).toHaveBeenCalledTimes(1)
    // A menu left standing open behind the dialog its item just raised reads as
    // a stuck page, so the group closes itself on any click inside.
    await waitFor(() => {
      expect(overflowMenu()).not.toHaveClass('show')
    })
  })

  it('shows no toggle when there is nothing to collapse', () => {
    mockViewport(true)
    render(
      <I18nextProvider i18n={i18n}>
        {/* What a viewer sees on an empty album: the conditional slots render
            `false`, and an overflow button opening an empty menu is worse than
            no button at all. */}
        <HeaderActions
          primary={[primaryButton('Slideshow')]}
          secondary={[false]}
          destructive={[false]}
        />
      </I18nextProvider>,
    )

    expect(screen.getByRole('button', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
  })
})
