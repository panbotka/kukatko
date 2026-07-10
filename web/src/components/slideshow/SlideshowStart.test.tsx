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

/** Persists a slideshow interval, as the player's speed picker would. */
function persistInterval(intervalMs: number): void {
  window.localStorage.setItem(
    'kukatko.slideshow.settings',
    JSON.stringify({ effect: 'fade', intervalMs }),
  )
}

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

  it('estimates the running time from the count and the persisted interval', () => {
    persistInterval(5000)

    setup({ count: 40 })

    expect(screen.getByText('40 fotek, asi 3 min 20 s')).toBeInTheDocument()
  })

  it('recomputes the estimate when the persisted interval changes', () => {
    persistInterval(10000)

    setup({ count: 40 })

    expect(screen.getByText('40 fotek, asi 6 min 40 s')).toBeInTheDocument()
  })

  it('warns that a 400-photo album is over half an hour', () => {
    persistInterval(5000)

    setup({ count: 400 })

    expect(screen.getByText('400 fotek, asi 33 min 20 s')).toBeInTheDocument()
  })

  it('reaches into hours for a long show', () => {
    persistInterval(15000)

    setup({ count: 400 })

    expect(screen.getByText('400 fotek, asi 1 h 40 min')).toBeInTheDocument()
  })

  it('pluralizes the photo count in Czech', () => {
    persistInterval(5000)

    setup({ count: 1 })

    expect(screen.getByText('1 fotka, asi 5 s')).toBeInTheDocument()
  })

  it('omits the estimate when the count is not known yet', () => {
    setup()

    expect(startLink()).toBeInTheDocument()
    expect(screen.queryByText(/asi/)).not.toBeInTheDocument()
  })

  it('omits the estimate for an empty set', () => {
    setup({ count: 0 })

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
