import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The grid's multi-select entry point is the per-tile corner checkmark, and on a
 * touch screen it is the *only* one: the library grid runs in hover-select mode
 * with no "Select" button at all, and in the explicit selection mode the check is
 * the sole hint that the grid just became a selection surface. A hover-only reveal
 * therefore made multi-select unreachable on a phone. These assertions pin both
 * halves of the fix — visible at rest on coarse pointers, still hover-revealed on
 * fine ones — neither of which can be observed from jsdom, which evaluates no
 * media queries. The finger-sized hit area the control carries on touch is
 * pinned with the app's other tap targets, in `styles/tapTargets.test.ts`.
 */
describe('tile selection checkmark on touch', () => {
  const css = readCss('src/styles/tokens.css')
  const base = declarations(ruleBody(css, /\.kk-tile__check\s*(?=\{)/) ?? '')
  // Both conditions, in either order: `hover: none` catches a touch screen, and
  // `pointer: coarse` also catches a hybrid device driven by a finger.
  const touch = ruleBody(
    css,
    /@media(?=[^{]*\(hover:\s*none\))(?=[^{]*\(pointer:\s*coarse\))[^{]*/,
    /\.kk-tile__check/,
  )

  it('hides the checkmark at rest so fine pointers keep the hover reveal', () => {
    expect(base.get('opacity')).toBe('0')
  })

  it('pins the checkmark visible on coarse pointers', () => {
    expect(touch).toBeDefined()
    const shown = declarations(ruleBody(touch ?? '', /\.kk-tile__check\s*(?=\{)/) ?? '')
    expect(shown.get('opacity')).toBe('1')
  })

  it('leaves the photo wall bare at rest, where a long press is the entry point', () => {
    // Every other grid keeps the pinned control (it has no gesture of its own);
    // the wall would otherwise carry a disc over every photograph for good.
    const wall = declarations(
      ruleBody(touch ?? '', /\.kukatko-photo-grid \.kk-tile__check\s*(?=\{)/) ?? '',
    )
    expect(wall.get('opacity')).toBe('0')
  })

  it('brings the wall’s checkmark back the moment the grid is selecting', () => {
    // The rule has to come after the one that hides it — same specificity — and
    // cover all three ways a tile is part of a live selection.
    const hidden = (touch ?? '').indexOf('.kukatko-photo-grid .kk-tile__check {')
    const shown = (touch ?? '').indexOf('.kukatko-photo-grid .kk-tile--selecting')
    expect(hidden).toBeGreaterThan(-1)
    expect(shown).toBeGreaterThan(hidden)

    const selecting = declarations(
      ruleBody(touch ?? '', /\.kukatko-photo-grid \.kk-tile--selecting[^{]*/) ?? '',
    )
    expect(selecting.get('opacity')).toBe('1')
    for (const state of ['.kk-tile--checks .kk-tile__check', '.kk-tile__check--on']) {
      expect(touch).toContain(`.kukatko-photo-grid ${state}`)
    }
  })

  // The size, centring and pointer-events of the invisible hit box that carries
  // this control's finger target live with the app's other tap targets, in
  // `styles/tapTargets.test.ts`.
})
