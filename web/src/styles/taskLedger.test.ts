import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The ledger's promises are about layout, which jsdom cannot compute: the row
 * is a grid of five columns, the whole line opens the photograph through the
 * title's stretched link with the selection button layered above it, and a
 * phone stacks the row rather than scrolling it sideways. These guards read the
 * shipped stylesheet for those facts.
 */

const app = readCss('src/styles/app.css')

/** The phone variant of the row: the `@media` block that restacks it. */
const phone = ruleBody(app, /@media \(max-width: 767\.98px\)\s*(?=\{)/, /\.kk-ledger-row\s*\{/)

describe('task ledger rows', () => {
  it('lays a row out as one line of five columns on a wide screen', () => {
    const row = declarations(ruleBody(app, /\.kk-ledger-row\s*(?=\{)/) ?? '')
    expect(row.get('display')).toBe('grid')
    expect(row.get('grid-template-areas')).toBe("'check thumb title date comment'")
    // The containing block of the title's stretched link.
    expect(row.get('position')).toBe('relative')
    // A grid child that may not force the page wider than its container.
    expect(row.get('min-width')).toBe('0')
  })

  it('keeps the selection button above the stretched link', () => {
    const check = declarations(ruleBody(app, /\.kk-ledger-row__check\s*(?=\{)/) ?? '')
    expect(check.get('position')).toBe('relative')
    // Bootstrap's `.stretched-link::after` sits at z-index 1.
    expect(Number(check.get('z-index'))).toBeGreaterThan(1)
  })

  it('stacks the row on a phone instead of scrolling it sideways', () => {
    expect(phone).toBeDefined()
    const row = declarations(ruleBody(phone ?? '', /\.kk-ledger-row\s*(?=\{)/) ?? '')
    const areas = row.get('grid-template-areas')?.replace(/\s+/g, ' ')
    expect(areas).toBe("'check thumb title' 'check thumb date' 'check thumb comment'")
    // The comment wraps to a clamped block rather than staying one unbreakable line.
    const excerpt = declarations(ruleBody(phone ?? '', /\.kk-ledger-row__excerpt\s*(?=\{)/) ?? '')
    expect(excerpt.get('white-space')).toBe('normal')
    expect(excerpt.get('-webkit-line-clamp')).toBe('2')
  })
})
