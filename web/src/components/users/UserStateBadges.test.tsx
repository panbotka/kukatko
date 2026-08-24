import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { UserStateBadges } from './UserStateBadges'

function renderBadges(user: { approved_at?: string | null; disabled: boolean }) {
  render(
    <I18nextProvider i18n={i18n}>
      <UserStateBadges user={user} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('UserStateBadges', () => {
  it('badges an approved, enabled account as active', () => {
    renderBadges({ approved_at: '2026-08-01T10:00:00Z', disabled: false })

    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for approval')).not.toBeInTheDocument()
  })

  it('badges a waiting account as waiting, never as active', () => {
    renderBadges({ approved_at: null, disabled: false })

    // It is enabled in the database, but it cannot sign in — so the reassuring
    // green badge would be a lie.
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument()
    expect(screen.queryByText('Enabled')).not.toBeInTheDocument()
  })

  it('paints waiting and blocked differently', () => {
    renderBadges({ approved_at: null, disabled: false })
    const waiting = screen.getByText('Waiting for approval')

    renderBadges({ approved_at: '2026-08-01T10:00:00Z', disabled: true })
    const blocked = screen.getByText('Disabled')

    // Two different states must not look like one: the classes differ, and
    // neither carries the other's colour.
    expect(waiting).toHaveClass('bg-warning')
    expect(blocked).toHaveClass('bg-danger')
    expect(waiting).not.toHaveClass('bg-danger')
    expect(blocked).not.toHaveClass('bg-warning')
  })

  it('shows both when an account was never approved and is blocked too', () => {
    renderBadges({ approved_at: null, disabled: true })

    // Two things to undo, so two badges: unblocking alone would not let them in.
    expect(screen.getByText('Disabled')).toBeInTheDocument()
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument()
  })
})
