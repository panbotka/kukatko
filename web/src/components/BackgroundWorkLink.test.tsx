import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { BackgroundWorkLink } from './BackgroundWorkLink'

function renderLink() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <BackgroundWorkLink title="Background work" intro="Repairs run in the background." />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

describe('BackgroundWorkLink', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  it('names the work and links to System status, with no counts of its own', () => {
    renderLink()
    expect(screen.getByRole('heading', { name: 'Background work' })).toBeInTheDocument()
    expect(screen.getByText('Repairs run in the background.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open System status' })).toHaveAttribute(
      'href',
      '/system',
    )
  })

  it('labels the link in Czech by default', async () => {
    await i18n.changeLanguage('cs')
    renderLink()
    expect(screen.getByRole('link', { name: 'Otevřít Stav systému' })).toBeInTheDocument()
  })
})
