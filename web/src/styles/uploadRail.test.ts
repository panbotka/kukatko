import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody, zIndexOf } from '../test/css'

/**
 * The upload flow's action rail is the one control surface every stage ends in:
 * it carries the stage's primary action *and* the batch progress, and the whole
 * redesign rests on it never scrolling out of reach. jsdom evaluates no media
 * queries and loads no Bootstrap, so what that promise is worth is read out of
 * the shipped stylesheet.
 *
 * Two things can silently break it. Pinning it with `fixed` instead of `sticky`
 * would make it float over content nobody reserved clearance for; and forgetting
 * `--kk-bottom-edge` in the offset would park it underneath the mobile tab bar,
 * which is precisely the "the button is somewhere I cannot reach" complaint the
 * rail was built to answer.
 */
describe('upload action rail', () => {
  const css = readCss('src/styles/app.css')
  const rail = declarations(ruleBody(css, /\.kk-upload-rail\s*(?=\{)/) ?? '')

  it('sticks to the bottom edge instead of floating over the page', () => {
    expect(rail.get('position')).toBe('sticky')
    expect(rail.get('bottom')).toBeDefined()
  })

  it('clears whatever already owns the bottom edge — the tab bar or the home bar', () => {
    // `--kk-bottom-edge` is the app's one answer to "what is down there": the
    // mobile tab bar's live height, or the bare safe-area inset without one.
    expect(rail.get('bottom')).toContain('var(--kk-bottom-edge)')
  })

  it('takes the in-page sticky layer: under the tab bar, under the picker it opens', () => {
    const layer = zIndexOf(css, /\.kk-upload-rail\s*(?=\{)/)
    // It stops short of the tab bar rather than covering it — the rail clears
    // the tabs geometrically, so stacking over them would only hide navigation.
    expect(layer).toBeLessThan(zIndexOf(css, /\.kk-tabbar\s*(?=\{)/))
    // And the album picker sitting above it opens a `MultiSelect` overlay that
    // has to paint over the rail, not disappear behind it.
    expect(layer).toBeLessThan(zIndexOf(css, /\.kk-overlay-menu\s*(?=\{)/))
  })

  it('stacks its actions on a phone and lines them up once there is room', () => {
    const stacked = declarations(ruleBody(css, /\.kk-upload-rail__actions\s*(?=\{)/) ?? '')
    expect(stacked.get('flex-direction')).toBe('column')

    // The same DOM, laid out along the other axis from `sm` up — no separate
    // mobile markup, which is the rule this whole page is built on.
    const wide = ruleBody(css, /@media\s*\(min-width:\s*576px\)/, /\.kk-upload-rail__actions/) ?? ''
    const row = declarations(ruleBody(wide, /\.kk-upload-rail__actions\s*(?=\{)/) ?? '')
    expect(row.get('flex-direction')).toBe('row')
    expect(row.get('justify-content')).toBe('flex-end')
  })
})
