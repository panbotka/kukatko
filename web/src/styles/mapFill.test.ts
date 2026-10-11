import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The map page as the whole screen below `lg`. On a 390x844 phone the map ended
 * around 600px from the top, and the footer plus bare background filled the
 * rest down to the tab bar. `MapPage` now wears `.kk-map-fill` there, and the
 * shell's stylesheet answers with a chain in which every link is load-bearing:
 * the shell sized (not just floored) to the dynamic viewport minus the bottom
 * edge `body` already reserves, each column down to the map flexing with a zero
 * minimum, the footer and the page's bottom gutter gone, and the map's inline
 * `70dvh` overruled by a zero flex basis. jsdom applies no stylesheet, so these
 * read the shipped sheet; the component half is in `MapPage.test.tsx`.
 */

const css = readCss('src/styles/app.css')

/** The declarations of the rule whose selector list is exactly `selector`. */
function rule(selector: RegExp): Map<string, string> {
  const body = ruleBody(css, selector)
  if (body === undefined) {
    throw new Error(`rule not found: ${selector.source}`)
  }
  return declarations(body)
}

describe('the full-viewport map page', () => {
  it('sizes the shell to one screen in dynamic viewport units, minus the taken bottom edge', () => {
    // A height, not a minimum: the page must not scroll, and the bottom edge is
    // the same token `body` pads by, so the two add up to exactly `100dvh`.
    expect(rule(/\.kukatko-shell:has\(\.kk-map-fill\)\s*(?=\{)/).get('height')).toBe(
      'calc(100dvh - var(--kk-bottom-edge))',
    )
  })

  it('flexes every column from the page down to the map stage, each allowed to shrink', () => {
    const chain = rule(
      /\.kukatko-page:has\(\.kk-map-fill\),\s*\.kukatko-main:has\(\.kk-map-fill\),\s*\.kk-map-fill,\s*\.kk-map-fill > \.kk-map-stage\s*(?=\{)/,
    )
    expect(chain.get('display')).toBe('flex')
    expect(chain.get('flex-direction')).toBe('column')
    expect(chain.get('flex')).toBe('1 1 0')
    expect(chain.get('min-height')).toBe('0')
  })

  it('lets the map take what is left instead of its inline 70dvh', () => {
    const map = rule(/\.kk-map-fill > \.kk-map-stage > \.kukatko-map\s*(?=\{)/)
    expect(map.get('flex')).toBe('1 1 0')
    expect(map.get('min-height')).toBe('0')
  })

  it('drops the footer and the bottom gutter, so the map meets the tab bar', () => {
    expect(
      rule(/\.kukatko-page:has\(\.kk-map-fill\) > \.kukatko-footer\s*(?=\{)/).get('display'),
    ).toBe('none')
    expect(rule(/\.kukatko-main:has\(\.kk-map-fill\)\s*(?=\{)/).get('padding-bottom')).toBe('0')
    // Both are the sheet's own (not `d-flex`/`pb-*` utilities, whose
    // `!important` would beat the two rules above — the footer's did, measured).
    expect(rule(/\.kukatko-footer\s*(?=\{)/).get('display')).toBe('flex')
    expect(rule(/\.kukatko-main\s*(?=\{)/).get('padding-bottom')).toBe('var(--kk-space-5)')
  })
})
