import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { LibraryCharts, MonthPhotos } from '../../services/system'

import { LibraryChartsPanel } from './LibraryChartsPanel'

/**
 * A twelve-month arrivals window in the shape the backend fills it: every month
 * present, oldest first, the ones nothing arrived in reported as zero. `filled`
 * says which of them carry photos, so a test names the shape it is about.
 */
function arrivals(filled: Record<string, number> = {}): MonthPhotos[] {
  return Array.from({ length: 12 }, (_unused, offset) => {
    const month = `2026-${String(offset + 1).padStart(2, '0')}`
    return { month, photos: filled[month] ?? 0 }
  })
}

/** The ordinary case: several years of growth, several busy months. */
function charts(overrides: Partial<LibraryCharts> = {}): LibraryCharts {
  return {
    photos_by_year: [
      { year: 2024, photos: 12 },
      { year: 2025, photos: 40 },
    ],
    added_by_month: arrivals({ '2026-03': 120, '2026-07': 40, '2026-08': 900 }),
    top_cameras: [{ camera: 'Canon EOS 5D', model: 'Canon EOS 5D', photos: 800 }],
    storage_by_media: [
      { media: 'image', photos: 20, bytes: 4096 },
      { media: 'live', photos: 0, bytes: 0 },
      { media: 'video', photos: 2, bytes: 2048 },
      { media: 'raw', photos: 1, bytes: 1024 },
    ],
    storage_by_year: [
      { year: 2024, photos: 10, bytes: 4096, cumulative_bytes: 4096 },
      { year: 2025, photos: 5, bytes: 2048, cumulative_bytes: 6144 },
      { year: 2026, photos: 8, bytes: 1024, cumulative_bytes: 7168 },
    ],
    ...overrides,
  }
}

function renderPanel(overrides: Partial<LibraryCharts> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <LibraryChartsPanel charts={charts(overrides)} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('LibraryChartsPanel', () => {
  it('draws both time charts when the data has something to compare', () => {
    renderPanel()

    expect(screen.getByTestId('chart-added')).toBeInTheDocument()
    expect(screen.getByTestId('chart-growth')).toBeInTheDocument()
    expect(screen.queryByTestId('chart-added-statement')).toBeNull()
    expect(screen.queryByTestId('chart-growth-statement')).toBeNull()
  })

  it('states a one-year growth series instead of drawing a single bar', () => {
    // A library filled by one import: one year, so one bar, which fills the card
    // and reads as software that failed to draw a chart.
    renderPanel({
      storage_by_year: [{ year: 2026, photos: 20890, bytes: 4096, cumulative_bytes: 4096 }],
    })

    expect(screen.queryByTestId('chart-growth')).toBeNull()
    const stated = screen.getByTestId('chart-growth-statement')
    expect(stated).toHaveTextContent('4.0 KB')
    expect(stated).toHaveTextContent(
      'The only year anything was added: 2026, 20,890 photos in total.',
    )
  })

  it('keeps the growth chart for a library that really did grow over years', () => {
    // The rule is the length of the series, not anything known about this
    // instance: two years of imports are already a shape worth drawing.
    renderPanel({
      storage_by_year: [
        { year: 2025, photos: 10, bytes: 4096, cumulative_bytes: 4096 },
        { year: 2026, photos: 13, bytes: 3072, cumulative_bytes: 7168 },
      ],
    })

    expect(screen.getByTestId('chart-growth')).toBeInTheDocument()
    expect(screen.queryByTestId('chart-growth-statement')).toBeNull()
  })

  it('states the one busy month instead of drawing eleven zeroes beside it', () => {
    renderPanel({ added_by_month: arrivals({ '2026-08': 20890 }) })

    expect(screen.queryByTestId('chart-added')).toBeNull()
    const stated = screen.getByTestId('chart-added-statement')
    expect(stated).toHaveTextContent('20,890 photos')
    expect(stated).toHaveTextContent(
      'The only month with arrivals in the last 12 months: Aug 2026.',
    )
  })

  it('draws the arrivals chart as soon as a second month has photos in it', () => {
    renderPanel({ added_by_month: arrivals({ '2026-07': 1, '2026-08': 20890 }) })

    expect(screen.getByTestId('chart-added')).toBeInTheDocument()
    expect(screen.queryByTestId('chart-added-statement')).toBeNull()
    // The summary still names the busiest month of the window, which is the one
    // month a reader is looking for.
    expect(screen.getByTestId('chart-added')).toHaveAccessibleName(
      'Photos added to the library over the last 12 months. Photos in total: 20,891. The busiest month is Aug 2026: 20,890.',
    )
  })

  it('says nothing arrived when the whole window is empty', () => {
    renderPanel({ added_by_month: arrivals() })

    expect(screen.queryByTestId('chart-added')).toBeNull()
    expect(screen.queryByTestId('chart-added-statement')).toBeNull()
    expect(
      screen.getByText('Nothing arrived in the library over the last 12 months.'),
    ).toBeInTheDocument()
  })

  it('lets every card end at its own content, so none is padded out to a neighbour', () => {
    // jsdom lays nothing out, so the assertion is on the mechanism: `h-100`
    // stretched a card to the tallest in its grid row, which left the arrivals
    // chart beside the ten-row camera list with hundreds of pixels of nothing.
    const { container } = renderPanel()

    const cards = container.querySelectorAll('.card')
    expect(cards.length).toBeGreaterThan(0)
    for (const card of cards) {
      expect(card).not.toHaveClass('h-100')
    }
  })

  it('pairs the two time axes in one row and the two lists in the next', () => {
    // Panels are paired by the shape they draw: a ten-row list is half a screen
    // taller than a column chart, and pairing one with the other is what left the
    // short card with a wall of empty space beneath its axis.
    renderPanel()

    const titles = screen
      .getAllByRole('heading', { level: 3 })
      .map((heading) => heading.textContent)
    expect(titles).toEqual([
      'Photos by the year they were taken',
      'Added to the library',
      'Library growth',
      'Most used cameras',
      'Size by media type',
    ])
  })
})
