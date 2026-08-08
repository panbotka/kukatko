import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * Leaflet ships one light theme and no hooks to retint it, so `app.css` reaches
 * in by Leaflet's own class names. That is a blunt instrument with two ways to
 * go wrong that nothing else would catch: a selector written without the
 * `.kukatko-map` scope repaints every Leaflet instance (and, via
 * `.leaflet-container a`, any link the plugin's rules happen to reach), and a
 * colour written as a literal silently forks the palette away from the tokens.
 *
 * jsdom loads no Leaflet stylesheet and resolves no custom property, so these
 * guards read the rules themselves. They also pin the handful of compound
 * selectors that exist purely to out-specify Leaflet — the bundler emits its
 * stylesheet after this one, so an override that merely ties loses, and the
 * symptom of a regression is a white popup, not a failing test.
 */

/** Every rule/at-rule prelude in a stylesheet, comments stripped. */
function preludes(css: string): string[] {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, '')
  return [...bare.matchAll(/([^{}]+)\{/g)].map((match) => match[1].trim()).filter((p) => p !== '')
}

/** The individual selectors of every rule that targets Leaflet's own chrome. */
function leafletSelectors(css: string): string[] {
  return preludes(css)
    .filter((prelude) => prelude.includes('.leaflet-'))
    .flatMap((prelude) => prelude.split(','))
    .map((selector) => selector.trim())
    .filter((selector) => selector.includes('.leaflet-'))
}

/** Properties whose value decides a colour, and so must come from a token. */
const COLOUR_PROPERTIES = /^(color|background|background-color|box-shadow|border(-[a-z-]+)?)$/

/** A literal colour: what these rules must never contain. */
const COLOUR_LITERAL = /#[0-9a-f]{3}|\brgba?\(|\bhsla?\(|\bwhite\b|\bblack\b/i

describe('Leaflet chrome overrides', () => {
  const css = readCss('src/styles/app.css')

  it('scopes every Leaflet override under the map container', () => {
    const selectors = leafletSelectors(css)
    // A guard over nothing would pass forever; the overrides must actually exist.
    expect(selectors.length).toBeGreaterThan(0)
    for (const selector of selectors) {
      expect(selector.startsWith('.kukatko-map')).toBe(true)
    }
  })

  it('paints the chrome from tokens rather than literal colours', () => {
    const bare = css.replace(/\/\*[\s\S]*?\*\//g, '')
    const rules = [...bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)].filter((match) =>
      match[1].includes('.leaflet-'),
    )
    let checked = 0
    for (const [, prelude, body] of rules) {
      for (const [property, value] of declarations(body)) {
        if (!COLOUR_PROPERTIES.test(property)) {
          continue
        }
        checked += 1
        expect(value, `${prelude.trim()} { ${property} }`).toContain('var(--kk-')
        expect(value, `${prelude.trim()} { ${property} }`).not.toMatch(COLOUR_LITERAL)
      }
    }
    expect(checked).toBeGreaterThan(0)
  })

  it('out-specifies the Leaflet rules that qualify with .leaflet-container', () => {
    // `.leaflet-container .leaflet-control-attribution` and
    // `.leaflet-container a.leaflet-popup-close-button` tie with the obvious
    // descendant override and win on order, so `.kukatko-map` has to sit on the
    // container element itself — it *is* the `.leaflet-container`.
    expect(css).toContain('.kukatko-map.leaflet-container .leaflet-control-attribution')
    expect(css).toContain('.kukatko-map.leaflet-container a.leaflet-popup-close-button')
    // Same for the bar's border, which `.leaflet-touch .leaflet-bar` re-declares.
    expect(css).toContain('.kukatko-map.leaflet-touch .leaflet-bar')
  })

  it('gives the popup bubble and its tip one surface', () => {
    const popup = declarations(
      ruleBody(css, /\.kukatko-map \.leaflet-popup-content-wrapper,/) ?? '',
    )
    expect(popup.get('background')).toBe('var(--kk-surface-overlay)')
    expect(popup.get('color')).toBe('var(--kk-text)')
    // The tip is clipped to two edges; the shared border continues the bubble's
    // outline into the arrow instead of leaving a bare triangle hanging off it.
    expect(popup.get('border')).toContain('var(--kk-surface-border)')
  })

  it('keeps the close button legible at rest and on focus', () => {
    const rest = declarations(
      ruleBody(css, /\.kukatko-map\.leaflet-container a\.leaflet-popup-close-button\s*(?=\{)/) ??
        '',
    )
    expect(rest.get('color')).toBe('var(--kk-text-muted)')

    const active = declarations(
      ruleBody(css, /\.kukatko-map\.leaflet-container a\.leaflet-popup-close-button:hover,/) ?? '',
    )
    expect(active.get('color')).toBe('var(--kk-text)')
  })

  it('leaves the markers alone and touches the tiles only to dim them', () => {
    // The map's *content* keeps its own colours: retinting a tile or a cluster
    // bubble would be recolouring the data, not the chrome. The one rule allowed
    // near the tiles is the dark-page dimming, which substitutes no colour at
    // all and is opt-in per mapset — hence the class in its selector.
    for (const selector of leafletSelectors(css)) {
      expect(selector).not.toContain('.marker-cluster')
      if (selector.includes('.leaflet-tile')) {
        expect(selector).toBe('.kukatko-map--dim-tiles .leaflet-tile-pane')
      }
    }
  })

  it('dims the tile pane with a filter and nothing else', () => {
    const dim = declarations(ruleBody(css, /\.kukatko-map--dim-tiles \.leaflet-tile-pane/) ?? '')
    // A single `filter` — anything else here would be repainting the map rather
    // than turning its brightness down. And it must stay a dimming: `invert()`
    // makes water orange, which is worse than a bright map you can still read.
    expect([...dim.keys()]).toEqual(['filter'])
    expect(dim.get('filter')).toContain('brightness(')
    expect(dim.get('filter')).not.toContain('invert(')
  })
})
