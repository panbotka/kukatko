import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { HelpPage } from './HelpPage'

function renderHelp() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <HelpPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('HelpPage', () => {
  it('opens with the page title and an intro', () => {
    renderHelp()

    expect(screen.getByRole('heading', { level: 1, name: 'Help' })).toBeInTheDocument()
    expect(screen.getByText(/your photo gallery/i)).toBeInTheDocument()
  })

  it('renders the main help sections as collapsible headers', () => {
    renderHelp()

    // Each section is an accordion header (a button); the table-of-contents
    // entries reuse the same labels as links.
    for (const name of [
      'Browsing photos',
      'Search',
      'Albums',
      'Labels',
      'Favourites & ratings',
      'People & faces',
      'Duplicates',
      'Map & places',
      'Deleting & the trash',
      'User roles',
      'Your account',
    ]) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    }
  })

  it('offers a table of contents that links to each section anchor', () => {
    renderHelp()

    const toc = screen.getByRole('navigation', { name: 'Contents' })
    const albums = within(toc).getByRole('link', { name: 'Albums' })
    expect(albums).toHaveAttribute('href', '#help-albums')
  })

  it('spells out the role ladder inside the roles section', () => {
    renderHelp()

    // The role names come from the shared `roles.*` keys, each with a plain
    // description of what that level can do.
    expect(screen.getByText('Viewer')).toBeInTheDocument()
    expect(screen.getByText('Editor')).toBeInTheDocument()
    expect(screen.getByText('Administrator')).toBeInTheDocument()
    expect(screen.getByText('Maintainer')).toBeInTheDocument()
  })

  it('notes that favourites and ratings are per-user', () => {
    renderHelp()

    expect(screen.getByText(/everyone has their own/i)).toBeInTheDocument()
  })
})
