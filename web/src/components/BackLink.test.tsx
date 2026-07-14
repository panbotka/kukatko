import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { BackLink } from './BackLink'

function renderLink(to: string, label = 'Zpět na alba') {
  return render(
    <MemoryRouter>
      <BackLink to={to} label={label} />
    </MemoryRouter>,
  )
}

describe('BackLink', () => {
  it('names the destination and links to it', () => {
    renderLink('/albums')

    const link = screen.getByRole('link', { name: 'Zpět na alba' })
    expect(link).toHaveAttribute('href', '/albums')
  })

  it('keeps the query string of the destination, so the list view state survives', () => {
    // "Back always works": the list's filters/sort/page live in the query params,
    // so the link carries them rather than calling history.back().
    renderLink('/albums?sort=oldest&year=2024')

    expect(screen.getByRole('link', { name: 'Zpět na alba' })).toHaveAttribute(
      'href',
      '/albums?sort=oldest&year=2024',
    )
  })

  it('renders the arrow as a decorative bootstrap icon', () => {
    const { container } = renderLink('/labels', 'Zpět na štítky')

    const icon = container.querySelector('.bi-arrow-left')
    expect(icon).not.toBeNull()
    // The visible label is the accessible name; the glyph stays out of it.
    expect(icon).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('link', { name: 'Zpět na štítky' })).toBeInTheDocument()
  })

  it('merges extra classes onto the link', () => {
    renderLink('/people', 'Zpět na lidi')
    const link = screen.getByRole('link', { name: 'Zpět na lidi' })
    expect(link).toHaveClass('kk-back-link')

    render(
      <MemoryRouter>
        <BackLink to="/people" label="Zpět na lidi" className="flex-shrink-0" />
      </MemoryRouter>,
    )
    const [, second] = screen.getAllByRole('link', { name: 'Zpět na lidi' })
    expect(second).toHaveClass('kk-back-link', 'flex-shrink-0')
  })
})
