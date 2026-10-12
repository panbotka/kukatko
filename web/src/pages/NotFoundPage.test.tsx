import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { NotFoundPage } from './NotFoundPage'

/** Mounts the page as the `*` route would: for a URL nothing else matched. */
function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/p/v/no-such-page']}>
        <NotFoundPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

afterEach(async () => {
  // Czech is the instance default; a test that switched away restores it.
  await i18n.changeLanguage('cs')
})

describe('NotFoundPage', () => {
  it('is drawn with the shared empty state, like every other "nothing here" screen', async () => {
    await i18n.changeLanguage('cs')
    renderPage()

    const empty = within(screen.getByTestId('not-found-page')).getByTestId('empty-state')
    expect(empty.querySelector('.kk-empty-state__icon .bi-compass')).not.toBeNull()
    expect(
      within(empty).getByRole('heading', { level: 1, name: 'Stránka nenalezena' }),
    ).toBeInTheDocument()
    expect(within(empty).getByText('Požadovaná stránka neexistuje.')).toBeInTheDocument()
    expect(within(empty).getByRole('link', { name: 'Zpět na domovskou stránku' })).toHaveAttribute(
      'href',
      '/p/v',
    )
  })

  it('speaks English too', async () => {
    await i18n.changeLanguage('en')
    renderPage()

    expect(screen.getByRole('heading', { level: 1, name: 'Page not found' })).toBeInTheDocument()
    expect(screen.getByText('The requested page does not exist.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to home' })).toHaveAttribute('href', '/p/v')
  })
})
