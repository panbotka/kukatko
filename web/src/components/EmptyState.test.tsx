import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { EmptyState } from './EmptyState'

describe('EmptyState', () => {
  it('renders the title and the hint', () => {
    render(<EmptyState title="Zatím žádná alba" hint="Vytvoř album a začni." />)

    expect(screen.getByText('Zatím žádná alba')).toBeInTheDocument()
    expect(screen.getByText('Vytvoř album a začni.')).toBeInTheDocument()
  })

  it('omits the hint when none is given', () => {
    render(<EmptyState title="Bez náhledu" />)

    expect(screen.getByTestId('empty-state').querySelector('.kk-empty-state__hint')).toBeNull()
  })

  it('renders the default icon hidden from assistive technology', () => {
    const { container } = render(<EmptyState title="Prázdno" />)

    const icon = container.querySelector('.kk-empty-state__icon')
    expect(icon).not.toBeNull()
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })

  it('renders a custom icon in place of the default one', () => {
    render(<EmptyState title="Prázdno" icon={<svg data-testid="custom-icon" />} />)

    expect(screen.getByTestId('custom-icon')).toBeInTheDocument()
  })

  it('renders the optional action button', () => {
    render(<EmptyState title="Zatím žádné štítky" action={<button type="button">Nový</button>} />)

    expect(screen.getByRole('button', { name: 'Nový' })).toBeInTheDocument()
  })

  it('omits the action wrapper when no action is given', () => {
    render(<EmptyState title="Zatím žádné štítky" />)

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('applies the compact variant and merges extra classes', () => {
    render(<EmptyState title="Bez štítků" size="sm" className="mt-2" />)

    const root = screen.getByTestId('empty-state')
    expect(root).toHaveClass('kk-empty-state', 'kk-empty-state--sm', 'kk-appear', 'mt-2')
  })

  it('does not apply the compact variant by default', () => {
    render(<EmptyState title="Prázdno" />)

    expect(screen.getByTestId('empty-state')).not.toHaveClass('kk-empty-state--sm')
  })
})
