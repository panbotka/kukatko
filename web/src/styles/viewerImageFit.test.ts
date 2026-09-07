import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The viewer's image must land on its own figure, and in WebKit it did not.
 *
 * JSX states the photograph's frame as `width`/`height` ATTRIBUTES on the image,
 * so the box does not collapse while the file is on the wire. An attribute is a
 * presentational hint — a real used height in pixels — and only `max-height: 100%`
 * cut it back to the figure. That percentage resolves against the figure's height,
 * which comes from `aspect-ratio` alone; WebKit calls such a height indefinite and
 * drops the percentage, so the image kept the attribute's height. Measured in
 * WebKit at a 402x713 phone viewport: figure 386x290 (correct), image 386x960, and
 * `object-fit: contain` centred the photograph in that 960px box — painting it at
 * y=547 instead of y=212, i.e. 335px below its own figure, off the bottom of the
 * screen, with the face overlay left behind on the figure. Blink resolved the same
 * percentage and showed nothing.
 *
 * jsdom lays nothing out, so this guard pins the declaration that fixes it: the
 * image scales by its ratio from its width, with or without a max-height to lean
 * on — and the two rules that deliberately size an image themselves keep saying so.
 */
describe('viewer image fit', () => {
  const css = readCss('src/components/photo/viewer.css')

  const rule = (prelude: RegExp): Map<string, string> => {
    const body = ruleBody(css, prelude)
    if (body === undefined) {
      throw new Error(`viewer.css declares no rule for ${prelude.source}`)
    }
    return declarations(body)
  }

  it('lets the image scale by its ratio rather than by its height attribute', () => {
    // Anchored, and global so the helper keeps the `m` flag: the file names
    // `.kk-viewer__image` in three preludes and only this one starts a line.
    const image = rule(/^\.kk-viewer__image\s*(?=\{)/gm)
    expect(image.get('height')).toBe('auto')
    // The cap stays: it is what keeps a tall photograph inside a short stage.
    expect(image.get('max-width')).toBe('100%')
    expect(image.get('max-height')).toBe('100%')
    expect(image.get('object-fit')).toBe('contain')
  })

  it('leaves the stand-in filling the framed figure exactly', () => {
    // It shares the base class, so `height: auto` would otherwise start scaling
    // the smaller rendition by its own ratio instead of stretching it over the
    // box the full-size image will land in.
    const under = rule(/\.kk-viewer__figure > \.kk-viewer__image--under\s*(?=\{)/)
    expect(under.get('width')).toBe('100%')
    expect(under.get('height')).toBe('100%')
  })
})
