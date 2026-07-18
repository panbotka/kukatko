import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ListSkeleton, Skeleton, TileGridSkeleton } from './Skeleton'

describe('Skeleton', () => {
  it('renders a decorative, sized shimmer block', () => {
    render(<Skeleton width={40} height="1rem" />)

    const block = document.querySelector('.kk-skeleton')
    expect(block).toBeInTheDocument()
    expect(block).toHaveAttribute('aria-hidden', 'true')
    expect(block).toHaveStyle({ width: '40px', height: '1rem' })
  })

  it('rounds fully when asked for a circle', () => {
    render(<Skeleton circle width={40} height={40} />)

    expect(document.querySelector('.kk-skeleton')).toHaveStyle({ borderRadius: '50%' })
  })
})

describe('TileGridSkeleton', () => {
  it('exposes a single labelled status and draws the requested card count', () => {
    render(<TileGridSkeleton label="Loading albums…" count={5} />)

    const status = screen.getByRole('status', { name: 'Loading albums…' })
    expect(status).toBeInTheDocument()
    // One card per requested count (the visually-hidden label is not aria-hidden).
    expect(status.querySelectorAll(':scope > [aria-hidden="true"]')).toHaveLength(5)
  })
})

describe('ListSkeleton', () => {
  it('exposes a labelled status with the requested row count', () => {
    render(<ListSkeleton label="Loading labels…" count={3} />)

    const status = screen.getByRole('status', { name: 'Loading labels…' })
    expect(status).toBeInTheDocument()
    expect(status.querySelectorAll('.kk-skeleton')).toHaveLength(3)
  })
})
