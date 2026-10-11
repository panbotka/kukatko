import { describe, expect, it } from 'vitest'

import { rootCustomProperties, stripCssComments } from '../test/colour'
import { declarations, readCss, ruleBody } from '../test/css'

/**
 * A disabled button must read as "unavailable", not as a second style. Every
 * variant keeps its own fill and label when disabled (`--bs-btn-disabled-*`), so
 * the only thing that says "off" is the fade — and at Superhero's 0.65 a disabled
 * filled primary was a dark-orange plate with a full-contrast label, a second
 * brand colour next to the live one (UI/UX review 2026-10-11, finding #1). These
 * guards read the shipped stylesheets for the one rule that dims every disabled
 * button, native or `.disabled`, and the `aria-disabled` twin `ReasonedButton`
 * wears, since jsdom computes none of it.
 */

const bridge = stripCssComments(readCss('src/styles/bootstrapBridge.css'))
const app = stripCssComments(readCss('src/styles/app.css'))
const tokens = rootCustomProperties()

const DISABLED = /\.btn:disabled,\s*\.btn\.disabled,\s*fieldset:disabled \.btn\s*(?=\{)/
const DISABLED_HOVER = /\.btn:disabled:hover,[^{]*fieldset:disabled \.btn:active\s*(?=\{)/

/** The filled variants, by the prelude that opens each one's own `--bs-btn-*` rule. */
const FILLED = [
  ['primary', /\.btn-primary\s*(?=\{)/],
  ['secondary + info', /\.btn-secondary,\s*\.btn-info\s*(?=\{)/],
  ['success', /\.btn-success\s*(?=\{)/],
  ['warning', /\.btn-warning\s*(?=\{)/],
  ['danger', /\.btn-danger\s*(?=\{)/],
  ['light', /\.btn-light\s*(?=\{)/],
  ['dark', /\.btn-dark\s*(?=\{)/],
] as const

describe('disabled buttons', () => {
  it('fade to about half strength, further than a disabled checkbox', () => {
    const opacity = Number(tokens.get('--kk-btn-disabled-opacity'))
    expect(opacity).toBeCloseTo(0.5, 1)
    // A checkbox keeps more of itself because its edge is measured for 3:1; a
    // button's label is inside the fade and a disabled control is exempt.
    expect(opacity).toBeLessThan(Number(tokens.get('--kk-control-disabled-opacity')))
  })

  it('hand Bootstrap that opacity through `.btn`, so every variant fades alike', () => {
    const btn = declarations(ruleBody(bridge, /(^|\})\s*\.btn\s*(?=\{)/) ?? '')
    expect(btn.get('--bs-btn-disabled-opacity')).toBe('var(--kk-btn-disabled-opacity)')
  })

  it('keep the variant hue: a disabled fill is the resting fill, only dimmed', () => {
    for (const [name, prelude] of FILLED) {
      const decls = declarations(ruleBody(bridge, prelude) ?? '')
      expect(decls.get('--bs-btn-disabled-bg'), name).toBe(decls.get('--bs-btn-bg'))
      expect(decls.get('--bs-btn-disabled-color'), name).toBe(decls.get('--bs-btn-color'))
    }
  })

  it('show the not-allowed cursor and lose the raised inset', () => {
    const body = ruleBody(bridge, DISABLED)
    expect(body).toBeDefined()
    const decls = declarations(body ?? '')
    expect(decls.get('cursor')).toBe('not-allowed')
    // Bootstrap's `pointer-events: none` would hide that cursor with the hover.
    expect(decls.get('pointer-events')).toBe('auto')
    expect(decls.get('box-shadow')).toBe('none')
  })

  it('never change colour under the pointer', () => {
    const body = ruleBody(bridge, DISABLED_HOVER)
    expect(body).toBeDefined()
    const decls = declarations(body ?? '')
    expect(decls.get('color')).toBe('var(--bs-btn-disabled-color)')
    expect(decls.get('background-color')).toBe('var(--bs-btn-disabled-bg)')
    expect(decls.get('border-color')).toBe('var(--bs-btn-disabled-border-color)')
    expect(decls.get('box-shadow')).toBe('none')
  })

  it('look the same whether the button is `disabled` or a `ReasonedButton`', () => {
    const inert = declarations(ruleBody(app, /\.kk-btn-inert\s*(?=\{)/) ?? '')
    const native = declarations(ruleBody(bridge, DISABLED) ?? '')
    // The twin resolves the same custom property, so the two fades cannot drift.
    expect(inert.get('opacity')).toBe('var(--bs-btn-disabled-opacity)')
    expect(inert.get('cursor')).toBe(native.get('cursor'))
    expect(inert.get('box-shadow')).toBe(native.get('box-shadow'))
  })
})
