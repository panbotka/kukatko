import { render, screen, within } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { Markdown } from './Markdown'

/** Renders under the i18n provider, for the mention chip's tooltips. */
function renderEn(ui: ReactNode) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>)
}

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
    expect(task).toHaveAttribute('href', '/p/v/tasks/tk1')
    expect(task).not.toHaveAttribute('target')
    expect(task).not.toHaveAttribute('rel')
    expect(screen.getByRole('link', { name: 'fotka' })).toHaveAttribute(
      'href',
      '/p/v/photos/ph1?x=1#a',
    )
  })

  it('keeps the project an own link names and leaves instance pages unprefixed', () => {
    render(
      <MemoryRouter>
        <Markdown taskUid="tk1">
          {`[fotka](${window.location.origin}/p/jiny/photos/ph1) a [účet](/account)`}
        </Markdown>
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'fotka' })).toHaveAttribute(
      'href',
      '/p/jiny/photos/ph1?task=tk1',
    )
    expect(screen.getByRole('link', { name: 'účet' })).toHaveAttribute('href', '/account')
  })

  it('carries the task into a link to a photo', () => {
    render(
      <MemoryRouter>
        <Markdown taskUid="tk1">{`[fotka](${window.location.origin}/photos/ph1)`}</Markdown>
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'fotka' })).toHaveAttribute(
      'href',
      '/p/v/photos/ph1?task=tk1',
    )
  })

  it('keeps an own link a plain same-tab anchor outside a router', () => {
    render(<Markdown>{'[úkol](/tasks/tk1)'}</Markdown>)

    const link = screen.getByRole('link', { name: 'úkol' })
    expect(link).toHaveAttribute('href', '/p/v/tasks/tk1')
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

  describe('mentions', () => {
    beforeAll(async () => {
      await i18n.changeLanguage('en')
    })
    const mentions = [
      { user_uid: 'us1', name: 'Tomáš Kozák', subject_uid: 'su1' },
      { user_uid: 'us2', name: 'Anna Nováková', subject_uid: null },
    ]

    it('renders a token as a chip linking to the linked person, under the current name', () => {
      renderEn(
        <MemoryRouter>
          <Markdown mentions={mentions}>{'Hele @[Tomas K.](user:us1), víš to?'}</Markdown>
        </MemoryRouter>,
      )

      // The stored name was „Tomas K."; the account is now „Tomáš Kozák".
      const chip = screen.getByRole('link', { name: 'Tomáš Kozák' })
      expect(chip).toHaveAttribute('href', '/p/v/people/su1')
      expect(chip).toHaveClass('kk-mention')
      expect(chip).not.toHaveClass('kk-mention--me')
      expect(chip.querySelector('.bi-at')).not.toBeNull()
      expect(screen.getByText(/víš to\?/)).toBeInTheDocument()
      expect(document.body.textContent).not.toContain('@[')
    })

    it('draws an unlinked account as a plain chip and the reader’s own stronger', () => {
      renderEn(
        <Markdown mentions={mentions} currentUserUid="us2">
          {'@[Anna](user:us2) a @[Tomáš](user:us1)'}
        </Markdown>,
      )

      expect(screen.queryByRole('link', { name: 'Anna Nováková' })).not.toBeInTheDocument()
      const me = screen.getByText('Anna Nováková')
      expect(me).toHaveClass('kk-mention', 'kk-mention--me')
      expect(me).toHaveAttribute('title', 'Mention: Anna Nováková (that is you)')
      expect(screen.getByText('Tomáš Kozák')).not.toHaveClass('kk-mention--me')
    })

    it('keeps the stored name, muted and unlinked, for an account that is gone', () => {
      renderEn(<Markdown mentions={mentions}>{'@[Bedřich](user:us9) víš?'}</Markdown>)

      const chip = screen.getByText('Bedřich')
      expect(chip).toHaveClass('kk-mention', 'kk-mention--gone')
      expect(chip.tagName).toBe('SPAN')
      expect(chip).toHaveAttribute('title', 'Bedřich — this account no longer exists')
    })

    it('leaves a mention in code, a bare @name and a user: link without the @ alone', () => {
      const { container } = renderEn(
        <Markdown mentions={mentions}>{'`@[Tomáš](user:us1)` @Tomáš [Tomáš](user:us1)'}</Markdown>,
      )

      expect(container.querySelector('.kk-mention')).toBeNull()
      expect(container.querySelector('code')).toHaveTextContent('@[Tomáš](user:us1)')
      // The sanitiser drops the unknown scheme, so the bare link is just text.
      expect(screen.queryByRole('link')).not.toBeInTheDocument()
      expect(container.innerHTML).not.toContain('user:us1"')
    })

    it('does not weaken the sanitiser: unsafe HTML and hrefs still die with mentions on', () => {
      const { container } = renderEn(
        <Markdown mentions={mentions}>
          {
            '<span class="kk-mention" onclick="x()">no</span> [k](javascript:alert(1)) <img src=x onerror=alert(1)>'
          }
        </Markdown>,
      )

      expect(container.querySelector('.kk-mention')).toBeNull()
      expect(container.querySelector('img')).toBeNull()
      expect(container.innerHTML).not.toContain('onclick')
      expect(container.innerHTML).not.toContain('javascript:')
    })

    it('leaves tokens to the sanitiser when no mentions are given', () => {
      const { container } = renderEn(<Markdown>{'@[Tomáš](user:us1)'}</Markdown>)

      expect(container.querySelector('.kk-mention')).toBeNull()
      expect(screen.getByText('Tomáš')).toBeInTheDocument()
    })
  })

  it('keeps a list that has content', () => {
    render(<Markdown>{'1987. Léto\n1988. Zima'}</Markdown>)

    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })
})
