import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The page shell's horizontal gutter, guarded as a *relation* rather than as a
 * number: `.kukatko-main` and `.kukatko-footer` are Bootstrap `.container`s, and
 * every `.row` inside them opens with a negative horizontal margin of half its
 * gutter. The container's padding is what absorbs that bleed. Override the
 * padding to something smaller — a safe-area rule that *replaced* Bootstrap's
 * value instead of adding to it once zeroed it outright — and every row hangs
 * half a gutter past the container on both sides. On a desktop that is invisible,
 * because the container is narrower than the viewport and the bleed lands in the
 * spare margin; on a phone the container *is* the viewport, so the whole page can
 * be dragged sideways (eight routes did, by 9 to 66 px).
 *
 * jsdom loads no stylesheets and resolves neither `var()`, `env()` nor `calc()`,
 * so these guards read both sheets — the shipped Bootstrap and the app's override
 * — and resolve the lengths themselves. Nothing here is transcribed: retune the
 * gutter in either sheet and the guards move with it, which is the point.
 */

/** The Bootstrap build the app actually loads (see `main.tsx`). */
const VENDOR_CSS = 'node_modules/bootswatch/dist/superhero/bootstrap.min.css'

const REM_PX = 16

/** No notch: every `env(safe-area-inset-*)` resolves to 0, as on a desktop. */
const NO_INSETS = new Map<string, number>()
/** An iPhone-class landscape screen, where the side insets are the big ones. */
const LANDSCAPE = new Map([
  ['safe-area-inset-left', 47],
  ['safe-area-inset-right', 47],
])

/**
 * Resolves one factor of a product — a length, a bare multiplier, a `var()` or an
 * `env()` — to pixels. Anything else throws rather than being read as zero, so a
 * rewrite in another form fails loudly instead of quietly passing.
 */
function factorPx(term: string, vars: Map<string, number>): number {
  const text = term.trim()
  const env = /^env\(\s*(safe-area-inset-\w+)\s*,\s*0px\s*\)$/.exec(text)
  if (env !== null) {
    return vars.get(env[1]) ?? 0
  }
  const variable = /^var\(\s*(--[\w-]+)\s*\)$/.exec(text)
  if (variable !== null) {
    const value = vars.get(variable[1])
    if (value === undefined) {
      throw new Error(`unresolved custom property: ${variable[1]}`)
    }
    return value
  }
  const number = /^(-?(?:\d+(?:\.\d+)?|\.\d+))(rem|px)?$/.exec(text)
  if (number === null) {
    throw new Error(`unsupported term: ${text}`)
  }
  return Number(number[1]) * (number[2] === 'rem' ? REM_PX : 1)
}

/**
 * Resolves a declaration to pixels. Both sheets spend the same small dialect —
 * a `calc()` holding a sum of products (`var(--bs-gutter-x) * .5`, plus an inset)
 * — so a sum of products is all this needs to understand.
 */
function valuePx(expr: string, vars: Map<string, number>): number {
  const calc = /^calc\((.+)\)$/s.exec(expr.trim())
  const body = calc === null ? expr.trim() : calc[1]
  return body
    .split('+')
    .reduce(
      (sum, part) => sum + part.split('*').reduce((product, f) => product * factorPx(f, vars), 1),
      0,
    )
}

/** The declarations of the rule matching `prelude`; throws when there is none. */
function rule(css: string, prelude: RegExp, contains?: RegExp): Map<string, string> {
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

const vendor = readCss(VENDOR_CSS)
// The one rule that sizes every container flavour, picked out of the several
// `.container, …` preludes by the only body that carries the gutter itself.
const container = rule(vendor, /\.container\s*,[^{}]*(?=\{)/, /--bs-gutter-x/)
const row = rule(vendor, /\.row\s*(?=\{)/, /--bs-gutter-x/)
const shell = rule(readCss('src/styles/app.css'), /\.kukatko-main,\s*\.kukatko-footer\s*(?=\{)/)

/** The gutter a container hands its rows, in pixels (Bootstrap's `1.5rem`). */
const gutter = new Map([
  ['--bs-gutter-x', valuePx(declared(container, '--bs-gutter-x'), NO_INSETS)],
])

/** The shell's own padding for one side, under the given insets. */
function shellPaddingPx(side: 'left' | 'right', insets: Map<string, number>): number {
  return valuePx(declared(shell, `padding-${side}`), new Map([...gutter, ...insets]))
}

/** Every component source under `web/src`, wherever the runner was launched from. */
function sourceFiles(): string[] {
  const root = ['src', 'web/src']
    .map((rel) => resolve(process.cwd(), rel))
    .find((path) => existsSync(path))
  if (root === undefined) {
    throw new Error(`src not found from cwd ${process.cwd()}`)
  }
  return readdirSync(root, { recursive: true, encoding: 'utf8' })
    .filter((name) => name.endsWith('.tsx'))
    .map((name) => resolve(root, name))
}

/** The horizontal gutter the `g-N` / `gx-N` utility asks for, in pixels. */
function utilityGutterPx(step: string): number {
  const utility = rule(vendor, new RegExp(`\\.gx-${step}\\s*(?=\\{)`))
  return valuePx(declared(utility, '--bs-gutter-x'), NO_INSETS)
}

describe('page shell gutter vs. row bleed', () => {
  it('keeps the padding Bootstrap gives a container', () => {
    const bootstrap = valuePx(declared(container, 'padding-left'), gutter)
    expect(bootstrap).toBeGreaterThan(0)
    expect(shellPaddingPx('left', NO_INSETS)).toBe(bootstrap)
    expect(shellPaddingPx('right', NO_INSETS)).toBe(bootstrap)
  })

  it('absorbs the negative margin every row opens with', () => {
    // Bootstrap writes the bleed as a negative margin; what has to fit inside the
    // container's padding is its magnitude.
    const bleed = -valuePx(declared(row, 'margin-left'), gutter)
    expect(bleed).toBeGreaterThan(0)
    expect(shellPaddingPx('left', NO_INSETS)).toBeGreaterThanOrEqual(bleed)
    expect(shellPaddingPx('right', NO_INSETS)).toBeGreaterThanOrEqual(
      -valuePx(declared(row, 'margin-right'), gutter),
    )
  })

  it('is not out-bled by any gutter the app asks a row for', () => {
    // `g-5` / `gx-5` would widen a row's gutter past the container's own and
    // reintroduce the overflow, so what the markup spends is part of the relation.
    const steps = new Set<string>()
    for (const file of sourceFiles()) {
      for (const match of readFileSync(file, 'utf8').matchAll(/\bgx?-([0-5])\b/g)) {
        steps.add(match[1])
      }
    }
    expect(steps.size).toBeGreaterThan(0)
    for (const step of steps) {
      expect(utilityGutterPx(step) / 2).toBeLessThanOrEqual(shellPaddingPx('left', NO_INSETS))
    }
  })

  it('adds the safe-area inset to that padding instead of replacing it', () => {
    // The bug this file guards was exactly that shape: an inset rule that took
    // the container's padding over rather than adding to it.
    const bootstrap = valuePx(declared(container, 'padding-left'), gutter)
    expect(shellPaddingPx('left', LANDSCAPE)).toBe(bootstrap + 47)
    expect(shellPaddingPx('right', LANDSCAPE)).toBe(bootstrap + 47)
  })
})

/**
 * The other way a phone page ended up wider than the phone: a table that is
 * genuinely too wide scrolls inside `.table-responsive`, and that is fine — but
 * `overflow: auto` clips only descendants whose containing block is the scroller
 * itself. The job queue's action column is headed by a `.visually-hidden` label,
 * which Bootstrap positions `absolute`; with no positioned ancestor inside the
 * scroller it resolved against the `.card` outside it, stayed beside the
 * scrolled-away last column and gave `/system` a 485px scroll width on a 393px
 * screen. `position: relative` on the scroller is what keeps it in.
 */
describe('a scrolled table keeps its absolutely positioned children inside', () => {
  it('is still the scroll container Bootstrap makes it', () => {
    // The premise of the rule below: without the scrolling there is nothing to
    // escape from, and the app override would be pointless.
    expect(declared(rule(vendor, /\.table-responsive\s*(?=\{)/), 'overflow-x')).toBe('auto')
  })

  it('is a containing block, so nothing absolute escapes its clip', () => {
    const scroller = rule(readCss('src/styles/app.css'), /\.table-responsive\s*(?=\{)/)
    expect(declared(scroller, 'position')).toBe('relative')
  })
})
