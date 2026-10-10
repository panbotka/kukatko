import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider, useTranslation } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { readLanguagePreference } from '../i18n/accountLanguage'
import { expectChosenToggle } from '../test/toggle'
import { LanguageSwitcher } from './LanguageSwitcher'

/** Probe component that renders a translated string so we can observe switches. */
function NavLabelProbe() {
  const { t } = useTranslation()
  return <span data-testid="nav-home-label">{t('nav.home')}</span>
}

function renderSwitcher(uid: string | null = 'u1') {
  const tree = (
    <I18nextProvider i18n={i18n}>
      <NavLabelProbe />
      <LanguageSwitcher />
    </I18nextProvider>
  )
  if (uid === null) {
    return render(tree)
  }
  const auth = { user: { uid } } as unknown as AuthContextValue
  return render(<AuthContext.Provider value={auth}>{tree}</AuthContext.Provider>)
}

describe('LanguageSwitcher', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    await i18n.changeLanguage('cs')
  })

  it('defaults to Czech and switches the active language to English on click', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    expect(i18n.language).toBe('cs')
    expect(screen.getByTestId('nav-home-label')).toHaveTextContent('Domů')
    const languages = screen.getAllByRole('button')
    expectChosenToggle(languages, screen.getByRole('button', { name: 'Čeština' }))

    await user.click(screen.getByRole('button', { name: 'English' }))

    expect(i18n.language).toBe('en')
    expect(screen.getByTestId('nav-home-label')).toHaveTextContent('Home')
    expectChosenToggle(languages, screen.getByRole('button', { name: 'English' }))
  })

  it('remembers the choice for the signed-in account only', async () => {
    const user = userEvent.setup()
    renderSwitcher('u1')

    await user.click(screen.getByRole('button', { name: 'English' }))

    expect(readLanguagePreference('u1')).toBe('en')
    expect(readLanguagePreference('u2')).toBeNull()
    expect(window.localStorage.getItem('i18nextLng')).toBeNull()
  })

  it('switches without remembering anything outside a session', async () => {
    const user = userEvent.setup()
    renderSwitcher(null)

    await user.click(screen.getByRole('button', { name: 'English' }))

    expect(i18n.language).toBe('en')
    expect(window.localStorage.length).toBe(0)
  })
})
