import { describe, expect, it } from 'vitest'

import {
  composite,
  contrastRatio,
  parseHex,
  resolveColour,
  rootCustomProperties,
  stripCssComments,
  toHex,
  type Rgba,
} from '../test/colour'
import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The palette, measured.
 *
 * Every colour in the app is derived from the five `--kk-palette-*` colours (and
 * the semantic block) in `palette.css`, so swapping the palette re-colours the
 * whole interface at once — including every place where text meets a surface.
 * This suite resolves the tokens exactly as the browser would and holds each
 * text/background combination the tokens define to WCAG AA: 4.5:1 for text,
 * 3:1 for large text and for the non-text edges a user has to find (form-field
 * borders, the focus ring, the selection outline). A new palette that puts light
 * on light or dark on dark anywhere fails here, in `make check`, before anyone
 * has to look at it.
 */

const tokens = rootCustomProperties()

function colour(expr: string): Rgba {
  return resolveColour(expr, tokens)
}

const token = (name: string): string => `var(${name})`

/** The opaque surfaces text sits on, from the page up the elevation ramp and down into the well. */
const SURFACES = [
  '--kk-surface-page',
  '--kk-surface-1',
  '--kk-surface-raised',
  '--kk-surface-overlay',
  '--kk-surface-sunken',
] as const

const TEXT = 4.5
const NON_TEXT = 3

/** Asserts `fg` on `bg` (each a token name or expression) reaches `min`. */
function expectContrast(fg: string, bg: string, min: number): void {
  const background = colour(bg.startsWith('--') ? token(bg) : bg)
  const opaque =
    background.a < 1 ? composite(background, colour(token('--kk-surface-page'))) : background
  const foreground = colour(fg.startsWith('--') ? token(fg) : fg)
  const ratio = contrastRatio(foreground, opaque)
  expect(
    ratio,
    `${fg} on ${bg} (${toHex(composite(foreground, opaque))} on ${toHex(opaque)}) is ${ratio.toFixed(2)}:1, needs ${min}:1`,
  ).toBeGreaterThanOrEqual(min)
}

describe('the palette file', () => {
  it('declares the five role-named palette colours as hex values', () => {
    for (const role of ['base', 'surface-accent', 'ink', 'accent', 'warm']) {
      expect(tokens.get(`--kk-palette-${role}`)).toMatch(/^#[0-9a-fA-F]{6}$/)
    }
  })

  it('keeps every -rgb triplet equal to the hex it mirrors', () => {
    const triplets = [...tokens.keys()].filter(
      (name) => name.startsWith('--kk-palette-') || name.startsWith('--kk-semantic-'),
    )
    const checked = triplets.filter((name) => name.endsWith('-rgb'))
    expect(checked.length).toBeGreaterThanOrEqual(8)
    for (const name of checked) {
      const hex = parseHex(tokens.get(name.replace(/-rgb$/, '')) ?? '')
      expect(tokens.get(name), name).toBe(`${hex.r}, ${hex.g}, ${hex.b}`)
    }
  })

  it('bakes the matching palette colour into every data-URI glyph', () => {
    const glyphs: [string, string][] = [
      ['--kk-select-chevron', '--kk-palette-ink'],
      ['--kk-check-glyph', '--kk-palette-base'],
      ['--kk-radio-glyph', '--kk-palette-base'],
      ['--kk-indeterminate-glyph', '--kk-palette-base'],
      ['--kk-switch-on-glyph', '--kk-palette-base'],
      ['--kk-switch-off-glyph', '--kk-palette-ink'],
      ['--kk-switch-focus-glyph', '--kk-palette-accent'],
    ]
    for (const [glyph, source] of glyphs) {
      const baked = /%23([0-9a-fA-F]{6})/.exec(tokens.get(glyph) ?? '')?.[1]
      expect(baked?.toLowerCase(), glyph).toBe(tokens.get(source)?.replace('#', '').toLowerCase())
    }
  })

  it('is the first of the app’s stylesheets, ahead of the tokens that read it', () => {
    const main = readCss('src/main.tsx')
    const order = ['palette.css', 'tokens.css', 'bootstrapBridge.css', 'app.css'].map((sheet) =>
      main.indexOf(`./styles/${sheet}`),
    )
    expect(order.every((at) => at > -1)).toBe(true)
    expect([...order].sort((a, b) => a - b)).toEqual(order)
  })
})

describe('text on the surfaces', () => {
  const tones = [
    '--kk-text',
    '--kk-text-muted',
    '--kk-text-subtle',
    '--kk-accent',
    '--kk-accent-hover',
    '--kk-secondary-text',
    '--kk-info-text',
    '--kk-success-text',
    '--kk-warning-text',
    '--kk-danger-text',
    '--kk-semantic-gold',
    '--kk-semantic-silver',
    '--kk-semantic-bronze',
  ]
  const cases = SURFACES.flatMap((surface) => tones.map((tone) => [tone, surface] as const))

  it.each(cases)('%s reads on %s', (tone, surface) => {
    expectContrast(tone, surface, TEXT)
  })

  it.each(SURFACES)('the body text reads on a table hover and a menu hover over %s', (surface) => {
    for (const hover of [
      'color-mix(in srgb, var(--kk-palette-ink) 8%, transparent)',
      'color-mix(in srgb, var(--kk-palette-ink) 12%, transparent)',
    ]) {
      const plate = composite(colour(hover), colour(token(surface)))
      expect(contrastRatio(colour(token('--kk-text')), plate)).toBeGreaterThanOrEqual(TEXT)
    }
  })
})

describe('labels on fills', () => {
  const fills: [string, string][] = [
    ['--kk-on-accent', '--kk-accent-solid'],
    ['--kk-on-accent', '--kk-accent-solid-hover'],
    ['--kk-on-secondary', '--kk-secondary'],
    ['--kk-on-secondary', '--kk-secondary-hover'],
    ['--kk-on-info', '--kk-info'],
    ['--kk-on-success', '--kk-success'],
    ['--kk-on-success', '--kk-success-hover'],
    ['--kk-on-warning', '--kk-warning'],
    ['--kk-on-warning', '--kk-warning-hover'],
    ['--kk-on-danger', '--kk-danger'],
    ['--kk-on-danger', '--kk-danger-hover'],
    ['--kk-on-light', '--kk-light'],
    ['--kk-on-light', '--kk-light-hover'],
    ['--kk-on-dark', '--kk-dark'],
    ['--kk-on-dark', '--kk-dark-hover'],
    ['--kk-on-dark', '--bs-dark'],
    ['--kk-on-warm', '--kk-warm'],
    ['--kk-on-secondary', '--kk-secondary'],
    ['--kk-selection-color', '--kk-selection-bg'],
    ['--kk-entity-fg', '--kk-entity-album-bg'],
    ['--kk-entity-fg', '--kk-entity-tag-bg'],
    ['--kk-entity-fg', '--kk-entity-person-bg'],
    ['--kk-task-question-fg', '--kk-task-question-bg'],
    ['--kk-task-fg', '--kk-task-working-bg'],
    ['--kk-task-fg', '--kk-task-review-bg'],
    ['--kk-task-fg', '--kk-task-done-bg'],
    ['--kk-task-fg', '--kk-task-rejected-bg'],
    ...Array.from({ length: 8 }, (_, i): [string, string] => [
      '--kk-avatar-fg',
      `--kk-avatar-${i}-bg`,
    ]),
  ]

  it.each(fills)('%s reads on %s', (fg, bg) => {
    expectContrast(fg, bg, TEXT)
  })
})

describe('text on the tinted surfaces', () => {
  const tints = [
    '--bs-primary-bg-subtle',
    '--bs-secondary-bg-subtle',
    '--bs-success-bg-subtle',
    '--bs-info-bg-subtle',
    '--bs-warning-bg-subtle',
    '--bs-danger-bg-subtle',
    '--bs-light-bg-subtle',
    '--bs-dark-bg-subtle',
    '--bs-highlight-bg',
  ]

  it.each(tints)('the body text and the muted text read on %s (an alert, a mark)', (tint) => {
    expectContrast('--kk-text', tint, TEXT)
    expectContrast('--kk-text-muted', tint, TEXT)
  })

  it('keeps the accent legible on its own subtle pill (the active nav item)', () => {
    expectContrast('--kk-accent', '--kk-accent-subtle', TEXT)
  })
})

describe('edges a user has to find', () => {
  it.each(SURFACES)('a form field’s border clears 3:1 on %s', (surface) => {
    expectContrast('--kk-control-border', surface, NON_TEXT)
    expectContrast('--kk-control-border-strong', surface, NON_TEXT)
  })

  it.each(SURFACES)('the focus ring and the selection outline clear 3:1 on %s', (surface) => {
    expectContrast('--kk-focus-ring-color', surface, NON_TEXT)
    expectContrast('--kk-accent', surface, NON_TEXT)
  })

  it('draws a form field’s border apart from its own sunken fill', () => {
    expectContrast('--kk-control-border', '--kk-surface-sunken', NON_TEXT)
  })
})

describe('an unchecked checkbox and switch', () => {
  // Since the palette swap an unchecked control is a dark well on a dark panel,
  // so what makes it findable is its edge and — for a switch — its knob. Both
  // are non-text UI components (WCAG 1.4.11): 3:1 against what they sit on, on
  // every surface a form appears on (page, card, modal, offcanvas), both enabled
  // and disabled. A disabled control fades as a whole (`opacity`), so its edge,
  // track and knob are measured as the browser composites them over the surface.
  const bridge = stripCssComments(readCss('src/styles/bootstrapBridge.css'))
  const opacity = Number(tokens.get('--kk-control-disabled-opacity'))
  const track = colour(token('--kk-surface-sunken'))

  /** The off knob as painted on its (opaque) track: the glyph's ink at its `fill-opacity`. */
  function knob(): Rgba {
    const glyph = tokens.get('--kk-switch-off-glyph') ?? ''
    const alpha = Number(/fill-opacity='([\d.]+)'/.exec(glyph)?.[1])
    expect(alpha, 'the off knob declares a fill-opacity').toBeGreaterThan(0)
    return composite({ ...colour(token('--kk-palette-ink')), a: alpha }, track)
  }

  /** `c` faded to the disabled opacity over the opaque `under`. */
  function faded(c: Rgba, under: Rgba): Rgba {
    return composite({ ...c, a: opacity }, under)
  }

  it('paints a real border — Superhero compiles the control with `border: none`', () => {
    const body = ruleBody(bridge, /(^|\})\s*\.form-check-input\s*(?=\{)/)
    expect(body).toBeDefined()
    expect(declarations(body ?? '').get('border')).toBe(
      'var(--bs-border-width) solid var(--kk-control-border)',
    )
  })

  it('replaces the dark theme’s 25 % white off knob with the palette glyph', () => {
    const body = ruleBody(
      bridge,
      /\[data-bs-theme='dark'\] \.form-switch \.form-check-input:not\(:checked\):not\(:focus\)\s*(?=\{)/,
    )
    expect(declarations(body ?? '').get('--bs-form-switch-bg')).toBe('var(--kk-switch-off-glyph)')
  })

  it('fades a disabled control less than Superhero’s 0.5 and strengthens its edge', () => {
    expect(opacity).toBeGreaterThan(0.5)
    expect(opacity).toBeLessThan(1)
    const body = ruleBody(bridge, /\.form-check-input:disabled\s*(?=\{)/)
    expect(declarations(body ?? '').get('opacity')).toBe('var(--kk-control-disabled-opacity)')
    const unchecked = ruleBody(
      bridge,
      /\.form-check-input:disabled:not\(:checked, :indeterminate\)\s*(?=\{)/,
    )
    expect(declarations(unchecked ?? '').get('border-color')).toBe(
      'var(--kk-control-border-strong)',
    )
  })

  it.each(SURFACES)('the edge and the off knob clear 3:1 on %s', (surface) => {
    expectContrast('--kk-control-border', surface, NON_TEXT)
    expect(contrastRatio(knob(), track)).toBeGreaterThanOrEqual(NON_TEXT)
  })

  it.each(SURFACES)('a disabled one still clears 3:1 on %s', (surface) => {
    const under = colour(token(surface))
    const edge = faded(colour(token('--kk-control-border-strong')), under)
    expect(
      contrastRatio(edge, under),
      `disabled edge ${toHex(edge)} on ${toHex(under)}`,
    ).toBeGreaterThanOrEqual(NON_TEXT)
    const knobFaded = faded(knob(), under)
    const trackFaded = faded(track, under)
    expect(
      contrastRatio(knobFaded, trackFaded),
      `disabled knob ${toHex(knobFaded)} on ${toHex(trackFaded)}`,
    ).toBeGreaterThanOrEqual(NON_TEXT)
  })
})

describe('text over a photograph', () => {
  // A label floating over a photo sits on a translucent plate of the shade; the
  // photo under it is unknown, so it is measured over the worst case — white.
  const white: Rgba = { r: 255, g: 255, b: 255, a: 1 }
  const plates = ['--kk-photo-plate', '--kk-photo-plate-strong']

  it.each(plates)('the light glyph colour reads on %s over a white photo', (plate) => {
    const under = composite(colour(token(plate)), white)
    expect(contrastRatio(colour(token('--kk-on-photo')), under)).toBeGreaterThanOrEqual(TEXT)
  })

  it('keeps the tile caption scrim dark enough at its base', () => {
    const base = composite(colour('color-mix(in srgb, var(--kk-shade) 72%, transparent)'), white)
    expect(contrastRatio(colour(token('--kk-on-photo')), base)).toBeGreaterThanOrEqual(TEXT)
  })
})
