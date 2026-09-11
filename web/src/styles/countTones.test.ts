import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The `/system` colour scale, measured rather than asserted.
 *
 * jsdom loads no stylesheet, so a rendered panel can only prove that a number
 * carries the right class (the panels' own tests do that). What the class then
 * *paints*, and whether it is legible where it is painted, can only be checked
 * here — and it is worth checking, because the page has two plates under it and
 * the same colour passes on one and fails on the other. A re-pinned token or a
 * "tidied" duplicate rule would silently take the numbers below the bar.
 */

/** One `#rrggbb` as its three channels. */
function channels(hex: string): [number, number, number] {
  const h = hex.trim().replace('#', '')
  const full = h.length === 3 ? h.replace(/./g, (c) => c + c) : h
  return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16)) as [number, number, number]
}

/** WCAG relative luminance. */
function luminance(hex: string): number {
  const [r, g, b] = channels(hex).map((c) => {
    const s = c / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio between two opaque colours. */
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** `a` laid over `b` at `alpha`, the sRGB channel mix both `opacity` and `color-mix(in srgb, …)` do. */
function mix(a: string, b: string, alpha: number): string {
  const [ca, cb] = [channels(a), channels(b)]
  return `#${ca
    .map((v, i) =>
      Math.round(alpha * v + (1 - alpha) * cb[i])
        .toString(16)
        .padStart(2, '0'),
    )
    .join('')}`
}

/**
 * Every `--name: #hex;` and `--name: var(--other);` in a stylesheet, last
 * definition winning — which for the two files read here is the one the cascade
 * lands on as well. The aliases matter: `tokens.css` re-points several of
 * Bootstrap's colour variables at `--kk-*` tokens rather than at a literal, and
 * reading only the literals would silently measure Superhero's orange instead of
 * this app's azure.
 */
function colourVariables(css: string): Map<string, string> {
  const found = new Map<string, string>()
  for (const m of css.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{3,8}|var\(--[\w-]+\))\s*;/g)) {
    found.set(m[1], m[2])
  }
  return found
}

const tokens = readCss('src/styles/tokens.css')
const bootswatch = readCss('node_modules/bootswatch/dist/superhero/bootstrap.css')
const vars = new Map([...colourVariables(bootswatch), ...colourVariables(tokens)])

/** A `var(--x)` or a `color-mix(in srgb, var(--x) N%, var(--y))` as a flat hex. */
function resolve(value: string): string {
  const plain = /^var\((--[\w-]+)\)$/.exec(value.trim())
  if (plain !== null) {
    return lookup(plain[1])
  }
  const blend =
    /^color-mix\(\s*in srgb,\s*var\((--[\w-]+)\)\s*(\d+)%,\s*var\((--[\w-]+)\)\s*\)$/.exec(
      value.trim(),
    )
  if (blend === null) {
    throw new Error(`cannot resolve ${value}`)
  }
  return mix(lookup(blend[1]), lookup(blend[3]), Number(blend[2]) / 100)
}

/** A colour variable as a flat hex, following an alias chain to its literal. */
function lookup(name: string, seen: string[] = []): string {
  if (seen.includes(name)) {
    throw new Error(`${name} aliases itself`)
  }
  const value = vars.get(name)
  if (value === undefined) {
    throw new Error(`${name} is not a colour token`)
  }
  const alias = /^var\((--[\w-]+)\)$/.exec(value)
  return alias === null ? value : lookup(alias[1], [...seen, name])
}

/** The colour a `.kk-count--*` rule paints, `display` picking the tile variant. */
function toneColour(tone: string, display: boolean): string {
  const prelude = display
    ? new RegExp(String.raw`\.kk-display\.kk-count--${tone}\s*(?=\{)`)
    : new RegExp(String.raw`(?<!\.kk-display)\.kk-count--${tone}\s*(?=\{)`)
  const body = ruleBody(tokens, prelude, /color:/)
  const colour = declarations(body ?? '')
    .get('color')
    ?.replace(/\s*!important$/, '')
  expect(colour, `.kk-count--${tone} paints no colour`).toBeDefined()
  return resolve(colour ?? '')
}

/**
 * The two plates, both taken from the stylesheets rather than from memory. A
 * table cell paints the page tone; a tile paints the `.card` fill, which
 * Superhero bakes into the `.card` rule itself and which is therefore NOT the
 * warm `--bs-card-bg` re-pin at the top of `tokens.css`.
 */
const CELL_PLATE = lookup('--kk-surface-page')
const TILE_PLATE =
  declarations(ruleBody(bootswatch, /\.card\s*(?=\{)/) ?? '').get('--bs-card-bg') ?? ''
expect(TILE_PLATE).toMatch(/^#[0-9a-fA-F]{6}$/)

const TONES = ['queued', 'running', 'failed'] as const

describe('the /system count scale', () => {
  // A table cell carries 14px text: WCAG AA wants 4.5:1.
  it.each(TONES)('keeps a %s table cell legible on the near-black page tone', (tone) => {
    expect(contrast(toneColour(tone, false), CELL_PLATE)).toBeGreaterThanOrEqual(4.5)
  })

  // A tile number is 30px at weight 600 — WCAG "large text", so the bar is 3:1.
  // It is the lower bar and the harder plate: Superhero's mid slate leaves very
  // little headroom, which is exactly why the tile variant exists at all.
  it.each(TONES)('keeps a %s tile number legible on the slate card fill', (tone) => {
    expect(contrast(toneColour(tone, true), TILE_PLATE)).toBeGreaterThanOrEqual(3)
  })

  it('needed the tile variant — the cell colours would not have cleared the card', () => {
    // Pins the reason the second set is not redundant: at least one of the three
    // cell colours genuinely fails the tile's bar, so deleting the
    // `.kk-display` rules would take the page below AA rather than simplify it.
    const failures = TONES.filter((tone) => contrast(toneColour(tone, false), TILE_PLATE) < 3)
    expect(failures.length).toBeGreaterThan(0)
  })

  it('keeps the muted zero legible on both plates', () => {
    // `text-secondary` is `--kk-text-muted`: the body text at 72%. It is what
    // every zero and every finished count wears.
    const muted = mix(lookup('--kk-text'), TILE_PLATE, 0.72)
    expect(contrast(muted, TILE_PLATE)).toBeGreaterThanOrEqual(3)
    expect(contrast(mix(lookup('--kk-text'), CELL_PLATE, 0.72), CELL_PLATE)).toBeGreaterThanOrEqual(
      4.5,
    )
  })
})

describe('the running-count pulse', () => {
  const trough = Number(
    declarations(ruleBody(tokens, /@keyframes kk-count-pulse\s*\{\s*50%\s*/) ?? '').get('opacity'),
  )

  it('breathes on a period of its own rather than a hard-coded one', () => {
    const animation = declarations(
      ruleBody(tokens, /\.kk-count--running\s*(?=\{)/, /animation:/) ?? '',
    ).get('animation')
    expect(animation).toContain('kk-count-pulse')
    expect(animation).toContain('var(--kk-duration-pulse)')
    expect(animation).toContain('infinite')
  })

  it('stays legible at the bottom of the breath, on both plates', () => {
    expect(trough).toBeLessThan(1)
    expect(
      contrast(mix(toneColour('running', false), CELL_PLATE, trough), CELL_PLATE),
    ).toBeGreaterThanOrEqual(4.5)
    expect(
      contrast(mix(toneColour('running', true), TILE_PLATE, trough), TILE_PLATE),
    ).toBeGreaterThanOrEqual(3)
  })

  it('is dropped entirely under prefers-reduced-motion', () => {
    const reduced = ruleBody(
      tokens,
      /@media \(prefers-reduced-motion: reduce\)\s*/,
      /kk-count--running/,
    )
    expect(
      declarations(ruleBody(reduced ?? '', /\.kk-count--running\s*/) ?? '').get('animation'),
    ).toBe('none')
  })
})
