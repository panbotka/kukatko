import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { BarList, type BarDatum } from './BarList'

/** Two cameras, the second half as busy as the first. */
function cameras(): BarDatum[] {
  return [
    {
      key: 'canon',
      label: 'Canon EOS 5D',
      value: 800,
      valueLabel: '800',
      title: 'Canon EOS 5D: 800 photos',
      href: '/?camera=Canon+EOS+5D',
      linkLabel: 'Show photos taken with Canon EOS 5D',
    },
    { key: 'pixel', label: 'Pixel 7', value: 400, valueLabel: '400', title: 'Pixel 7: 400 photos' },
  ]
}

function renderList(data: BarDatum[], ariaLabel = 'Most used cameras, 2 of them') {
  return render(
    <MemoryRouter>
      <BarList data={data} ariaLabel={ariaLabel} testId="bars" />
    </MemoryRouter>,
  )
}

describe('BarList', () => {
  it('writes every row´s name and value out, so identity is never the bar alone', () => {
    renderList(cameras())

    expect(screen.getByText('Canon EOS 5D')).toBeInTheDocument()
    expect(screen.getByText('800')).toBeInTheDocument()
    expect(screen.getByText('Pixel 7')).toBeInTheDocument()
    expect(screen.getByText('400')).toBeInTheDocument()
  })

  it('fills each track against the largest row', () => {
    renderList(cameras())

    expect(screen.getByTestId('bar-fill-canon')).toHaveStyle({ width: '100%' })
    expect(screen.getByTestId('bar-fill-pixel')).toHaveStyle({ width: '50%' })
  })

  it('leaves a zero row empty rather than full', () => {
    renderList([
      { key: 'video', label: 'Videos', value: 0, valueLabel: '0 B', title: 'Videos: 0 B' },
    ])

    expect(screen.getByTestId('bar-fill-video')).toHaveStyle({ width: '0%' })
  })

  it('carries the summary numbers as the list´s accessible name', () => {
    renderList(cameras())

    const list = screen.getByRole('group', { name: 'Most used cameras, 2 of them' })
    expect(
      within(list).getByRole('link', { name: 'Show photos taken with Canon EOS 5D' }),
    ).toHaveAttribute('href', '/?camera=Canon+EOS+5D')
  })

  it('is a single image when no row links anywhere', () => {
    renderList([{ key: 'raw', label: 'RAW', value: 5, valueLabel: '5 MB', title: 'RAW: 5 MB' }])

    expect(screen.getByRole('img', { name: 'Most used cameras, 2 of them' })).toBeInTheDocument()
    expect(screen.queryByRole('link')).toBeNull()
  })
})
