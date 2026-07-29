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

  it('squares the navbar brand off to the floor on touch', () => {
    // The brand carries its own coarse block (next to the rules that shape it),
    // so pick the one that actually mentions it rather than the shared floor.
    const block = ruleBody(css, /@media\s*\(pointer:\s*coarse\)/, /\.kukatko-navbar-brand/) ?? ''
    const brand = declarations(ruleBody(block, /\.kukatko-navbar-brand\s*(?=\{)/) ?? '')
    // Below `sm` the wordmark is hidden and the mark is the whole control, so the
    // box has to clear the floor on both axes — not just vertically.
    expect(lengthPx(brand.get('min-width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    expect(lengthPx(brand.get('min-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)

    // The base rule keeps the flex centering that holds the glyph in the middle
    // once the box grows past it.
    const base = declarations(ruleBody(css, /\.kukatko-navbar-brand\s*(?=\{)/) ?? '')
    expect(base.get('display')).toBe('inline-flex')
    expect(base.get('align-items')).toBe('center')
    expect(base.get('justify-content')).toBe('center')
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

  it('squares the header overflow toggle off to the floor on touch', () => {
    // `HeaderActions`' "…" toggle is a phone-only control and a bare glyph, so
    // the shared `min-height` alone would leave it ~34px wide. It carries its
    // own coarse block next to the rules that shape it.
    const block = ruleBody(css, /@media\s*\(pointer:\s*coarse\)/, /\.kk-header-overflow-toggle/)
    const toggle = declarations(ruleBody(block ?? '', /\.kk-header-overflow-toggle\s*(?=\{)/) ?? '')
    expect(lengthPx(toggle.get('min-width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
  })

  it('gives a small face marker a 44px hit box without growing its outline', () => {
    // A `FaceOverlay` box is sized to the face's bbox and is not a `.btn`, so
    // nothing in the list above reaches it — and sizing the box itself would
    // slide the outline off the face. The hit area is a pseudo-element instead.
    const hit = declarations(ruleBody(coarse, /\.kk-face-box::after\s*(?=\{)/) ?? '')
    expect(lengthPx(hit.get('width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    expect(lengthPx(hit.get('height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    // Only the small markers grow: `100%` of the button is the floor, so a face
    // already bigger than 44px keeps its own (larger) target.
    expect(hit.get('min-width')).toBe('100%')
    expect(hit.get('min-height')).toBe('100%')
    // Centred on the marker, so it grows symmetrically around the face.
    expect(hit.get('position')).toBe('absolute')
    expect(hit.get('top')).toBe('50%')
    expect(hit.get('left')).toBe('50%')
    expect(hit.get('transform')).toBe('translate(-50%, -50%)')
    // Nothing is painted — the visible rectangle is still the bbox alone.
    for (const painted of ['background', 'background-color', 'border', 'outline', 'box-shadow']) {
      expect(hit.has(painted)).toBe(false)
    }
  })

  it('leaves the face marker untouched on a fine pointer', () => {
    // The hit box exists only inside the coarse block: on a mouse the target is
    // the drawn bbox, exactly as before.
    expect(ruleBody(css, /\.kk-face-box(?!::)/)).toBeUndefined()
  })

  it('exempts the close button inside a pill chip', () => {
    const chip = declarations(ruleBody(coarse, /\.badge\s+\.btn-close\s*(?=\{)/) ?? '')
    expect(lengthPx(chip.get('min-width'))).toBe(0)
    expect(lengthPx(chip.get('min-height'))).toBe(0)
    expect(chip.get('box-sizing')).toBe('content-box')
  })
})

/**
 * Leaflet ships its own control sizing (26px, 30px with `leaflet-touch`) and no
 * theme hooks, so the map's buttons sit far below the floor the rest of the app
 * holds to unless `app.css` reaches in by Leaflet's class names. jsdom loads no
 * Leaflet stylesheet and evaluates no media query, so read the rules instead.
 */
describe('map controls on a coarse pointer', () => {
  const css = readCss('src/styles/app.css')
  const coarse = ruleBody(css, /@media\s*\(pointer:\s*coarse\)/, /\.leaflet-bar/) ?? ''

  it('lifts the Leaflet toolbar buttons to the floor', () => {
    const bar = declarations(ruleBody(coarse, /\.kukatko-map \.leaflet-bar a,/) ?? '')
    expect(lengthPx(bar.get('width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    expect(lengthPx(bar.get('height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    // The glyph is centred by `line-height`, which has to grow with the box.
    expect(lengthPx(bar.get('line-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
  })

  it('out-specifies Leaflet, which the bundler emits after app.css', () => {
    // `.leaflet-touch .leaflet-bar a` (0,2,1) ties with a single-class override
    // and wins on order, so the rule has to carry `.kukatko-map` on the
    // container itself as well.
    expect(coarse).toContain('.kukatko-map.leaflet-touch .leaflet-bar a')
    expect(coarse).toContain('.kukatko-map.leaflet-touch .leaflet-control-zoom-in')
  })

  it('gives the mandatory mapy.com logo a real hit area', () => {
    const logo = declarations(ruleBody(coarse, /\.kukatko-map \.kukatko-map-logo\s*(?=\{)/) ?? '')
    expect(lengthPx(logo.get('min-width'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
    expect(lengthPx(logo.get('min-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
  })
})

/**
 * The map's height is asked for in `dvh` inline, which an engine without the
 * unit drops — leaving the map with no height at all. The stylesheet's `vh`
 * equivalent is what catches that, so it must stay in step with the component.
 */
describe('map sizing fallback', () => {
  it('backs the inline dvh height with the same length in vh', () => {
    const css = readCss('src/styles/app.css')
    // The lookahead picks the bare `.kukatko-map` rule, not the descendant ones.
    const map = declarations(ruleBody(css, /\.kukatko-map\s+(?=\{)/) ?? '')
    expect(map.get('height')).toBe('70vh')
    // Absolute positioning of the gesture hint needs a positioned container from
    // the first paint, before Leaflet sets it itself.
    expect(map.get('position')).toBe('relative')
  })

  it('keeps a draggable picker pin answering a one-finger drag', () => {
    const css = readCss('src/styles/app.css')
    const pin = declarations(ruleBody(css, /\.kukatko-map \.leaflet-marker-draggable/) ?? '')
    expect(pin.get('touch-action')).toBe('none')
  })
})
