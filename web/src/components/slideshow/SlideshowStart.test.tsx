import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SlideshowScope } from '../../lib/slideshowView'

import { SlideshowStart, type SlideshowStartProps } from './SlideshowStart'

const VIEW: LibraryView = { ...LIBRARY_DEFAULTS }
const NO_SCOPE: SlideshowScope = {}

function setup(overrides: Partial<SlideshowStartProps> = {}) {
  const props: SlideshowStartProps = { scope: NO_SCOPE, view: VIEW, ...overrides }
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <SlideshowStart {...props} />
      </I18nextProvider>
    </MemoryRouter>,
  )
}

/** The "start" link, found by its Czech label (the default language). */
function startLink(): HTMLAnchorElement {
  return screen.getByRole('link', { name: 'Promítání' })
}

describe('SlideshowStart', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })
  afterEach(() => {
    window.localStorage.clear()
  })

  it('renders only the start link, with no running-time estimate', () => {
    setup({ count: 40 })

    expect(startLink()).toBeInTheDocument()
    // The estimate now lives in the player, beside the speed control, so the
    // start screen shows nothing about duration — even when a count is known.
    expect(screen.queryByText(/asi/)).not.toBeInTheDocument()
    expect(screen.queryByText(/fotek|fotka|fotky/)).not.toBeInTheDocument()
  })

  it('shows no estimate even for a large set', () => {
    setup({ count: 400 })

    expect(startLink()).toBeInTheDocument()
    expect(screen.queryByText(/asi/)).not.toBeInTheDocument()
  })

  it('links to the slideshow, carrying the scope and the current filters', () => {
    setup({ scope: { album: 'al1' }, view: { ...VIEW, year: '2024' }, count: 3 })

    expect(startLink()).toHaveAttribute('href', '/slideshow?year=2024&album=al1')
  })

  it('carries the search mode so the slideshow replays the search', () => {
    setup({ scope: { mode: 'semantic' }, view: { ...VIEW, q: 'beach' }, count: 3 })

    expect(startLink()).toHaveAttribute('href', '/slideshow?q=beach&mode=semantic')
  })
})
