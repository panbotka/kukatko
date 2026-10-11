import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The viewer's stars and its three review marks were the smallest targets in the
 * top bar: 22×22 and 26×26px with ~4px between them, beside a 44px heart (UI/UX
 * review 2026-10-11, finding #10). They share `kk-viewer__target`, which grows
 * the hit area — not the glyph — to 36px for a mouse and 44px for a finger, and
 * the bar spaces neighbouring targets at least 8px apart.
 *
 * jsdom evaluates no media queries and lays nothing out, so this guard reads the
 * stylesheet and pins the declarations that decide the outcome.
 */

const REM_PX = 16

/** Resolves the `2.25rem` / `36px` length forms these rules use. */
function lengthPx(value: string | undefined): number {
  const match = /^(-?[\d.]+)(rem|px)?$/.exec(value ?? '')
  if (match === null) {
    throw new Error(`unsupported length: ${String(value)}`)
  }
  return Number(match[1]) * (match[2] === 'rem' ? REM_PX : 1)
}

describe('the viewer’s rating and review-mark targets', () => {
  const css = readCss('src/components/photo/viewer.css')

  const rule = (source: string, prelude: RegExp): Map<string, string> => {
    const body = ruleBody(source, prelude)
    if (body === undefined) {
      throw new Error(`viewer.css declares no rule for ${prelude.source}`)
    }
    return declarations(body)
  }

  it('gives a mouse a 36px square, the glyph centred in it', () => {
    const target = rule(css, /\n\.kk-viewer__target\s*(?=\{)/)
    expect(lengthPx(target.get('min-width'))).toBeGreaterThanOrEqual(36)
    expect(lengthPx(target.get('min-height'))).toBeGreaterThanOrEqual(36)
    expect(target.get('align-items')).toBe('center')
    expect(target.get('justify-content')).toBe('center')
  })

  it('lifts it to the 44px finger floor on a coarse pointer', () => {
    const coarse = ruleBody(css, /@media\s*\(pointer:\s*coarse\)/, /\.kk-viewer__target/) ?? ''
    const target = rule(coarse, /\.kk-viewer__target\s*(?=\{)/)
    expect(lengthPx(target.get('min-width'))).toBeGreaterThanOrEqual(44)
    expect(lengthPx(target.get('min-height'))).toBeGreaterThanOrEqual(44)
  })

  it('sets neighbouring targets in the top bar at least 8px apart', () => {
    const spaced = rule(
      css,
      /\.kk-viewer__actions \.kk-viewer__stars,\s*\.kk-viewer__actions \.kk-viewer__marks\s*(?=\{)/,
    )
    // --kk-space-2 is the 8px step of the spacing scale (tokens.css).
    expect(spaced.get('gap')).toBe('var(--kk-space-2)')
  })

  it('marks the chosen review mark by shape too, never hiding the focus ring', () => {
    const pressed = rule(
      css,
      /\.kk-viewer__marks \.kk-viewer__target\[aria-pressed='true'\]:not\(:focus-visible\)\s*(?=\{)/,
    )
    expect(pressed.get('box-shadow')).toMatch(/^inset /)
  })

  it('leaves the phone dock’s star scale free to yield its width', () => {
    // The dock is one row only because the stars may shrink towards 24px; its
    // rule must stay more specific than the hit-area floor it overrides.
    const dockStar = rule(css, /\.kk-viewer__dock \.kk-viewer__stars \.btn\s*(?=\{)/)
    expect(lengthPx(dockStar.get('min-width'))).toBe(24)
  })
})
