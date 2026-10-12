import { fireEvent, render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { COMMENT_STRIP_MAX, CommentBody } from './CommentBody'

/** A uid of the catalogue's shape, numbered. */
function uid(n: number): string {
  return `ph${String(n).padStart(24, '0')}`
}

function renderBody(body: string, taskUid?: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <CommentBody body={body} taskUid={taskUid} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('CommentBody', () => {
  it('turns each uid token into a link and keeps the rest of the text as written', () => {
    renderBody(`These are wrong:\n${uid(1)}\n${uid(2)}, sorry.`)

    const body = document.querySelector('.kk-comment__body')
    expect(body).toHaveTextContent(`These are wrong: ${uid(1)} ${uid(2)}, sorry.`)
    const inline = within(body as HTMLElement).getAllByRole('link')
    expect(inline.map((a) => a.getAttribute('href'))).toEqual([
      `/photos/${uid(1)}`,
      `/photos/${uid(2)}`,
    ])
    expect(inline[0]).toHaveTextContent(uid(1))
  })

  it('carries the task into every link inside a task thread', () => {
    renderBody(`See ${uid(1)}`, 'tk1')

    const links = screen.getAllByRole('link')
    // The inline anchor and its thumbnail lead to the same place.
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(link).toHaveAttribute('href', `/photos/${uid(1)}?task=tk1`)
    }
  })

  it('renders Markdown: bold, inline code and a link', () => {
    renderBody('**tučně**, `kód` a [kalendář](https://example.com/cal)')

    expect(screen.getByText('tučně').tagName).toBe('STRONG')
    expect(screen.getByText('kód').tagName).toBe('CODE')
    const link = screen.getByRole('link', { name: 'kalendář' })
    expect(link).toHaveAttribute('href', 'https://example.com/cal')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('renders a mention from the comment’s resolved list, the reader’s own stronger', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <CommentBody
            body="@[Tom](user:us1) a @[Anna](user:us2)?"
            mentions={[
              { user_uid: 'us1', name: 'Tomáš Kozák', subject_uid: 'su1' },
              { user_uid: 'us2', name: 'Anna', subject_uid: null },
            ]}
            currentUserUid="us2"
          />
        </MemoryRouter>
      </I18nextProvider>,
    )

    expect(screen.getByRole('link', { name: 'Tomáš Kozák' })).toHaveAttribute('href', '/people/su1')
    expect(screen.getByText('Anna')).toHaveClass('kk-mention--me')
  })

  it('never interprets raw HTML in the body', () => {
    renderBody('<b>bold</b> <a href="/x">link</a> and <img src=x onerror=alert(1)>')

    expect(document.querySelector('b')).toBeNull()
    expect(document.querySelector('img')).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('shows a single newline as a line break', () => {
    renderBody('first line\nsecond line')

    expect(document.querySelector('.kk-comment__body br')).not.toBeNull()
  })

  it('links a bare uid and lists it in the strip', () => {
    renderBody(`Wrong one: ${uid(1)}.`)

    const body = document.querySelector('.kk-comment__body')
    if (!(body instanceof HTMLElement)) {
      throw new Error('the comment body is missing')
    }
    expect(within(body).getByRole('link', { name: uid(1) })).toHaveAttribute(
      'href',
      `/photos/${uid(1)}`,
    )
    const strip = screen.getByRole('list', { name: 'Photos mentioned in the comment' })
    expect(within(strip).getAllByRole('link')).toHaveLength(1)
  })

  it('lists a photo linked by its own-site URL in the strip, once', () => {
    const own = `${window.location.origin}/photos/${uid(1)}`
    renderBody(`[this one](${own}), again ${own} and /photos/${uid(1)} plus ${uid(1)}`, 'tk1')

    const strip = screen.getByRole('list', { name: 'Photos mentioned in the comment' })
    const thumbs = within(strip).getAllByRole('link')
    expect(thumbs).toHaveLength(1)
    expect(thumbs[0]).toHaveAttribute('href', `/photos/${uid(1)}?task=tk1`)
    // The written link stays in the app and carries the task.
    const written = screen.getByRole('link', { name: 'this one' })
    expect(written).toHaveAttribute('href', `/photos/${uid(1)}?task=tk1`)
    expect(written).not.toHaveAttribute('target')
  })

  it('leaves a uid inside inline code alone', () => {
    renderBody(`run \`kukatko ctl photos get ${uid(1)}\``)

    expect(screen.getByText(`kukatko ctl photos get ${uid(1)}`).tagName).toBe('CODE')
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('does not link a uid that is already the text of a link twice', () => {
    renderBody(`[${uid(1)}](https://example.com/${uid(1)})`)

    const links = screen.getAllByRole('link')
    expect(links).toHaveLength(1)
    expect(links[0]).toHaveAttribute('href', `https://example.com/${uid(1)}`)
    // Another site's URL is not a reference to one of our photos.
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('draws a thumbnail strip of the referenced photos, each once, capped with +N', () => {
    const many = Array.from({ length: COMMENT_STRIP_MAX + 3 }, (_, i) => uid(i + 1))
    // The first uid is mentioned twice; the strip still shows it once.
    renderBody(`${uid(1)} again ${many.join(' ')}`, 'tk1')

    const strip = screen.getByRole('list', { name: 'Photos mentioned in the comment' })
    const thumbs = within(strip).getAllByRole('link')
    expect(thumbs).toHaveLength(COMMENT_STRIP_MAX)
    expect(thumbs[0]).toHaveAttribute('href', `/photos/${uid(1)}?task=tk1`)
    expect(within(thumbs[0]).getByRole('presentation')).toHaveAttribute(
      'src',
      `/api/v1/photos/${uid(1)}/thumb/tile_100`,
    )
    expect(within(strip).getByText('+3')).toBeInTheDocument()
  })

  it('draws no strip for a comment that mentions no photo', () => {
    renderBody('1987.')

    expect(screen.queryByRole('list')).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('drops a thumbnail that fails to load instead of showing a broken image', () => {
    renderBody(`${uid(1)} ${uid(2)}`)

    const strip = screen.getByRole('list', { name: 'Photos mentioned in the comment' })
    const [first] = within(strip).getAllByRole('presentation')
    fireEvent.error(first)

    expect(within(strip).getAllByRole('link')).toHaveLength(1)
    // The inline link to the same photo stays: the reference is still valid text.
    expect(screen.getAllByRole('link', { name: uid(1) })).toHaveLength(1)
  })
})
