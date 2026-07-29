import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Button from 'react-bootstrap/Button'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { HeaderActions } from './HeaderActions'

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

/** A labelled button standing in for a page's real header action. */
function action(label: string, onClick?: () => void, danger = false) {
  return (
    <Button
      key={label}
      size="sm"
      variant={danger ? 'outline-danger' : 'outline-secondary'}
      onClick={onClick}
    >
      {label}
    </Button>
  )
}

function renderGroup(onDelete?: () => void) {
  return render(
    <I18nextProvider i18n={i18n}>
      <HeaderActions
        primary={[action('Slideshow')]}
        secondary={[action('Download'), action('Edit')]}
        destructive={[action('Delete', onDelete, true)]}
      />
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
  vi.restoreAllMocks()
})

describe('HeaderActions on a wide screen', () => {
  it('keeps every action inline, with no overflow toggle', () => {
    renderGroup()

    for (const name of ['Slideshow', 'Download', 'Edit', 'Delete']) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    }
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
    expect(document.querySelector('.dropdown-menu')).toBeNull()
  })

  it('renders the destructive action last, after the neutral ones', () => {
    renderGroup()

    const labels = screen.getAllByRole('button').map((button) => button.textContent)
    expect(labels).toEqual(['Slideshow', 'Download', 'Edit', 'Delete'])
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
    const menu = overflowMenu()
    expect(within(menu).getByRole('button', { name: 'Download' })).toBeInTheDocument()
    expect(within(menu).getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(within(menu).getByRole('button', { name: 'Delete' })).toBeInTheDocument()
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
    const remove = within(menu).getByRole('button', { name: 'Delete' })
    expect(remove).toHaveClass('btn-outline-danger')
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
    await user.click(within(overflowMenu()).getByRole('button', { name: 'Delete' }))

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
        <HeaderActions primary={[action('Slideshow')]} secondary={[false]} destructive={[false]} />
      </I18nextProvider>,
    )

    expect(screen.getByRole('button', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
  })
})
