import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { CAPABILITIES_DEFAULT, CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'

import { HelpPage } from './HelpPage'

function renderHelp(caps = CAPABILITIES_DEFAULT) {
  return render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider value={caps}>
        <MemoryRouter>
          <HelpPage />
        </MemoryRouter>
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
}

/** Capabilities carrying a build, as the backend reports it. */
function withBuild(version: string, commit: string) {
  return { ...CAPABILITIES_DEFAULT, version: { version, commit } }
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
      'Adding photos from your phone',
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

  it('teaches the key:value query language in the search section, worked examples first', () => {
    renderHelp()

    // Three ready-made queries a reader can copy straight into the field.
    for (const query of ['year:1965', 'person:Jarmila rating:4-5', 'album:"Léto 2024" faces:2']) {
      expect(screen.getByText(query)).toBeInTheDocument()
    }
  })

  it('embeds the full query reference, so help and the ? modal cannot disagree', () => {
    renderHelp()

    // The very tables the search page's `?` opens, rendered inline: one source
    // of truth for the syntax, and the language is findable without stumbling
    // over the modal first.
    expect(screen.getByRole('heading', { name: 'Operators' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Filters' })).toBeInTheDocument()
    expect(screen.getByText('camera:')).toBeInTheDocument()
    expect(screen.getByText('rating:')).toBeInTheDocument()
  })

  it('notes that favourites and ratings are per-user', () => {
    renderHelp()

    expect(screen.getByText(/everyone has their own/i)).toBeInTheDocument()
  })

  it('spells out the build in full: the version and a link to the commit', () => {
    renderHelp(withBuild('0.5.1', '77fba72'))

    const build = screen.getByRole('region', { name: 'App version' })
    expect(within(build).getByText(/v0\.5\.1/)).toBeInTheDocument()

    // The commit is the one place the whole build identity is available, so it
    // links into the public repository — in a new tab, without handing it an
    // opener reference to this window.
    const commit = within(build).getByRole('link', { name: '77fba72' })
    expect(commit).toHaveAttribute('href', 'https://github.com/panbotka/kukatko/commit/77fba72')
    expect(commit).toHaveAttribute('target', '_blank')
    expect(commit).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('shows a development build as dev, with no commit link', () => {
    // An un-stamped binary reports the internal/version placeholders; "none" is
    // not a commit, so there is nothing to link to.
    renderHelp(withBuild('dev', 'none'))

    const build = screen.getByRole('region', { name: 'App version' })
    expect(within(build).getByText('dev')).toBeInTheDocument()
    expect(within(build).queryByRole('link')).not.toBeInTheDocument()
    expect(within(build).queryByText(/none/)).not.toBeInTheDocument()
  })

  it('stays intact when the capabilities call has not answered', () => {
    // The default context is what a failed call leaves behind: the page renders
    // as usual, only without the build block.
    renderHelp()

    expect(screen.getByRole('heading', { level: 1, name: 'Help' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'App version' })).not.toBeInTheDocument()
  })
})
