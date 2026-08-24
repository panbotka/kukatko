import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { UserEmail } from './UserEmail'

function renderEmail(email: string) {
  render(
    <I18nextProvider i18n={i18n}>
      <UserEmail email={email} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('UserEmail', () => {
  it('prints a real address as it stands', () => {
    renderEmail('ada@example.com')

    expect(screen.getByText('ada@example.com')).toBeInTheDocument()
    expect(screen.queryByText('No address')).not.toBeInTheDocument()
  })

  it('shows a .invalid placeholder as no address at all, and asks for one', () => {
    renderEmail('ada@kukatko.invalid')

    // The placeholder itself is never shown: an administrator must not be able
    // to read it as a mailbox somebody could be reached at.
    expect(screen.queryByText('ada@kukatko.invalid')).not.toBeInTheDocument()
    expect(screen.getByText('No address')).toBeInTheDocument()
    expect(screen.getByText(/Fill in a real one/)).toBeInTheDocument()
  })
})
