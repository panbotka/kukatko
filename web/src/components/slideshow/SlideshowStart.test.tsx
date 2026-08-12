import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { readSettings, SLIDESHOW_DEFAULTS, writeSettings } from '../../lib/slideshowSettings'
import { type SlideshowScope } from '../../lib/slideshowView'

import { SlideshowStart, type SlideshowStartProps } from './SlideshowStart'

const VIEW: LibraryView = { ...LIBRARY_DEFAULTS }
const NO_SCOPE: SlideshowScope = {}

/** Renders the current URL, so a test can assert the show actually started. */
function Here() {
  const { pathname, search } = useLocation()
  return <div data-testid="here">{pathname + search}</div>
}

function setup(overrides: Partial<SlideshowStartProps> = {}) {
  const props: SlideshowStartProps = { scope: NO_SCOPE, view: VIEW, ...overrides }
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <SlideshowStart {...props} />
        <Routes>
          <Route path="*" element={<Here />} />
        </Routes>
      </I18nextProvider>
    </MemoryRouter>,
  )
}

/** The "start" link, found by its Czech label (the default language). */
function startLink(): HTMLAnchorElement {
  return screen.getByRole('link', { name: 'Promítání' })
}

/** The open settings dialog. */
function dialog(): HTMLElement {
  return screen.getByRole('dialog')
}

/** Clicks the start button and waits for the dialog to open. */
async function openDialog(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.click(startLink())
  await screen.findByRole('dialog')
}

/** Where the router ended up. */
function here(): string {
  return screen.getByTestId('here').textContent
}

describe('SlideshowStart', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    await i18n.changeLanguage('cs')
  })
  afterEach(() => {
    window.localStorage.clear()
  })

  it('links to the slideshow, carrying the scope and the current filters', () => {
    setup({ scope: { album: 'al1' }, view: { ...VIEW, year: '2024' }, count: 3 })

    expect(startLink()).toHaveAttribute('href', '/slideshow?year=2024&album=al1')
  })

  it('carries the search mode so the slideshow replays the search', () => {
    setup({ scope: { mode: 'semantic' }, view: { ...VIEW, q: 'beach' }, count: 3 })

    expect(startLink()).toHaveAttribute('href', '/slideshow?q=beach&mode=semantic')
  })

  it('asks how you want to watch instead of starting the show', async () => {
    const user = userEvent.setup()
    setup({ count: 3 })

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await openDialog(user)

    // The click opened the dialog; it did not navigate into the player.
    expect(here()).toBe('/')
    for (const name of ['Přechod', 'Rychlost', 'Dokola', 'Náhodné pořadí', 'Název', 'Popis']) {
      expect(within(dialog()).getByLabelText(name)).toBeInTheDocument()
    }
  })

  it('pre-fills the settings last used', async () => {
    writeSettings({ ...SLIDESHOW_DEFAULTS, effect: 'slide', intervalMs: 15000, shuffle: true })
    const user = userEvent.setup()
    setup({ count: 3 })

    await openDialog(user)

    expect(within(dialog()).getByLabelText('Přechod')).toHaveValue('slide')
    expect(within(dialog()).getByLabelText('Rychlost')).toHaveValue('15000')
    expect(within(dialog()).getByLabelText('Náhodné pořadí')).toBeChecked()
  })

  it('states how long the show will take, and follows the chosen speed', async () => {
    const user = userEvent.setup()
    setup({ count: 40 }) // 40 × 5 s = 200 s = 3 min 20 s

    await openDialog(user)
    expect(within(dialog()).getByText(/3 min 20 s/)).toBeInTheDocument()

    await user.selectOptions(within(dialog()).getByLabelText('Rychlost'), '10000')
    // 40 × 10 s = 400 s = 6 min 40 s.
    expect(within(dialog()).getByText(/6 min 40 s/)).toBeInTheDocument()
    expect(within(dialog()).queryByText(/3 min 20 s/)).not.toBeInTheDocument()
  })

  it('says a repeating show starts again rather than quoting a total that is not true', async () => {
    const user = userEvent.setup()
    setup({ count: 40 })

    await openDialog(user)
    await user.click(within(dialog()).getByLabelText('Dokola'))

    expect(within(dialog()).getByText(/Jedno kolo trvá asi 3 min 20 s/)).toBeInTheDocument()
  })

  it('shows no estimate when the count is unknown', async () => {
    const user = userEvent.setup()
    setup()

    await openDialog(user)

    expect(within(dialog()).queryByText(/asi/)).not.toBeInTheDocument()
  })

  it('starts the show and saves the chosen settings on confirmation', async () => {
    const user = userEvent.setup()
    setup({ scope: { album: 'al1' }, view: VIEW, count: 3 })

    await openDialog(user)
    await user.selectOptions(within(dialog()).getByLabelText('Rychlost'), '3000')
    await user.click(within(dialog()).getByLabelText('Náhodné pořadí'))
    await user.click(screen.getByRole('button', { name: 'Spustit' }))

    expect(here()).toBe('/slideshow?album=al1')
    expect(readSettings()).toMatchObject({ intervalMs: 3000, shuffle: true })
  })

  it('starts nothing and changes nothing when dismissed', async () => {
    writeSettings({ ...SLIDESHOW_DEFAULTS, intervalMs: 5000 })
    const user = userEvent.setup()
    setup({ count: 3 })

    await openDialog(user)
    await user.selectOptions(within(dialog()).getByLabelText('Rychlost'), '30000')
    await user.click(screen.getByRole('button', { name: 'Zrušit' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(here()).toBe('/')
    // The dialog edits a draft: cancelling leaves the stored speed untouched.
    expect(readSettings().intervalMs).toBe(5000)
  })
})
