import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider, useTranslation } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'
import { LanguageSwitcher } from './LanguageSwitcher'

/** Probe component that renders a translated string so we can observe switches. */
function NavLabelProbe() {
  const { t } = useTranslation()
  return <span data-testid="nav-home-label">{t('nav.home')}</span>
}

function renderSwitcher() {
  return render(
    <I18nextProvider i18n={i18n}>
      <NavLabelProbe />
      <LanguageSwitcher />
    </I18nextProvider>,
  )
}

describe('LanguageSwitcher', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('cs')
  })

  it('defaults to Czech and switches the active language to English on click', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    expect(i18n.language).toBe('cs')
    expect(screen.getByTestId('nav-home-label')).toHaveTextContent('Domů')

    await user.click(screen.getByRole('button', { name: 'English' }))

    expect(i18n.language).toBe('en')
    expect(screen.getByTestId('nav-home-label')).toHaveTextContent('Home')
  })
})
