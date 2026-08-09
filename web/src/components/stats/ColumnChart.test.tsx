import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { ColumnChart, type ColumnDatum } from './ColumnChart'

/** Three years of a histogram: a tall one, an empty one, and a linked short one. */
function years(): ColumnDatum[] {
  return [
    { key: '1905', value: 40, title: '1905: 40 photos', tick: '1905', href: '/?year=1905' },
    { key: '1906', value: 0, title: '1906: no photos', tick: undefined },
    {
      key: '1907',
      value: 10,
      title: '1907: 10 photos',
      href: '/?year=1907',
      linkLabel: 'Open 1907',
    },
  ]
}

function renderChart(data: ColumnDatum[], ariaLabel = 'Photos by year, 51 in total') {
  return render(
    <MemoryRouter>
      <ColumnChart data={data} ariaLabel={ariaLabel} testId="chart" />
    </MemoryRouter>,
  )
}

describe('ColumnChart', () => {
  it('scales every bar against the tallest, and never rounds a small one away', () => {
    renderChart(years())

    // 40 is the tallest, so it fills the plot; 10 is a quarter of it.
    expect(screen.getByTestId('chart-bar-1905')).toHaveStyle({ height: '100%' })
    expect(screen.getByTestId('chart-bar-1907')).toHaveStyle({ height: '25%' })
  })

  it('keeps a one-photo year visible instead of drawing it as empty', () => {
    renderChart([
      { key: '2020', value: 5000, title: 'a' },
      { key: '2021', value: 1, title: 'b' },
    ])

    // 1/5000 rounds to 0 %; the floor keeps the year on the chart.
    expect(screen.getByTestId('chart-bar-2021')).toHaveStyle({ height: '2%' })
  })

  it('draws an empty bucket as a baseline tick with no height of its own', () => {
    renderChart(years())

    const empty = screen.getByTestId('chart-bar-1906')
    expect(empty).toHaveClass('kk-chart-bar--empty')
    expect(empty.style.height).toBe('')
  })

  it('carries the summary numbers as the chart´s accessible name', () => {
    renderChart(years())

    expect(screen.getByTestId('chart')).toHaveAccessibleName('Photos by year, 51 in total')
  })

  it('links the buckets that have photos, naming the destination', () => {
    renderChart(years())

    const chart = screen.getByRole('group', { name: 'Photos by year, 51 in total' })
    expect(within(chart).getByRole('link', { name: 'Open 1907' })).toHaveAttribute(
      'href',
      '/?year=1907',
    )
    // An empty bucket leads nowhere: there would be nothing behind the link.
    expect(within(chart).getAllByRole('link')).toHaveLength(2)
  })

  it('is a single image when nothing links anywhere', () => {
    renderChart([{ key: '2026-01', value: 3, title: 'January: 3 photos', tick: 'Jan' }])

    expect(screen.getByRole('img', { name: 'Photos by year, 51 in total' })).toBeInTheDocument()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('labels only the ticked columns, so the axis stays readable', () => {
    renderChart(years())

    expect(screen.getByText('1905')).toBeInTheDocument()
    expect(screen.queryByText('1907')).toBeNull()
  })

  it('renders an all-zero chart without dividing by its own maximum', () => {
    renderChart([
      { key: '2025', value: 0, title: 'nothing' },
      { key: '2026', value: 0, title: 'nothing' },
    ])

    expect(screen.getByTestId('chart-bar-2025')).toHaveClass('kk-chart-bar--empty')
    expect(screen.getByTestId('chart-bar-2026')).toHaveClass('kk-chart-bar--empty')
  })
})
