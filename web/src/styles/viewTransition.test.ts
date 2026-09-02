import { describe, expect, it } from 'vitest'

import { MORPH_NAME } from '../lib/viewTransition'
import { declarations, readCss, ruleBody } from '../test/css'

/**
 * jsdom evaluates no `::view-transition-*` pseudo elements — it has no
 * compositor and implements none of the API — so the choreography of the
 * grid ⇄ viewer morph cannot be observed from a mounted tree at all. These
 * assertions read the stylesheet instead and pin the three things that would
 * silently break the effect: the mark that names the pair, a duration built on
 * the motion tokens (which collapse under prefers-reduced-motion), and the
 * `object-fit` that keeps two differently-cropped snapshots of one photograph
 * from squashing into each other's proportions.
 */
describe('the morph stylesheet', () => {
  const css = readCss('src/styles/viewTransition.css')

  it('names the marked element with the name the runner pairs on', () => {
    const marked = declarations(ruleBody(css, /\[data-kk-morph\]\s*(?=\{)/) ?? '')

    expect(marked.get('view-transition-name')).toBe(MORPH_NAME)
  })

  it('times the morph off the motion tokens, which reduced motion zeroes', () => {
    const group = declarations(ruleBody(css, /::view-transition-group\(kk-morph\)\s*(?=\{)/) ?? '')

    expect(group.get('animation-duration')).toBe('var(--kk-duration-base)')
  })

  it('keeps both snapshots in the photograph’s own proportions', () => {
    const pair = declarations(
      ruleBody(
        css,
        /::view-transition-old\(kk-morph\),\s*::view-transition-new\(kk-morph\)\s*(?=\{)/,
      ) ?? '',
    )

    expect(pair.get('object-fit')).toBe('cover')
    // The default `plus-lighter` washes an overlapping pair of photographs out
    // to white halfway through the cross-fade; these two overlap by design.
    expect(pair.get('mix-blend-mode')).toBe('normal')
  })

  it('stops every morph animation under prefers-reduced-motion', () => {
    const reduced = ruleBody(css, /@media \(prefers-reduced-motion: reduce\)\s*(?=\{)/)

    expect(reduced).toContain('::view-transition-group(kk-morph)')
    expect(reduced).toContain('animation: none')
  })
})
