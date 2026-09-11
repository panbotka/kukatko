import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { StatTileGrid, type StatTileSpec } from './StatTile'

function renderTiles(tiles: StatTileSpec[]) {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <StatTileGrid tiles={tiles} />
      </I18nextProvider>
    </MemoryRouter>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('StatTile', () => {
  it('formats the number for the active language', async () => {
    renderTiles([{ key: 'photos', labelKey: 'system.tiles.photos', value: 20930 }])
    expect(screen.getByTestId('tile-photos')).toHaveTextContent('20,930')

    await i18n.changeLanguage('cs')
    renderTiles([{ key: 'videos', labelKey: 'system.tiles.videos', value: 20930 }])
    // A narrow no-break space in Czech, so match on the digits either side of it.
    expect(screen.getByTestId('tile-videos').textContent).toMatch(/^20\s?930$/)
    await i18n.changeLanguage('en')
  })

  it('colours a backlog as work waiting, and only while there is some', () => {
    renderTiles([
      { key: 'clusters', labelKey: 'system.remaining.clusters', value: 41, tone: 'queued' },
      { key: 'without-ocr', labelKey: 'system.remaining.withoutOcr', value: 0, tone: 'queued' },
    ])

    expect(screen.getByTestId('tile-clusters')).toHaveClass('kk-count--queued')
    const cleared = screen.getByTestId('tile-without-ocr')
    expect(cleared).toHaveClass('text-secondary')
    expect(cleared.className).not.toMatch(/kk-count--(queued|running|failed)/)
  })

  it('leaves a plain library count in the body colour', () => {
    renderTiles([{ key: 'albums', labelKey: 'system.tiles.albums', value: 132 }])
    expect(screen.getByTestId('tile-albums').className).toBe('kk-display')
  })

  it('gives a non-zero failure a glyph as well as the colour', () => {
    renderTiles([
      { key: 'a', labelKey: 'system.tiles.photos', value: 3, tone: 'failed' },
      { key: 'b', labelKey: 'system.tiles.photos', value: 0, tone: 'failed' },
    ])

    const failing = screen.getByTestId('tile-a')
    expect(failing).toHaveClass('kk-count--failed')
    expect(failing.querySelector('.bi-exclamation-triangle')).not.toBeNull()

    const clear = screen.getByTestId('tile-b')
    expect(clear).not.toHaveClass('kk-count--failed')
    expect(clear.querySelector('.bi-exclamation-triangle')).toBeNull()
  })

  it('shows a value that is not a count at all muted and untouched', () => {
    // The duplicates tile while the background scan has no answer yet: it is not
    // a figure the eye should be drawn to, and it must not be formatted either.
    renderTiles([
      {
        key: 'duplicates',
        labelKey: 'system.remaining.duplicates',
        value: '—',
        tone: 'queued',
      },
    ])

    const tile = screen.getByTestId('tile-duplicates')
    expect(tile).toHaveTextContent('—')
    expect(tile).toHaveClass('text-secondary')
    expect(tile.className).not.toMatch(/kk-count--(queued|running|failed)/)
  })

  it('still makes the whole card the click target, named by its label', () => {
    renderTiles([
      { key: 'trashed', labelKey: 'system.tiles.trashed', value: 7, to: '/trash' },
      { key: 'faces', labelKey: 'system.tiles.faces', value: 7 },
    ])

    // The accessible name is the label, not the number.
    const link = screen.getByRole('link', { name: 'In the trash' })
    expect(link).toHaveAttribute('href', '/trash')
    expect(link).toHaveClass('stretched-link')
    // A tile with nowhere to go offers no link at all.
    expect(screen.getAllByRole('link')).toHaveLength(1)
  })
})
