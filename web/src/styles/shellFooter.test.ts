import { describe, expect, it } from 'vitest'

import { blockBodyAt, declarations, readCss, ruleBody } from '../test/css'

/**
 * The footer at the bottom of a short page, guarded as a *relation* rather than
 * as a number. The shell used to have no minimum height, so on any page whose
 * content is shorter than the viewport the footer sat directly under the content
 * with a large empty band beneath it — measured on `/people` in a 1440x900
 * window: the footer ended at 451px, leaving 449px of nothing below. That is
 * every empty state, which is most of what a new instance shows.
 *
 * The fix is one flex column (`.kukatko-shell`) spanning the navbar and the page,
 * tall enough to reach the bottom edge; the page grows into the slack and the
 * footer takes the rest of it with `margin-top: auto`. Every part of that chain
 * is load-bearing and useless on its own, so the guards below check them
 * together — along with the two ways the fix could go wrong: reserving *too much*
 * (a scrollbar on a page that had none) and reserving a fixed height instead of a
 * minimum (a tall page clipped, or a footer pinned over the content).
 *
 * jsdom loads no stylesheets and resolves neither `var()`, `env()`, `calc()` nor
 * `dvh`, so this file reads the shipped sheet and resolves the lengths itself
 * against a named screen. Nothing is transcribed: retune the bottom-edge token
 * and the guards move with it, which is the point.
 */

const REM_PX = 16

const css = readCss('src/styles/app.css')

/** The declarations of the rule matching `prelude`; throws when there is none. */
function rule(prelude: RegExp, contains?: RegExp): Map<string, string> {
  const body = ruleBody(css, prelude, contains)
  if (body === undefined) {
    throw new Error(`rule not found: ${prelude.source}`)
  }
  return declarations(body)
}

/** One declaration of a rule; throws when the rule stopped declaring it. */
function declared(rules: Map<string, string>, name: string): string {
  const value = rules.get(name)
  if (value === undefined) {
    throw new Error(`no ${name} declaration`)
  }
  return value
}

/** The block holding the app's own layout tokens. */
const tokens = rule(/:root\s*(?=\{)/, /--kk-bottom-edge/)

/** The three rules that hold the footer down, plus the document below them. */
const shell = rule(/\.kukatko-shell\s*(?=\{)/)
const page = rule(/\.kukatko-page\s*(?=\{)/, /flex/)
const footer = rule(/\.kukatko-footer\s*(?=\{)/, /margin-top/)
const body = rule(/\bbody\s*(?=\{)/, /padding-bottom/)

/**
 * A screen to resolve lengths against: the viewport height plus the two things
 * that vary with the device — the notch insets, and the tab bar's live height
 * (`MobileTabBar` publishes it over the `0px` the sheet declares at rest).
 */
interface Screen {
  /** Viewport height in CSS pixels, what `100dvh` resolves to. */
  height: number
  /** `env(safe-area-inset-*)` values; anything absent resolves to its fallback. */
  insets: Map<string, number>
  /** The rendered height of the mobile tab bar, `0` where none is mounted. */
  tabBar: number
}

/** A 1440x900 desktop window: no notch, no tab bar — the bug's own measurement. */
const DESKTOP: Screen = { height: 900, insets: new Map(), tabBar: 0 }
/** A notched phone with the bottom tab bar mounted (its height carries the inset). */
const PHONE: Screen = {
  height: 852,
  insets: new Map([['safe-area-inset-bottom', 34]]),
  tabBar: 83,
}

/** Splits `expr` on top-level `+`/`-`, keeping each term's sign. */
function terms(expr: string): { sign: number; text: string }[] {
  const out: { sign: number; text: string }[] = []
  let depth = 0
  let sign = 1
  let start = 0
  for (let i = 0; i < expr.length; i += 1) {
    const ch = expr[i]
    if (ch === '(') {
      depth += 1
    } else if (ch === ')') {
      depth -= 1
    } else if (depth === 0 && (ch === '+' || ch === '-') && i > start) {
      out.push({ sign, text: expr.slice(start, i) })
      sign = ch === '+' ? 1 : -1
      start = i + 1
    }
  }
  out.push({ sign, text: expr.slice(start) })
  return out
}

/** Splits a function's argument list on top-level commas. */
function args(list: string): string[] {
  const out: string[] = []
  let depth = 0
  let start = 0
  for (let i = 0; i < list.length; i += 1) {
    const ch = list[i]
    if (ch === '(') {
      depth += 1
    } else if (ch === ')') {
      depth -= 1
    } else if (ch === ',' && depth === 0) {
      out.push(list.slice(start, i))
      start = i + 1
    }
  }
  out.push(list.slice(start))
  return out
}

/**
 * Resolves a length to pixels on the given screen. The sheet spends a small
 * dialect here — `calc()`, `max()`, `env()`, `var()` and `dvh`/`rem`/`px` — and
 * anything outside it throws rather than being read as zero, so a rewrite in
 * another form fails loudly instead of quietly passing.
 */
function lengthPx(expr: string, screen: Screen): number {
  const text = expr.trim()

  const fn = /^(calc|max|min)\((.*)\)$/s.exec(text)
  if (fn !== null) {
    if (fn[1] === 'calc') {
      return terms(fn[2]).reduce((sum, term) => sum + term.sign * lengthPx(term.text, screen), 0)
    }
    const parts = args(fn[2]).map((part) => lengthPx(part, screen))
    return fn[1] === 'max' ? Math.max(...parts) : Math.min(...parts)
  }

  const env = /^env\(\s*([\w-]+)\s*,\s*([^)]+)\)$/.exec(text)
  if (env !== null) {
    return screen.insets.get(env[1]) ?? lengthPx(env[2], screen)
  }

  const variable = /^var\(\s*(--[\w-]+)\s*\)$/.exec(text)
  if (variable !== null) {
    // The tab bar's height is written by `MobileTabBar` at runtime; the sheet's
    // own `0px` is only the no-bar case, which is the desktop screen anyway.
    if (variable[1] === '--kk-tabbar-height') {
      return screen.tabBar
    }
    return lengthPx(declared(tokens, variable[1]), screen)
  }

  // The unit is a required group with an empty alternative, so a bare number
  // comes back as `''` — one string to switch on, and no optional-group narrowing.
  const number = /^(-?(?:\d+(?:\.\d+)?|\.\d+))(dvh|vh|rem|px|)$/.exec(text)
  if (number === null) {
    throw new Error(`unsupported length: ${text}`)
  }
  return Number(number[1]) * unitPx(number[2], screen)
}

/** What one unit of `unit` is worth in pixels; an empty unit is a bare `px`. */
function unitPx(unit: string, screen: Screen): number {
  switch (unit) {
    case 'dvh':
    case 'vh':
      return screen.height / 100
    case 'rem':
      return REM_PX
    default:
      return 1
  }
}

/** The shell's minimum height on the given screen. */
function shellMinHeightPx(screen: Screen): number {
  return lengthPx(declared(shell, 'min-height'), screen)
}

describe('the shell holds the footer at the bottom of a short page', () => {
  it('reaches the bottom of a window it does not otherwise fill', () => {
    // The bug: no minimum at all, so a short page ended wherever its content did.
    // Nothing is reserved above the shell — the navbar is inside it — so on a
    // desktop the column is the whole window, which is what puts the footer *on*
    // the bottom edge rather than near it.
    expect(shellMinHeightPx(DESKTOP)).toBe(DESKTOP.height)
  })

  it('never claims more than the viewport, so no short page starts scrolling', () => {
    for (const screen of [DESKTOP, PHONE]) {
      // The shell and the clearance `body` keeps below it have to fit the viewport
      // together; overshoot by a pixel and every empty state grows a scrollbar it
      // did not have before (the `--kukatko-navbar-height` estimate did exactly
      // that, by the 1px the real bar differs from it).
      const clearance = lengthPx(declared(body, 'padding-bottom'), screen)
      expect(shellMinHeightPx(screen) + clearance).toBeLessThanOrEqual(screen.height)
    }
  })

  it('keeps the phone tab bar out of the height it claims', () => {
    // The bar is fixed over the bottom edge: count it in, or the footer ends up
    // behind the tabs on exactly the pages this change is about. It is the
    // *taller* of the bar and the bare home-indicator inset that is kept clear,
    // never their sum — the published height already carries the inset.
    expect(shellMinHeightPx(PHONE)).toBe(PHONE.height - PHONE.tabBar)
  })

  it('is a minimum, not a height, so a long page still grows past it', () => {
    expect(shell.has('min-height')).toBe(true)
    expect(shell.has('height')).toBe(false)
    expect(shell.get('max-height')).toBeUndefined()
  })

  it('passes the slack down to the footer, which takes it with an auto margin', () => {
    // Three declarations, one chain: the shell is a column, the page grows to
    // fill it, and only then does the footer's auto top margin have free space to
    // absorb. Drop any one of them and the footer floats mid-screen again.
    expect(declared(shell, 'display')).toBe('flex')
    expect(declared(shell, 'flex-direction')).toBe('column')
    expect(Number(declared(page, 'flex').split(/\s+/)[0])).toBeGreaterThanOrEqual(1)
    expect(declared(page, 'display')).toBe('flex')
    expect(declared(page, 'flex-direction')).toBe('column')
    expect(declared(footer, 'margin-top')).toBe('auto')
  })

  it('leaves the footer in flow, so a long page scrolls it away', () => {
    // The lazy way to hold the bottom edge is `position: fixed`, which turns the
    // footer into a bar overlaying every tall page. Every rule the sheet writes
    // for the footer has to leave it in normal flow, not just the one above.
    const rules = [...css.matchAll(/[^{}]*\.kukatko-footer[^{}]*(?=\{)/g)]
    expect(rules.length).toBeGreaterThan(0)
    for (const match of rules) {
      expect(
        declarations(blockBodyAt(css, match.index + match[0].length)).get('position'),
      ).toBeUndefined()
    }
  })
})
