import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * A dialog opened from a full-screen sheet has to paint over it. Bootstrap ships
 * its dialog band at 1040–1055 while the immersive viewer owns its route at
 * `--kk-viewer-z` (1080), so the comment-delete confirm opened in the viewer
 * rendered *under* the photograph: in the DOM, `.modal.show`, and every click
 * on its buttons landed on the image. The tests of the flow stayed green, because
 * a scripted `.click()` does not hit-test.
 *
 * jsdom computes no paint order, so this guard pins the numbers that decide it,
 * read out of the shipped stylesheets: each layer of the band — the backdrops as
 * much as the dialogs, or the sheet still wins the hit test around them — stands
 * above the viewer and every other full-screen sheet, in Bootstrap's own order,
 * and under the toasts so a toast raised from a dialog still lands on top.
 */
describe('dialog band', () => {
  const app = readCss('src/styles/app.css')
  const tokens = readCss('src/styles/tokens.css')

  /** Resolves a `var(--kk-…)` reference against the custom properties the two
   *  global sheets declare on `:root` (the layering scale in `app.css`, the
   *  viewer's own layer in `tokens.css`). */
  const resolve = (value: string): number => {
    const token = /^var\((--[\w-]+)\)$/.exec(value)
    if (token === null) {
      return Number(value)
    }
    const declaring = new RegExp(`${token[1]}\\s*:`)
    for (const [css, prelude] of [
      [app, /:root\s*(?=\{)/],
      [tokens, /:root\s*(?=\{)/],
    ] as const) {
      const body = ruleBody(css, prelude, declaring)
      const declared = body === undefined ? undefined : declarations(body).get(token[1])
      if (declared !== undefined) {
        return resolve(declared)
      }
    }
    throw new Error(`no declaration for ${token[1]}`)
  }

  /** The resolved value `property` takes in the rule of `css` matching `prelude`. */
  const layer = (css: string, prelude: RegExp, property: string): number => {
    const body = ruleBody(css, prelude)
    const declared = body === undefined ? undefined : declarations(body).get(property)
    if (declared === undefined) {
      throw new Error(`no ${property} in a rule for ${prelude.source}`)
    }
    const z = resolve(declared)
    expect(Number.isFinite(z)).toBe(true)
    return z
  }

  const band = {
    offcanvasBackdrop: layer(app, /\.offcanvas-backdrop\s*(?=\{)/, 'z-index'),
    offcanvas: layer(app, /\.offcanvas,\s*\.offcanvas-sm,[^{]*(?=\{)/, '--bs-offcanvas-zindex'),
    modalBackdrop: layer(app, /\.modal-backdrop\s*(?=\{)/, '--bs-backdrop-zindex'),
    modal: layer(app, /(?:^|\n)\.modal\s*(?=\{)/, '--bs-modal-zindex'),
  }

  const sheets = {
    viewer: layer(readCss('src/components/photo/viewer.css'), /\.kk-viewer\s*(?=\{)/, 'z-index'),
    reviewGame: layer(
      readCss('src/components/review/review.css'),
      /\.review-game\s*(?=\{)/,
      'z-index',
    ),
    albumFaces: layer(
      readCss('src/components/people/albumFaces.css'),
      /\.kk-album-faces\s*(?=\{)/,
      'z-index',
    ),
    slideshow: layer(
      readCss('src/components/slideshow/slideshow.css'),
      /\.slideshow\s*(?=\{)/,
      'z-index',
    ),
  }

  it.each(Object.entries(band))('lifts the %s above the immersive viewer', (_, z) => {
    expect(z).toBeGreaterThan(sheets.viewer)
  })

  it.each(Object.entries(sheets))('keeps the lowest dialog layer above the %s', (_, z) => {
    expect(Math.min(...Object.values(band))).toBeGreaterThan(z)
  })

  it("keeps Bootstrap's order inside the band", () => {
    expect(band.offcanvasBackdrop).toBeLessThan(band.offcanvas)
    expect(band.offcanvas).toBeLessThan(band.modalBackdrop)
    expect(band.modalBackdrop).toBeLessThan(band.modal)
  })

  it('stays under the toast stack, so a toast raised from a dialog lands on top', () => {
    expect(band.modal).toBeLessThan(layer(tokens, /\.kk-toast-stack\s*(?=\{)/, 'z-index'))
  })
})
