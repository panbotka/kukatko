import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Markdown } from './Markdown'

describe('Markdown', () => {
  it('renders headings, emphasis and lists', () => {
    render(<Markdown>{'# Ahoj\n\n**tučně**\n\n- jedna\n- dvě'}</Markdown>)

    expect(screen.getByRole('heading', { level: 1, name: 'Ahoj' })).toBeInTheDocument()
    expect(screen.getByText('tučně').tagName).toBe('STRONG')
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })

  it('opens a link in a new tab with the usual protections', () => {
    render(<Markdown>{'[archiv](https://example.com/fotky)'}</Markdown>)

    const link = screen.getByRole('link', { name: 'archiv' })
    expect(link).toHaveAttribute('href', 'https://example.com/fotky')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('strips a javascript: href', () => {
    const { container } = render(<Markdown>{'[klikni](javascript:alert(1))'}</Markdown>)

    // The text survives, the scheme does not — so there is nothing to click.
    expect(screen.getByText('klikni')).toBeInTheDocument()
    expect(container.innerHTML).not.toContain('javascript:')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('does not render raw HTML from the source', () => {
    const { container } = render(
      <Markdown>{'<img src="x" onerror="alert(1)">\n\n<b>bold?</b>'}</Markdown>,
    )

    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('b')).toBeNull()
    expect(container.innerHTML).not.toContain('onerror')
  })

  it('applies the wrapper class it is given', () => {
    const { container } = render(<Markdown className="kk-welcome">text</Markdown>)

    expect(container.firstElementChild).toHaveClass('kk-welcome')
  })
})
