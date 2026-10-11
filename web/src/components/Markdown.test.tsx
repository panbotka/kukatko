import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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

  it('renders a GFM table inside a sideways-scrolling container', () => {
    const { container } = render(
      <Markdown>{'| fotka | rámeček |\n| --- | --- |\n| `a` | **b** |'}</Markdown>,
    )

    const table = screen.getByRole('table')
    expect(table).toHaveClass('table', 'table-sm')
    expect(table.parentElement).toHaveClass('kk-markdown__table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['fotka', 'rámeček'])
    expect(container.querySelector('td code')).toHaveTextContent('a')
    expect(container.querySelector('td strong')).toHaveTextContent('b')
  })

  it('renders strikethrough and autolinks a bare URL', () => {
    const { container } = render(<Markdown>{'~~staré~~ viz https://example.com/x'}</Markdown>)

    expect(container.querySelector('del')).toHaveTextContent('staré')
    expect(screen.getByRole('link', { name: 'https://example.com/x' })).toHaveAttribute(
      'target',
      '_blank',
    )
  })

  it('opens an own-origin link in the app, in the same tab', () => {
    const own = `${window.location.origin}/tasks/tk1`
    render(
      <MemoryRouter>
        <Markdown>{`[úkol](${own}) a [fotka](/photos/ph1?x=1#a)`}</Markdown>
      </MemoryRouter>,
    )

    const task = screen.getByRole('link', { name: 'úkol' })
    expect(task).toHaveAttribute('href', '/tasks/tk1')
    expect(task).not.toHaveAttribute('target')
    expect(task).not.toHaveAttribute('rel')
    expect(screen.getByRole('link', { name: 'fotka' })).toHaveAttribute('href', '/photos/ph1?x=1#a')
  })

  it('carries the task into a link to a photo', () => {
    render(
      <MemoryRouter>
        <Markdown taskUid="tk1">{`[fotka](${window.location.origin}/photos/ph1)`}</Markdown>
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'fotka' })).toHaveAttribute(
      'href',
      '/photos/ph1?task=tk1',
    )
  })

  it('keeps an own link a plain same-tab anchor outside a router', () => {
    render(<Markdown>{'[úkol](/tasks/tk1)'}</Markdown>)

    const link = screen.getByRole('link', { name: 'úkol' })
    expect(link).toHaveAttribute('href', '/tasks/tk1')
    expect(link).not.toHaveAttribute('target')
  })

  it('maps headings down from the floor and caps them at six', () => {
    const { container } = render(
      <Markdown headingFloor={4}>{'# Jedna\n\n## Dvě\n\n#### Čtyři'}</Markdown>,
    )

    expect(screen.getByRole('heading', { level: 4, name: 'Jedna' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 5, name: 'Dvě' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 6, name: 'Čtyři' })).toBeInTheDocument()
    expect(container.firstElementChild).toHaveClass('kk-markdown', 'kk-markdown--embedded')
  })

  it('keeps one newline inside a paragraph unless breaks is on', () => {
    const { container, rerender } = render(<Markdown>{'jedna\ndvě'}</Markdown>)
    expect(container.querySelector('br')).toBeNull()

    rerender(<Markdown breaks>{'jedna\ndvě'}</Markdown>)
    expect(container.querySelector('br')).not.toBeNull()
  })

  it('shows a lone number as typed, not as an empty list', () => {
    const { container } = render(<Markdown breaks>{'1987.\n2)'}</Markdown>)

    expect(container.querySelector('ol')).toBeNull()
    expect([...container.querySelectorAll('p')].map((p) => p.textContent)).toEqual(['1987.', '2)'])
  })

  it('keeps a list that has content', () => {
    render(<Markdown>{'1987. Léto\n1988. Zima'}</Markdown>)

    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })
})
