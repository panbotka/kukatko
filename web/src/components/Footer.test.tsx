import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { Footer } from './Footer'

function renderFooter() {
  return render(
    <I18nextProvider i18n={i18n}>
      <Footer />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('Footer', () => {
  it('renders a footer landmark naming the operator', () => {
    renderFooter()
    // <footer> at the top level carries the contentinfo landmark role.
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Operated by SDH Veselice')
  })

  it('translates the operator text', async () => {
    await i18n.changeLanguage('cs')
    renderFooter()
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Provozuje SDH Veselice')
  })

  it('links to the GitHub repository in a new tab with safe rel attributes', () => {
    renderFooter()
    const link = screen.getByRole('link', { name: 'GitHub' })
    expect(link).toHaveAttribute('href', 'https://github.com/panbotka/kukatko')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('hides the decorative GitHub icon from assistive technology', () => {
    renderFooter()
    const icon = screen.getByRole('link', { name: 'GitHub' }).querySelector('i.bi.bi-github')
    expect(icon).not.toBeNull()
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })
})
