import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'
import { type GuardRole } from '../services/auth'

import { ForbiddenPage } from './ForbiddenPage'

/** Mounts the page as a guard would: standing in for a protected route. */
function renderPage(role: GuardRole) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/review']}>
        <ForbiddenPage role={role} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

afterEach(async () => {
  // Czech is the instance default; a test that switched away restores it.
  await i18n.changeLanguage('cs')
})

describe('ForbiddenPage', () => {
  it('names the role the route actually needs', async () => {
    await i18n.changeLanguage('en')
    const cases: [GuardRole, RegExp][] = [
      ['editor', /editor role/i],
      ['admin', /administrator role/i],
      ['maintainer', /maintainer role/i],
    ]

    for (const [role, expected] of cases) {
      const { unmount } = renderPage(role)
      expect(screen.getByTestId('forbidden-page')).toHaveTextContent(expected)
      unmount()
    }
  })

  it('offers the library as the way out', async () => {
    await i18n.changeLanguage('en')
    renderPage('editor')

    // The link matters most on the fullscreen guarded routes (/review,
    // /duplicates/compare), where there is no navbar to escape through.
    expect(screen.getByRole('link', { name: /back to the library/i })).toHaveAttribute('href', '/')
  })

  it('explains itself in Czech, the default language', async () => {
    await i18n.changeLanguage('cs')
    renderPage('editor')

    expect(
      screen.getByRole('heading', { level: 1, name: 'Sem nemáte přístup' }),
    ).toBeInTheDocument()
    expect(
      screen.getByText('Na tuhle část potřebujete roli editora — požádejte o ni správce.'),
    ).toBeInTheDocument()
  })
})
