import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The album index's search field takes a row of its own on a phone, so its
 * placeholder is not cut beside the ordering select. jsdom has no viewport and
 * applies no stylesheet, so the component can only prove it carries the class;
 * that the class means "the whole row, on a phone only" is read here from the
 * shipped CSS.
 */

const app = readCss('src/styles/app.css')

describe('filter search on its own row', () => {
  it('spans the whole row below md', () => {
    const phone = ruleBody(
      app,
      /@media \(max-width: 767\.98px\)\s*(?=\{)/,
      /\.kukatko-filter-search--own-row/,
    )
    const rule = declarations(
      ruleBody(phone ?? '', /\.kukatko-filter-search--own-row\s*(?=\{)/) ?? '',
    )
    expect(rule.get('flex-basis')).toBe('100%')
  })

  it('leaves the desktop row alone', () => {
    // Outside the phone query the modifier declares nothing, so from md up the
    // field shares its row with the ordering exactly as before.
    const outside = app.replace(
      /@media \(max-width: 767\.98px\)\s*\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}/g,
      '',
    )
    expect(outside).not.toContain('.kukatko-filter-search--own-row')
  })
})
