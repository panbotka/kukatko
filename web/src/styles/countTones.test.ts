import { describe, expect, it } from 'vitest'

import {
  composite,
  contrastRatio,
  resolveColour,
  rootCustomProperties,
  stripCssComments,
  type Rgba,
} from '../test/colour'
import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The `/system` colour scale, measured rather than asserted.
 *
 * jsdom loads no stylesheet, so a rendered panel can only prove that a number
 * carries the right class (the panels' own tests do that). What the class then
 * *paints*, and whether it is legible where it is painted, can only be checked
 * here — the page has two plates under it, a table cell and a tile, and a
 * re-pinned token or a palette swap could silently take the numbers below the bar.
 */

const tokens = readCss('src/styles/tokens.css')
// Comments out: the bridge's own prose quotes Superhero's `.card { … }` rule.
const bridge = stripCssComments(readCss('src/styles/bootstrapBridge.css'))
const vars = rootCustomProperties()

function colour(expr: string): Rgba {
  return resolveColour(expr, vars)
}

/** The colour a `.kk-count--*` rule paints. */
function toneColour(tone: string): Rgba {
  const body = ruleBody(tokens, new RegExp(String.raw`\.kk-count--${tone}\s*(?=\{)`), /color:/)
  const painted = declarations(body ?? '')
    .get('color')
    ?.replace(/\s*!important$/, '')
  expect(painted, `.kk-count--${tone} paints no colour`).toBeDefined()
  return colour(painted ?? '')
}

/**
 * The two plates, both taken from the stylesheets rather than from memory: a
 * table cell paints `--bs-table-bg`, a tile the `.card` fill, which the bridge
 * re-pins on the `.card` rule itself (Superhero bakes it there as a literal).
 */
const CELL_PLATE = colour(
  declarations(ruleBody(bridge, /\.table\s*(?=\{)/) ?? '').get('--bs-table-bg') ?? '',
)
const TILE_PLATE = colour(
  declarations(ruleBody(bridge, /\.card\s*(?=\{)/) ?? '').get('--bs-card-bg') ?? '',
)

const TONES = ['queued', 'running', 'failed'] as const

describe('the /system count scale', () => {
  // A table cell carries 14px text: WCAG AA wants 4.5:1.
  it.each(TONES)('keeps a %s table cell legible on the page tone', (tone) => {
    expect(contrastRatio(toneColour(tone), CELL_PLATE)).toBeGreaterThanOrEqual(4.5)
  })

  // A tile number is 30px at weight 600 — WCAG "large text", so the bar is 3:1;
  // the tones are held to the text bar anyway, so one set serves both plates.
  it.each(TONES)('keeps a %s tile number legible on the card fill', (tone) => {
    expect(contrastRatio(toneColour(tone), TILE_PLATE)).toBeGreaterThanOrEqual(4.5)
  })

  it('paints the three states in three different colours', () => {
    const hexes = new Set(TONES.map((tone) => JSON.stringify(toneColour(tone))))
    expect(hexes.size).toBe(3)
  })

  it('keeps the muted zero legible on both plates', () => {
    // `text-secondary` is `--kk-text-muted`: the ink at 72%. It is what every
    // zero and every finished count wears.
    const muted = colour('var(--kk-text-muted)')
    expect(contrastRatio(muted, TILE_PLATE)).toBeGreaterThanOrEqual(4.5)
    expect(contrastRatio(muted, CELL_PLATE)).toBeGreaterThanOrEqual(4.5)
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
    const running = toneColour('running')
    for (const plate of [CELL_PLATE, TILE_PLATE]) {
      const faded = composite({ ...running, a: trough }, plate)
      expect(contrastRatio(faded, plate)).toBeGreaterThanOrEqual(4.5)
    }
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
