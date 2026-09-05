import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The viewer's header title is the photograph's primary label, and on a phone it
 * was losing the one part of it a reader came for. At 393 px the bar rendered
 * `7. 9. 201…`: the row's fixed-size button cluster left the heading a 96 px
 * content box, the whole line truncated from the right, and the year — which sits
 * at the END of the date in both of the app's locales — was the first thing an
 * ellipsis ate.
 *
 * jsdom computes no layout, no media queries and no container queries, so this
 * guard pins the declarations that decide the outcome rather than the pixels:
 *
 *  - the heading takes every pixel the cluster leaves, and may go narrower than
 *    its text asks for (`flex: 1 1 auto` + `min-width: 0`);
 *  - it is a query container, so the title measures itself against the room this
 *    reader's controls actually leave rather than against the viewport;
 *  - the date refuses to shrink while the place beside it yields;
 *  - and when the room runs out it is the CLOCK that is dropped, never the day.
 *
 * The rendered behaviour behind the numbers quoted here was measured in a
 * standalone harness over this very stylesheet, at 393 px, for an editor (four
 * controls) and a viewer (three).
 */
describe('viewer header title at phone width', () => {
  const css = readCss('src/components/photo/viewer.css')

  const rule = (prelude: RegExp, contains?: RegExp): Map<string, string> => {
    const body = ruleBody(css, prelude, contains)
    if (body === undefined) {
      throw new Error(`viewer.css declares no rule for ${prelude.source}`)
    }
    return declarations(body)
  }

  it('lets the heading grow into whatever the button cluster leaves', () => {
    const heading = rule(/\n\.kk-viewer__heading\s*(?=\{)/)
    expect(heading.get('flex')).toBe('1 1 auto')
    // Without this the heading cannot go below its text's width, and a flex line
    // that cannot shrink overflows the bar instead of ellipsising inside it.
    expect(heading.get('min-width')).toBe('0')
  })

  it('measures the title against the heading, not against the viewport', () => {
    // An editor's bar carries controls a viewer's does not, so the same 393 px
    // leaves the two headings very different widths. Only a container query can
    // tell them apart.
    const heading = rule(/\n\.kk-viewer__heading\s*(?=\{)/)
    expect(heading.get('container-type')).toBe('inline-size')
    expect(heading.get('container-name')).toBe('kk-viewer-heading')
  })

  it('lays the title out as parts so the date is not just the head of a run of text', () => {
    const title = rule(/\n\.kk-viewer__title\s*(?=\{)/)
    expect(title.get('display')).toBe('flex')
    expect(title.get('overflow')).toBe('hidden')
  })

  it('makes the date the part that never gives way', () => {
    const date = rule(/\n\.kk-viewer__title-date\s*(?=\{)/)
    expect(date.get('flex')).toBe('0 0 auto')
    // Capped at the line even so, so an impossible width ellipsises the date
    // rather than clipping the year off with nothing to show for it.
    expect(date.get('max-width')).toBe('100%')
  })

  it('makes the place the part that yields', () => {
    const muted = rule(
      /\.kk-viewer__title-text,\s+\.kk-viewer__title \.kk-viewer__title-muted\s*(?=\{)/,
    )
    expect(muted.get('flex')).toBe('0 1 auto')
    expect(muted.get('min-width')).toBe('0')
  })

  it('drops the clock, and only the clock, when the heading cannot hold the label whole', () => {
    const narrow = ruleBody(css, /@container kk-viewer-heading \(max-width: 13rem\)\s*(?=\{)/)
    expect(narrow).toBeDefined()
    // The date is not in this block: shortening the label must never be a way of
    // shortening the date, which is what the year was being lost to.
    expect(narrow).toContain('.kk-viewer__title-time')
    expect(narrow).not.toContain('.kk-viewer__title-date {')
    expect(declarations(narrow ?? '').size).toBe(0)
    expect(ruleBody(narrow ?? '', /\.kk-viewer__title-time\s*(?=\{)/)).toContain('display: none')
  })

  it('drops a place only when a date is left to name the photo', () => {
    const tightest = ruleBody(css, /@container kk-viewer-heading \(max-width: 9rem\)\s*(?=\{)/)
    expect(tightest).toBeDefined()
    // The sibling combinator is the guard: a photograph with a place and no date
    // has nothing else to be called, so that place must survive every width.
    expect(tightest).toContain('.kk-viewer__title-date + .kk-viewer__title-muted')
  })

  it('steps the title down a size on a phone, which is what makes the date fit', () => {
    const phone = ruleBody(
      css,
      /@media \(max-width: 767\.98px\)\s*(?=\{)/,
      /\n {2}\.kk-viewer__title \{/,
    )
    expect(phone).toBeDefined()
    const title = ruleBody(phone ?? '', /\n {2}\.kk-viewer__title\s*(?=\{)/)
    expect(declarations(title ?? '').get('font-size')).toBe('1.05rem')
  })

  it('keeps the desktop size on the bar itself, so a wide viewer is unchanged', () => {
    const title = rule(/\n\.kk-viewer__title\s*(?=\{)/)
    expect(title.get('font-size')).toBe('var(--kk-font-size-section-title, 1.05rem)')
  })
})
