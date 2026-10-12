import { describe, expect, it } from 'vitest'

import { markdownPhotoUids, photoUidFromHref, resolveMarkdownLink } from './markdownLinks'

const ORIGIN = 'https://fotky.example.cz'

/** A uid of the catalogue's shape, numbered. */
function uid(n: number): string {
  return `ph${String(n).padStart(24, '0')}`
}

describe('resolveMarkdownLink', () => {
  it('keeps an own-origin or root-relative link in the app', () => {
    expect(resolveMarkdownLink(`${ORIGIN}/tasks/tk1`, ORIGIN)).toEqual({
      kind: 'app',
      to: '/tasks/tk1',
    })
    expect(resolveMarkdownLink('/albums?q=x#top', ORIGIN)).toEqual({
      kind: 'app',
      to: '/albums?q=x#top',
    })
  })

  it('carries the task into a bare photo link only', () => {
    expect(resolveMarkdownLink(`${ORIGIN}/photos/${uid(1)}`, ORIGIN, 'tk1')).toEqual({
      kind: 'app',
      to: `/photos/${uid(1)}?task=tk1`,
    })
    expect(resolveMarkdownLink(`/photos/${uid(1)}?task=tk2`, ORIGIN, 'tk1')).toEqual({
      kind: 'app',
      to: `/photos/${uid(1)}?task=tk2`,
    })
    expect(resolveMarkdownLink(`/photos/${uid(1)}`, ORIGIN)).toEqual({
      kind: 'app',
      to: `/photos/${uid(1)}`,
    })
  })

  it('keeps the project a link names and carries the task into its photo link', () => {
    expect(resolveMarkdownLink(`${ORIGIN}/p/v/photos/${uid(1)}`, ORIGIN, 'tk1')).toEqual({
      kind: 'app',
      to: `/p/v/photos/${uid(1)}?task=tk1`,
    })
    expect(resolveMarkdownLink(`/p/v/photos/${uid(1)}#c2`, ORIGIN)).toEqual({
      kind: 'app',
      to: `/p/v/photos/${uid(1)}#c2`,
    })
    expect(resolveMarkdownLink(`${ORIGIN}/p/v/albums/al1?q=x`, ORIGIN)).toEqual({
      kind: 'app',
      to: '/p/v/albums/al1?q=x',
    })
  })

  it('sends another site to a new tab, scheme-relative included', () => {
    expect(resolveMarkdownLink('https://example.com/x', ORIGIN).kind).toBe('external')
    expect(resolveMarkdownLink('http://fotky.example.cz/x', ORIGIN).kind).toBe('external')
    expect(resolveMarkdownLink('//evil.example/x', ORIGIN).kind).toBe('page')
  })

  it('leaves server routes, in-page anchors and other schemes as plain anchors', () => {
    expect(resolveMarkdownLink(`${ORIGIN}/api/v1/photos/x/download`, ORIGIN)).toEqual({
      kind: 'page',
      href: '/api/v1/photos/x/download',
    })
    expect(resolveMarkdownLink('#user-content-fn-1', ORIGIN).kind).toBe('page')
    expect(resolveMarkdownLink('mailto:a@b.cz', ORIGIN).kind).toBe('page')
  })
})

describe('photoUidFromHref', () => {
  it('reads the uid of an own photo link and nothing else', () => {
    expect(photoUidFromHref(`${ORIGIN}/photos/${uid(1)}?task=tk1`, ORIGIN)).toBe(uid(1))
    expect(photoUidFromHref(`/photos/${uid(2)}`, ORIGIN)).toBe(uid(2))
    expect(photoUidFromHref(`https://example.com/photos/${uid(1)}`, ORIGIN)).toBeNull()
    expect(photoUidFromHref('/photos/not-a-uid', ORIGIN)).toBeNull()
    expect(photoUidFromHref(`/photos/${uid(1)}/edit`, ORIGIN)).toBeNull()
    expect(photoUidFromHref(`${ORIGIN}/p/v/photos/${uid(3)}`, ORIGIN)).toBe(uid(3))
    expect(photoUidFromHref(`/p/v/photos/${uid(4)}?task=tk1`, ORIGIN)).toBe(uid(4))
    expect(photoUidFromHref(`/p/v/x/photos/${uid(4)}`, ORIGIN)).toBeNull()
  })
})

describe('markdownPhotoUids', () => {
  it('lists bare uids and own photo links once, in order of first mention', () => {
    const body = [
      `${uid(2)} first`,
      `| fotka |\n| --- |\n| [x](${ORIGIN}/photos/${uid(1)}) |`,
      `${ORIGIN}/photos/${uid(3)} and ${uid(2)} again`,
      `[ref][r]\n\n[r]: /photos/${uid(4)}`,
    ].join('\n\n')

    expect(markdownPhotoUids(body, ORIGIN)).toEqual([uid(2), uid(1), uid(3), uid(4)])
  })

  it('ignores uids inside code and on other sites', () => {
    const body = `\`${uid(1)}\`\n\n\`\`\`\n${uid(2)}\n\`\`\`\n\nhttps://example.com/photos/${uid(3)}`

    expect(markdownPhotoUids(body, ORIGIN)).toEqual([])
  })
})
