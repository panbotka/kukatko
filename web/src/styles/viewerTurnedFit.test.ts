import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * A draft rotation by a quarter turn is the one case where CSS turns the
 * picture (a saved rotation is baked into the rendition), and a turned picture
 * needs a turned box: laid out unturned, it fitted the stage by its own width
 * and, once rotated, overflowed the viewport by 108px top and bottom on a 900px
 * screen. jsdom lays nothing out, so this guard pins the declarations that make
 * the fit: the stage is the query container the turned figure measures against,
 * and the figure and its image are sized in container units from the turned
 * ratio the page stamps inline.
 */
describe('viewer turned-preview fit', () => {
  const css = readCss('src/components/photo/viewer.css')

  const rule = (prelude: RegExp): Map<string, string> => {
    const body = ruleBody(css, prelude)
    if (body === undefined) {
      throw new Error(`viewer.css declares no rule for ${prelude.source}`)
    }
    return declarations(body)
  }

  it('makes the stage the size container the turned figure measures against', () => {
    expect(rule(/\.kk-viewer__stage\s*(?=\{)/).get('container-type')).toBe('size')
  })

  it('fits the turned figure to the stage in container units at the turned ratio', () => {
    const figure = rule(/\.kk-viewer__figure\[data-turned='true'\]\s*(?=\{)/)
    expect(figure.get('width')).toBe('min(100cqw, calc(100cqh * var(--kk-turn-ratio)))')
  })

  it('lays the image out unturned, centred, at the figure’s transposed size', () => {
    const image = rule(/\.kk-viewer__figure\[data-turned='true'\] > \.kk-viewer__image\s*(?=\{)/)
    expect(image.get('position')).toBe('absolute')
    expect(image.get('margin')).toBe('auto')
    // Its width is the figure's height and its height the figure's width, so the
    // rotate() in its transform lands it on the figure exactly.
    expect(image.get('width')).toBe('min(calc(100cqw / var(--kk-turn-ratio)), 100cqh)')
    expect(image.get('height')).toBe('min(100cqw, calc(100cqh * var(--kk-turn-ratio)))')
    // The base rule's max-width/max-height: 100% would clamp the transposed
    // width to the (narrower) figure.
    expect(image.get('max-width')).toBe('none')
    expect(image.get('max-height')).toBe('none')
  })
})
