import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

const REM_PX = 16

/** Resolves a `--kk-*` custom property declared on `:root` to pixels. */
function tokenPx(css: string, name: string): number {
  const declared = new RegExp(`${name}:\\s*([^;]+);`).exec(css)
  if (declared === null) {
    throw new Error(`token ${name} is not declared`)
  }
  return lengthPx(css, declared[1].trim())
}

/**
 * Resolves the handful of length forms these rules use — `1.65rem`, `-0.85rem`,
 * `12px` and `calc(-1 * var(--token))` — to pixels. Anything else throws rather
 * than being silently treated as zero, so a rewrite in another form fails loudly
 * instead of quietly passing the size assertions below.
 */
function lengthPx(css: string, value: string): number {
  const negatedToken = /^calc\(\s*-1\s*\*\s*var\((--[\w-]+)\)\s*\)$/.exec(value)
  if (negatedToken !== null) {
    return -tokenPx(css, negatedToken[1])
  }
  const token = /^var\((--[\w-]+)\)$/.exec(value)
  if (token !== null) {
    return tokenPx(css, token[1])
  }
  const absolute = /^(-?[\d.]+)(rem|px)$/.exec(value)
  if (absolute !== null) {
    return Number(absolute[1]) * (absolute[2] === 'rem' ? REM_PX : 1)
  }
  throw new Error(`unsupported length: ${value}`)
}

/**
 * The grid's multi-select entry point is the per-tile corner checkmark, and on a
 * touch screen it is the *only* one: the library grid runs in hover-select mode
 * with no "Select" button at all, and in the explicit selection mode the check is
 * the sole hint that the grid just became a selection surface. A hover-only reveal
 * therefore made multi-select unreachable on a phone. These assertions pin both
 * halves of the fix — visible at rest on coarse pointers, still hover-revealed on
 * fine ones — plus the finger-sized hit area, since none of it can be observed
 * from jsdom (it evaluates no media queries).
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

  it('grows the hit area to the 44px touch-target floor', () => {
    const hit = declarations(ruleBody(touch ?? '', /\.kk-tile__check::before\s*/) ?? '')
    expect(hit.get('content')).toBe("''")
    expect(hit.get('position')).toBe('absolute')

    const size = lengthPx(css, base.get('width') ?? '0px')
    expect(size).toBe(lengthPx(css, base.get('height') ?? '0px'))
    const grow = (a: string, b: string): number =>
      size - lengthPx(css, hit.get(a) ?? '0px') - lengthPx(css, hit.get(b) ?? '0px')
    expect(grow('left', 'right')).toBeGreaterThanOrEqual(44)
    expect(grow('top', 'bottom')).toBeGreaterThanOrEqual(44)
  })

  it('keeps the hit area inside its own tile so it cannot steal a neighbour tap', () => {
    // The disc sits `top`/`left` in from the tile's corner; the invisible box may
    // reach that corner but no further, or it would overhang the grid gutter and
    // swallow taps meant for the tile next to it.
    const hit = declarations(ruleBody(touch ?? '', /\.kk-tile__check::before\s*/) ?? '')
    for (const side of ['top', 'left']) {
      const overhang = -lengthPx(css, hit.get(side) ?? '0px')
      expect(overhang).toBeLessThanOrEqual(lengthPx(css, base.get(side) ?? '0px'))
    }
  })
})
