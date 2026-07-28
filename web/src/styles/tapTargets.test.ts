import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The app-wide touch-target floor in `app.css` enumerates the controls it lifts to
 * the 44px finger minimum, which means anything left out of that list silently
 * stays finger-hostile — and the two it used to miss were the shell's most-used
 * controls: the navbar hamburger and every dismiss `X`.
 *
 * jsdom evaluates no media queries and loads no Bootstrap, so these guards read
 * the stylesheet and assert the rules themselves. What they pin: both controls
 * clear the floor on a coarse pointer, the glyph inside a close button is not
 * grown along with its box (the whole point is a bigger *hit area*, not a bigger
 * `X`), and the chips keep their compact close button, which a 44px minimum would
 * burst out of the pill it sits in.
 */

const REM_PX = 16
/** The finger-friendly minimum a touch target has to clear. */
const TOUCH_FLOOR_PX = 44

/** Resolves the `2.75rem` / `44px` / `0` length forms these rules use. */
function lengthPx(value: string | undefined): number {
  if (value === undefined) {
    throw new Error('length is not declared')
  }
  const match = /^(-?[\d.]+)(rem|px)?$/.exec(value)
  if (match === null) {
    throw new Error(`unsupported length: ${value}`)
  }
  return Number(match[1]) * (match[2] === 'rem' ? REM_PX : 1)
}

describe('coarse-pointer touch-target floor', () => {
  const css = readCss('src/styles/app.css')
  // `app.css` carries more than one `pointer: coarse` block; the floor is the one
  // that sizes the close buttons.
  const coarse = ruleBody(css, /@media\s*\(pointer:\s*coarse\)/, /\.btn-close/) ?? ''

  it('lifts the navbar hamburger to the floor', () => {
    // The shared min-height list — matched by the comma, which the standalone
    // `.navbar-toggler` rule below does not carry.
    const shared = declarations(ruleBody(coarse, /\.navbar-toggler,/) ?? '')
    expect(lengthPx(shared.get('min-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)

    const toggler = declarations(ruleBody(coarse, /\.navbar-toggler\s*(?=\{)/) ?? '')
    expect(lengthPx(toggler.get('min-width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    // Without flex centering the icon rides the text baseline once the box grows.
    expect(toggler.get('display')).toBe('inline-flex')
    expect(toggler.get('align-items')).toBe('center')
    expect(toggler.get('justify-content')).toBe('center')
  })

  it('gives every close button a 44px hit area without growing its glyph', () => {
    // The lookbehind skips `.badge .btn-close` (a descendant combinator leaves a
    // non-space then a space before the class) so this is the bare rule.
    const close = declarations(ruleBody(coarse, /(?<!\S\s)\.btn-close\s*(?=\{)/) ?? '')
    expect(lengthPx(close.get('min-width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    expect(lengthPx(close.get('min-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    // `border-box` keeps the minimum the whole button, so the padding a modal
    // header or an alert adds does not bulk it past the floor.
    expect(close.get('box-sizing')).toBe('border-box')
    // The visible `X` is a `1em` background image: leave the font size and the
    // box's own width/height alone and it stays exactly as small as it looks.
    for (const grown of ['font-size', 'width', 'height', 'background-size']) {
      expect(close.has(grown)).toBe(false)
    }
  })

  it('exempts the close button inside a pill chip', () => {
    const chip = declarations(ruleBody(coarse, /\.badge\s+\.btn-close\s*(?=\{)/) ?? '')
    expect(lengthPx(chip.get('min-width'))).toBe(0)
    expect(lengthPx(chip.get('min-height'))).toBe(0)
    expect(chip.get('box-sizing')).toBe('content-box')
  })
})
