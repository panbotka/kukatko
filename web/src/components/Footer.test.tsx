import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { Footer } from './Footer'

function renderFooter(text?: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <Footer text={text} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('Footer', () => {
  it("renders a footer landmark carrying the project's own line", () => {
    renderFooter('Provozuje SDH Veselice')
    // <footer> at the top level carries the contentinfo landmark role.
    const footer = screen.getByRole('contentinfo')
    expect(footer).toHaveTextContent('Provozuje SDH Veselice · GitHub')
    expect(screen.getByTestId('footer-text')).toHaveTextContent('Provozuje SDH Veselice')
  })

  it('names no operator without a line — no hard-coded tenant, no stray separator', () => {
    renderFooter()
    const footer = screen.getByRole('contentinfo')
    expect(footer).not.toHaveTextContent('Veselice')
    expect(footer).not.toHaveTextContent('·')
    expect(screen.queryByTestId('footer-text')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'GitHub' })).toBeInTheDocument()
  })

  it('treats a whitespace-only line as none', () => {
    renderFooter('   ')
    expect(screen.queryByTestId('footer-text')).not.toBeInTheDocument()
  })

  it('links to the GitHub repository in a new tab with safe rel attributes', () => {
    renderFooter()
    const link = screen.getByRole('link', { name: 'GitHub' })
    expect(link).toHaveAttribute('href', 'https://github.com/panbotka/kukatko')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('keeps the credits intact next to the status content it hosts', () => {
    // The maintainers' job-queue line (`JobQueueStatus`) is passed in as
    // children; it sits beside the credits, never inside or instead of them.
    render(
      <I18nextProvider i18n={i18n}>
        <Footer text="Operated by the club">
          <a href="/system">17 jobs permanently failed</a>
        </Footer>
      </I18nextProvider>,
    )
    const footer = screen.getByRole('contentinfo')
    expect(footer).toHaveTextContent('Operated by the club · GitHub')
    const status = screen.getByRole('link', { name: '17 jobs permanently failed' })
    expect(footer).toContainElement(status)
    expect(status).toHaveAttribute('href', '/system')
  })

  it('hides the decorative GitHub icon from assistive technology', () => {
    renderFooter()
    const icon = screen.getByRole('link', { name: 'GitHub' }).querySelector('i.bi.bi-github')
    expect(icon).not.toBeNull()
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })
})
