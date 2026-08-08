import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The review game and the duplicate-compare view are fullscreen overlays mounted
 * outside the layout shell, so the shell's safe-area padding never reaches them —
 * and `web/index.html` opts into `viewport-fit=cover`, which makes the insets real.
 * Without their own `env(safe-area-inset-*)` terms the rows that carry the whole
 * interaction (Yes / No / Don't know; Keep left / both / right; close and back)
 * sit under the notch or the home-indicator bar.
 *
 * jsdom evaluates neither `env()` nor media queries, so these guards read the
 * stylesheets and resolve the paddings themselves against an iPhone-class safe
 * area. What they pin: every edge row clears its inset, and — just as important —
 * the desktop spacing is unchanged, because the insets are *added* to the existing
 * padding rather than replacing it.
 */

const SIDES = ['top', 'right', 'bottom', 'left'] as const
type Side = (typeof SIDES)[number]
type Insets = Record<Side, number>

/** No notch, no home indicator: every `env(safe-area-inset-*)` resolves to 0. */
const DESKTOP: Insets = { top: 0, right: 0, bottom: 0, left: 0 }
/** An iPhone-class portrait screen: notch above, home-indicator bar below. */
const PORTRAIT: Insets = { top: 47, right: 0, bottom: 34, left: 0 }
/** The same phone on its side: the notch takes a column, the home bar is slimmer. */
const LANDSCAPE: Insets = { top: 0, right: 47, bottom: 21, left: 47 }

const REM_PX = 16

/**
 * The spacing scale, read out of `tokens.css`. App rules spend tokens rather than
 * raw lengths, so resolving a padding means resolving `var()` first. Read, never
 * transcribed — a retuned scale has to move these guards with it.
 */
const SPACING = spacingScale()

/** Parses `--kk-space-*` out of `tokens.css` into a token → length map. */
function spacingScale(): Map<string, string> {
  const out = new Map<string, string>()
  for (const match of readCss('src/styles/tokens.css').matchAll(
    /(--kk-space-\d+)\s*:\s*([^;]+);/g,
  )) {
    out.set(match[1], match[2].trim())
  }
  return out
}

/** Splits a value into its top-level terms, keeping `calc(a + b)` groups whole. */
function terms(value: string): string[] {
  const out: string[] = []
  let depth = 0
  let current = ''
  for (const ch of value) {
    if (ch === '(') {
      depth += 1
    } else if (ch === ')') {
      depth -= 1
    }
    if (depth === 0 && /\s/.test(ch)) {
      if (current !== '') {
        out.push(current)
        current = ''
      }
      continue
    }
    current += ch
  }
  if (current !== '') {
    out.push(current)
  }
  return out
}

/**
 * Resolves one length term — `0.75rem`, `12px` or `env(safe-area-inset-*, 0px)` —
 * to pixels under `insets`. Anything else throws rather than being silently read
 * as zero, so a rewrite in another form fails loudly instead of quietly passing.
 */
function lengthPx(term: string, insets: Insets): number {
  const env = /^env\(\s*safe-area-inset-(top|right|bottom|left)\s*,\s*0px\s*\)$/.exec(term)
  if (env !== null) {
    return insets[env[1] as Side]
  }
  const absolute = /^(-?[\d.]+)(rem|px)$/.exec(term)
  if (absolute !== null) {
    return Number(absolute[1]) * (absolute[2] === 'rem' ? REM_PX : 1)
  }
  const token = /^var\(\s*(--[\w-]+)\s*\)$/.exec(term)
  if (token !== null) {
    const value = SPACING.get(token[1])
    if (value === undefined) {
      throw new Error(`not a spacing token: ${token[1]}`)
    }
    return lengthPx(value, insets)
  }
  throw new Error(`unsupported length: ${term}`)
}

/** Resolves a single padding value, including the `calc(a + b)` sums used here. */
function valuePx(value: string, insets: Insets): number {
  const calc = /^calc\((.+)\)$/s.exec(value.trim())
  if (calc === null) {
    return lengthPx(value.trim(), insets)
  }
  return calc[1].split('+').reduce((sum, term) => sum + lengthPx(term.trim(), insets), 0)
}

/** Expands a `padding` shorthand (1–4 values) into its four sides. */
function expand(shorthand: string): Record<Side, string> {
  const parts = terms(shorthand)
  if (parts.length === 0 || parts.length > 4) {
    throw new Error(`unsupported padding shorthand: ${shorthand}`)
  }
  const top = parts[0]
  const right = parts.length > 1 ? parts[1] : top
  return {
    top,
    right,
    bottom: parts.length > 2 ? parts[2] : top,
    left: parts.length > 3 ? parts[3] : right,
  }
}

/** A rule's padding per side, shorthand first and longhands layered over it. */
function paddingSides(rule: Map<string, string>): Record<Side, string> {
  const shorthand = rule.get('padding')
  const sides: Record<Side, string> =
    shorthand === undefined
      ? { top: '0px', right: '0px', bottom: '0px', left: '0px' }
      : expand(shorthand)
  for (const side of SIDES) {
    const longhand = rule.get(`padding-${side}`)
    if (longhand !== undefined) {
      sides[side] = longhand
    }
  }
  return sides
}

/**
 * The padding a row actually gets, in pixels: the overlay container's padding plus
 * the row's own (they nest, so the insets add up on every side).
 */
function paddingPx(insets: Insets, ...rules: Map<string, string>[]): Insets {
  const total: Insets = { top: 0, right: 0, bottom: 0, left: 0 }
  for (const rule of rules) {
    const sides = paddingSides(rule)
    for (const side of SIDES) {
      total[side] += valuePx(sides[side], insets)
    }
  }
  return total
}

/**
 * The declarations of the rule matching `prelude` (and, when given, whose body
 * matches `contains` — a selector can appear in more than one rule). Throws when
 * there is no such rule, so a renamed class fails loudly instead of vacuously.
 */
function rule(css: string, prelude: RegExp, contains?: RegExp): Map<string, string> {
  const body = ruleBody(css, prelude, contains)
  if (body === undefined) {
    throw new Error(`rule not found: ${prelude.source}`)
  }
  return declarations(body)
}

describe('fullscreen review game safe-area insets', () => {
  const css = readCss('src/components/review/review.css')
  const game = rule(css, /\.review-game\s*(?=\{)/)
  const top = rule(css, /\.review-game__top\s*(?=\{)/)
  const actions = rule(css, /\.review-game__actions\s*(?=\{)/)
  const centre = rule(css, /\.review-game__center\s*(?=\{)/)
  // A landscape phone is both the shortest viewport and the one with a side
  // notch, and this block re-declares the chrome padding — the case where the
  // insets are easiest to lose.
  const short = ruleBody(css, /@media[^{]*\(max-height:\s*500px\)[^{]*/) ?? ''
  const shortTop = rule(short, /\.review-game__top\s*(?=\{)/)
  const shortActions = rule(short, /\.review-game__actions\s*(?=\{)/)

  it('keeps the spacing it always had where there is no notch', () => {
    expect(paddingPx(DESKTOP, game, top)).toEqual({ top: 8, right: 12, bottom: 8, left: 12 })
    expect(paddingPx(DESKTOP, game, actions)).toEqual({ top: 8, right: 16, bottom: 12, left: 16 })
    expect(paddingPx(DESKTOP, game, shortTop)).toEqual({ top: 4, right: 12, bottom: 4, left: 12 })
  })

  it('clears the notch and the home indicator in portrait', () => {
    // The close/undo header must start below the notch...
    expect(paddingPx(PORTRAIT, game, top).top).toBeGreaterThanOrEqual(PORTRAIT.top)
    // ...and the answer buttons must end above the home-indicator bar.
    expect(paddingPx(PORTRAIT, game, actions).bottom).toBeGreaterThanOrEqual(PORTRAIT.bottom)
    // The loading / error / empty bodies replace the answer row and then own the
    // bottom edge themselves (the retry button lives there).
    expect(paddingPx(PORTRAIT, game, centre).bottom).toBeGreaterThanOrEqual(PORTRAIT.bottom)
  })

  it('clears a side notch and the short home bar in landscape', () => {
    // The tightened landscape chrome re-declares the whole shorthand, so it
    // replaces the base padding — it has to carry the vertical insets itself.
    expect(shortTop.has('padding')).toBe(true)
    expect(shortActions.has('padding')).toBe(true)

    const header = paddingPx(LANDSCAPE, game, shortTop)
    expect(header.left).toBeGreaterThanOrEqual(LANDSCAPE.left)
    expect(header.right).toBeGreaterThanOrEqual(LANDSCAPE.right)

    const row = paddingPx(LANDSCAPE, game, shortActions)
    expect(row.bottom).toBeGreaterThanOrEqual(LANDSCAPE.bottom)
    expect(row.left).toBeGreaterThanOrEqual(LANDSCAPE.left)
    expect(row.right).toBeGreaterThanOrEqual(LANDSCAPE.right)
  })
})

describe('duplicate compare safe-area insets', () => {
  const css = readCss('src/components/duplicates/compare.css')
  const view = rule(css, /\.kk-compare\s*(?=\{)/)
  // Header and footer share the base padding rule, then each adds its own edge —
  // hence the `padding-*` filters, which pick those out of the shared rule.
  const shared = rule(css, /\.kk-compare__header,\s*\.kk-compare__footer\s*(?=\{)/)
  const header = rule(css, /\.kk-compare__header\s*(?=\{)/, /padding-top/)
  const footer = rule(css, /\.kk-compare__footer\s*(?=\{)/, /padding-bottom/)

  it('keeps the spacing it always had where there is no notch', () => {
    expect(paddingPx(DESKTOP, view, shared)).toEqual({ top: 8, right: 16, bottom: 8, left: 16 })
    expect(valuePx(paddingSides(header).top, DESKTOP)).toBe(8)
    expect(valuePx(paddingSides(footer).bottom, DESKTOP)).toBe(8)
  })

  it('clears the notch and the home indicator in portrait', () => {
    // Back / zoom / help at the top, Keep-left / both / right at the bottom.
    expect(valuePx(paddingSides(header).top, PORTRAIT)).toBeGreaterThanOrEqual(PORTRAIT.top)
    expect(valuePx(paddingSides(footer).bottom, PORTRAIT)).toBeGreaterThanOrEqual(PORTRAIT.bottom)
  })

  it('clears a side notch and the short home bar in landscape', () => {
    const rows = paddingPx(LANDSCAPE, view, shared)
    expect(rows.left).toBeGreaterThanOrEqual(LANDSCAPE.left)
    expect(rows.right).toBeGreaterThanOrEqual(LANDSCAPE.right)
    expect(valuePx(paddingSides(footer).bottom, LANDSCAPE)).toBeGreaterThanOrEqual(LANDSCAPE.bottom)
  })
})

/**
 * The phone filter drawer's footer, which owns the bottom edge while it is open.
 * The drawer is a full-height offcanvas layered over `.kk-tabbar` — the element
 * that normally carries the home-indicator inset for the whole app — so the
 * footer has to carry that inset itself; without it the button that closes the
 * drawer sits in the swipe strip, which is exactly the tap this footer exists to
 * make easy. Landscape matters as much as portrait here: the home bar is slimmer
 * but still there, and the footer is the same rule in both.
 */
describe('filter drawer footer safe-area insets', () => {
  const footer = rule(readCss('src/styles/app.css'), /\.kukatko-filter-footer\s*(?=\{)/)

  it('keeps the even padding it reads as where there is no home indicator', () => {
    // The inset is *added* to the spacing, never a replacement for it, so a
    // desktop-width drawer is unchanged.
    expect(paddingPx(DESKTOP, footer)).toEqual({ top: 12, right: 12, bottom: 12, left: 12 })
  })

  it('clears the home-indicator bar in portrait and in landscape alike', () => {
    expect(paddingPx(PORTRAIT, footer).bottom).toBeGreaterThanOrEqual(PORTRAIT.bottom)
    expect(paddingPx(LANDSCAPE, footer).bottom).toBeGreaterThanOrEqual(LANDSCAPE.bottom)
  })
})
