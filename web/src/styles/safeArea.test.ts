import { describe, expect, it } from 'vitest'

import { blockBodyAt, declarations, readCss, ruleBody } from '../test/css'

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
 * An iPhone-class screen height, for the one rule that sizes itself off `100vh`.
 * Under `viewport-fit=cover` that is the whole screen, status bar included.
 */
const SCREEN_HEIGHT_PX = 852

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

/** Pixels per unit of every absolute length unit these rules use. */
const UNIT_PX: Record<string, number> = { px: 1, rem: REM_PX, vh: SCREEN_HEIGHT_PX / 100 }

/**
 * Resolves one length term — `0`, `0.75rem`, `12px`, `100vh` or
 * `env(safe-area-inset-*, 0px)` — to pixels under `insets`. Anything else throws
 * rather than being silently read as zero, so a rewrite in another form fails
 * loudly instead of quietly passing.
 */
function lengthPx(term: string, insets: Insets): number {
  const env = /^env\(\s*safe-area-inset-(top|right|bottom|left)\s*,\s*0px\s*\)$/.exec(term)
  if (env !== null) {
    return insets[env[1] as Side]
  }
  if (term === '0') {
    return 0
  }
  const absolute = /^(-?[\d.]+)(rem|px|vh)$/.exec(term)
  if (absolute !== null) {
    return Number(absolute[1]) * UNIT_PX[absolute[2]]
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

/**
 * Resolves a single length value, including the `calc(a + b - c)` sums and
 * differences used here (CSS requires whitespace around both operators, so the
 * top-level terms alternate operand, operator, operand).
 */
function valuePx(value: string, insets: Insets): number {
  const calc = /^calc\((.+)\)$/s.exec(value.trim())
  if (calc === null) {
    return lengthPx(value.trim(), insets)
  }
  const parts = terms(calc[1])
  let total = lengthPx(parts[0], insets)
  for (let i = 1; i < parts.length; i += 2) {
    const operator = parts[i]
    const operand = parts.at(i + 1)
    if ((operator !== '+' && operator !== '-') || operand === undefined) {
      throw new Error(`unsupported calc(): ${value}`)
    }
    total += (operator === '+' ? 1 : -1) * lengthPx(operand, insets)
  }
  return total
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

/**
 * Every Bootstrap modal. `index.html` paints under the iOS status bar, and below
 * `sm` a `scrollable` dialog — every `ConfirmModal`, so every destructive confirm
 * — is forced to the full viewport height with its ✕ in the status-bar band and
 * its footer on the home indicator. One rule insets `.modal` itself, so the
 * dialog's margin box and its `100%` height both resolve against the padded box.
 *
 * A fullscreen modal steps out of that padding and insets its `.modal-content`
 * instead, so its surface still paints edge to edge — and must end up inset
 * exactly once, not by both. The `-sm-down` variant does so only inside
 * Bootstrap's phone breakpoint, because above it the class renders a windowed
 * dialog that takes the `.modal` rule like any other.
 */
describe('modal safe-area insets', () => {
  const css = readCss('src/styles/app.css')
  const modal = rule(css, /\n\.modal\s*(?=\{)/, /safe-area-inset/)
  const fullscreenOut = rule(css, /\n\.modal:has\(> \.modal-fullscreen\)\s*(?=\{)/)
  const fullscreen = rule(css, /\n\.modal-fullscreen \.modal-content\s*(?=\{)/, /safe-area-inset/)
  const phone = ruleBody(
    css,
    /@media \(max-width: 575\.98px\)\s*(?=\{)/,
    /\.modal-fullscreen-sm-down \.modal-content/,
  )
  const smDownOut = rule(phone ?? '', /\.modal:has\(> \.modal-fullscreen-sm-down\)\s*(?=\{)/)
  const smDown = rule(phone ?? '', /\.modal-fullscreen-sm-down \.modal-content\s*(?=\{)/)

  it('names all four insets on every modal', () => {
    const padding = modal.get('padding') ?? ''
    for (const side of SIDES) {
      expect(padding).toContain(`env(safe-area-inset-${side}, 0px)`)
    }
  })

  it('puts the inset on the modal, not on a dialog margin', () => {
    // A top margin alone would leave a full-height scrollable dialog too tall by
    // the inset; padding the fixed `.modal` shrinks the box its `100%` resolves in.
    const code = css.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(code).not.toMatch(/\.modal-dialog[^{}]*\{[^}]*safe-area-inset/)
  })

  it('names all four insets on the always-fullscreen modal', () => {
    const padding = fullscreen.get('padding') ?? ''
    for (const side of SIDES) {
      expect(padding).toContain(`env(safe-area-inset-${side}, 0px)`)
    }
  })

  it('takes the -sm-down variant only inside the phone media query', () => {
    expect(phone).toBeDefined()
    // Not at the top level too: above the breakpoint the dialog is windowed.
    expect(css).not.toMatch(/\n\.modal-fullscreen-sm-down \.modal-content\s*\{/)
    expect(css).not.toMatch(/\n\.modal:has\(> \.modal-fullscreen-sm-down\)\s*\{/)
    const padding = smDown.get('padding') ?? ''
    for (const side of SIDES) {
      expect(padding).toContain(`env(safe-area-inset-${side}, 0px)`)
    }
  })

  it('declares the fullscreen escapes after the rule they cancel', () => {
    // Specificity already favours `:has(...)`, but order says it twice.
    const at = (needle: string): number => css.indexOf(needle)
    const general = css.search(/\n\.modal \{\n\s*padding: env\(/)
    expect(general).toBeGreaterThan(-1)
    expect(at('.modal:has(> .modal-fullscreen)')).toBeGreaterThan(general)
    expect(at('.modal:has(> .modal-fullscreen-sm-down)')).toBeGreaterThan(general)
  })

  it('adds nothing where there is no notch', () => {
    expect(paddingPx(DESKTOP, modal)).toEqual(DESKTOP)
    expect(paddingPx(DESKTOP, fullscreenOut, fullscreen)).toEqual(DESKTOP)
    expect(paddingPx(DESKTOP, smDownOut, smDown)).toEqual(DESKTOP)
  })

  it('moves every dialog clear of the status bar, home bar and a side notch', () => {
    expect(paddingPx(PORTRAIT, modal)).toEqual(PORTRAIT)
    expect(paddingPx(LANDSCAPE, modal)).toEqual(LANDSCAPE)
  })

  it('insets a fullscreen modal exactly once', () => {
    // The modal's own padding is cancelled, the content's is the only inset: the
    // two layers nest, so they must sum to the insets, not to twice them.
    for (const [out, content] of [
      [fullscreenOut, fullscreen],
      [smDownOut, smDown],
    ]) {
      expect(paddingPx(PORTRAIT, out)).toEqual(DESKTOP)
      expect(paddingPx(PORTRAIT, out, content)).toEqual(PORTRAIT)
      expect(paddingPx(LANDSCAPE, out, content)).toEqual(LANDSCAPE)
    }
  })
})

/**
 * The phone filter drawers — the library's and the map's. Both are an
 * `end`-placed offcanvas whose close-button header is the only way out besides
 * the backdrop, and the panel meets the top and the right edge of the screen, so
 * without the insets that ✕ sat on the battery indicator (or behind a landscape
 * notch). The bottom edge is the footer's, which already adds its inset — the
 * drawer adding it too would pad that edge twice.
 */
describe('filter drawer safe-area insets', () => {
  const css = readCss('src/styles/app.css')
  const drawer = rule(css, /\n\.kukatko-filter-drawer\s*(?=\{)/)
  const footer = rule(css, /\.kukatko-filter-footer\s*(?=\{)/)

  it.each([['src/components/library/FilterBar.tsx'], ['src/components/map/MapFilterBar.tsx']])(
    '%s puts its drawer under the inset rule',
    (path) => {
      const source = readCss(path)
      // Every Offcanvas the bar opens, from its tag to its close-button header.
      const drawers = [...source.matchAll(/<Offcanvas\s[\s\S]*?<Offcanvas\.Header closeButton/g)]
      expect(drawers).toHaveLength(1)
      for (const [tag] of drawers) {
        expect(tag).toContain('className="kukatko-filter-drawer"')
      }
    },
  )

  it('adds nothing where there is no notch', () => {
    expect(paddingPx(DESKTOP, drawer)).toEqual(DESKTOP)
  })

  it('moves the header clear of the status bar and a right-hand notch', () => {
    expect(paddingPx(PORTRAIT, drawer).top).toBe(PORTRAIT.top)
    expect(paddingPx(LANDSCAPE, drawer).right).toBe(LANDSCAPE.right)
  })

  it('leaves the middle-facing edge alone and the bottom to the footer', () => {
    expect(paddingPx(LANDSCAPE, drawer).left).toBe(0)
    expect(paddingPx(PORTRAIT, drawer).bottom).toBe(0)
    // ...and the footer, the last row, still clears the home indicator once.
    const bottom = paddingPx(PORTRAIT, drawer).bottom + paddingPx(PORTRAIT, footer).bottom
    expect(bottom - paddingPx(DESKTOP, footer).bottom).toBe(PORTRAIT.bottom)
  })
})

/**
 * The photo viewer's info drawer. At ≥768px it is a side drawer against the top
 * and right edge of the screen — which is also a landscape iPhone Pro Max and
 * every iPad — so its head (the ✕) has to clear the status bar and the notch
 * column. Below 768px the very same element is a bottom sheet with no top or
 * right screen edge, and padding it for one would be dead space.
 */
describe('viewer drawer head safe-area insets', () => {
  const css = readCss('src/components/photo/viewer.css')
  const base = rule(css, /\n\.kk-viewer__panel-head\s*(?=\{)/)
  const wide = ruleBody(css, /@media \(min-width: 768px\)\s*(?=\{)/, /kk-viewer__panel-head/)
  const wideHead = rule(wide ?? '', /\.kk-viewer__panel-head\s*(?=\{)/)

  /** Every rule on the drawer or one of its parts inside a phone (sheet) block. */
  function sheetRules(): { selector: string; body: string }[] {
    const out: { selector: string; body: string }[] = []
    for (const match of css.matchAll(/@media \(max-width: 767\.98px\)\s*(?=\{)/g)) {
      const block = blockBodyAt(css, match.index + match[0].length)
      for (const panel of block.matchAll(/(\.kk-viewer__panel[\w-]*)\s*\{([^{}]*)\}/g)) {
        out.push({ selector: panel[1], body: panel[2] })
      }
    }
    return out
  }

  it('takes the insets inside the >=768px branch only', () => {
    expect(wide).toBeDefined()
    // The base rule serves the sheet too, so it must not carry them.
    expect([...base.values()].join(' ')).not.toContain('safe-area-inset')
  })

  it('keeps the head spacing it always had where there is no notch', () => {
    const plain = paddingPx(DESKTOP, base)
    expect({ ...plain, top: valuePx(wideHead.get('padding-top') ?? '', DESKTOP) }).toEqual(plain)
    expect(valuePx(wideHead.get('padding-right') ?? '', DESKTOP)).toBe(plain.right)
  })

  it('moves the close button clear of the status bar and a right-hand notch', () => {
    const plain = paddingPx(DESKTOP, base)
    expect(valuePx(wideHead.get('padding-top') ?? '', PORTRAIT)).toBe(plain.top + PORTRAIT.top)
    expect(valuePx(wideHead.get('padding-right') ?? '', LANDSCAPE)).toBe(
      plain.right + LANDSCAPE.right,
    )
  })

  it('gives the phone sheet no top or right inset', () => {
    const rules = sheetRules()
    expect(rules.map((r) => r.selector)).toContain('.kk-viewer__panel')
    for (const { body } of rules) {
      expect(body).not.toMatch(/safe-area-inset-(top|right)/)
    }
  })
})

/**
 * The slideshow's settings panel. It floats above the controls bar, which adds
 * the home-indicator inset to its own height — so a fixed offset let the bar (the
 * later sibling) paint over the panel's last rows and take their taps. Its height
 * is capped off `100vh`, which under `viewport-fit=cover` is the whole screen, so
 * both insets have to come off that cap or the top edge lands under the island.
 */
describe('slideshow settings safe-area insets', () => {
  const css = readCss('src/components/slideshow/slideshow.css')
  const settings = rule(css, /\n\.slideshow__settings\s*(?=\{)/)
  const controls = rule(css, /\n\.slideshow__controls\s*(?=\{)/)

  const offset = (insets: Insets): number => valuePx(settings.get('bottom') ?? '', insets)
  const cap = (insets: Insets): number => valuePx(settings.get('max-height') ?? '', insets)
  /** Where the panel's top edge lands when it is as tall as it may be. */
  const topEdge = (insets: Insets): number => SCREEN_HEIGHT_PX - offset(insets) - cap(insets)

  it('keeps the offset and cap it always had where there is no notch', () => {
    expect(offset(DESKTOP)).toBe(4.5 * REM_PX)
    expect(cap(DESKTOP)).toBe(SCREEN_HEIGHT_PX - 8 * REM_PX)
  })

  it('rises with the controls bar, so the bar cannot cover it', () => {
    for (const insets of [PORTRAIT, LANDSCAPE]) {
      const barGrowth = paddingPx(insets, controls).bottom - paddingPx(DESKTOP, controls).bottom
      expect(offset(insets) - offset(DESKTOP)).toBe(barGrowth)
    }
  })

  it('keeps its top edge below the status bar', () => {
    expect(topEdge(DESKTOP)).toBe(3.5 * REM_PX)
    for (const insets of [PORTRAIT, LANDSCAPE]) {
      expect(topEdge(insets)).toBe(insets.top + 3.5 * REM_PX)
    }
  })
})

/**
 * The app-wide toast stack. It is fixed to the top of the viewport, which the iOS
 * PWA paints under, so without the insets a toast's close button sits in the
 * status-bar band (where a tap scrolls the page instead) and, in landscape,
 * behind the notch column. The bottom edge is the toast's, not the screen's, so
 * it keeps its plain spacing.
 */
describe('toast stack safe-area insets', () => {
  const stack = rule(readCss('src/styles/tokens.css'), /\.kk-toast-stack\s*(?=\{)/)

  it('names the top, left and right insets', () => {
    const padding = stack.get('padding') ?? ''
    for (const side of ['top', 'right', 'left'] as const) {
      expect(padding).toContain(`env(safe-area-inset-${side}, 0px)`)
    }
  })

  it('keeps the 1rem it always had where there is no notch', () => {
    expect(paddingPx(DESKTOP, stack)).toEqual({ top: 16, right: 16, bottom: 16, left: 16 })
  })

  it('clears the status bar in portrait and the notch column in landscape', () => {
    expect(paddingPx(PORTRAIT, stack).top).toBeGreaterThanOrEqual(PORTRAIT.top)
    const side = paddingPx(LANDSCAPE, stack)
    expect(side.left).toBeGreaterThanOrEqual(LANDSCAPE.left)
    expect(side.right).toBeGreaterThanOrEqual(LANDSCAPE.right)
  })
})
