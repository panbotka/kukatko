import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

/**
 * Reads `tokens.css`. Vitest runs with the `web/` package as its cwd, but resolve
 * from the repo root too so the guard holds whoever launches it — `import.meta.url`
 * is unusable here, as the jsdom environment reports a non-`file:` document URL.
 */
function readTokensCss(): string {
  const candidate = ['src/styles/tokens.css', 'web/src/styles/tokens.css']
    .map((rel) => resolve(process.cwd(), rel))
    .find((path) => existsSync(path))
  if (candidate === undefined) {
    throw new Error('tokens.css not found from cwd ' + process.cwd())
  }
  return readFileSync(candidate, 'utf8')
}

/**
 * Returns the body of the block that opens at the first `{` at or after `from`,
 * brace-matched so nested rules inside an at-rule come back whole.
 */
function blockBodyAt(css: string, from: number): string {
  const open = css.indexOf('{', from)
  if (open === -1) {
    throw new Error('no block found')
  }
  let depth = 0
  for (let i = open; i < css.length; i += 1) {
    if (css[i] === '{') {
      depth += 1
    } else if (css[i] === '}') {
      depth -= 1
      if (depth === 0) {
        return css.slice(open + 1, i)
      }
    }
  }
  throw new Error('unbalanced braces in tokens.css')
}

/**
 * Returns the body of the first rule whose selector/prelude matches `prelude` and
 * whose body also satisfies `contains` (used to pick one of several `@media`
 * blocks apart). Undefined when no such rule exists.
 */
function ruleBody(css: string, prelude: RegExp, contains?: RegExp): string | undefined {
  const scan = new RegExp(prelude.source, prelude.flags.includes('g') ? prelude.flags : 'g')
  let match = scan.exec(css)
  while (match !== null) {
    const body = blockBodyAt(css, match.index + match[0].length)
    if (contains === undefined || contains.test(body)) {
      return body
    }
    match = scan.exec(css)
  }
  return undefined
}

/** Parses a rule body's declarations into a name → value map. */
function declarations(body: string): Map<string, string> {
  const out = new Map<string, string>()
  // Strip comments and any nested block so only this rule's own declarations remain.
  const own = body.replace(/\/\*[\s\S]*?\*\//g, '').replace(/[^{}]*\{[^{}]*\}/g, '')
  for (const line of own.split(';')) {
    const [name, ...rest] = line.split(':')
    if (rest.length > 0) {
      out.set(name.trim(), rest.join(':').trim())
    }
  }
  return out
}

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
  const css = readTokensCss()
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
