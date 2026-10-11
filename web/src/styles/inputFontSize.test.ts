import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * iOS Safari zooms the whole page into a focused text field smaller than 16 px,
 * and Bootstrap's `size="sm"` renders one at 14 px — the task search
 * (`pages/TasksPage.tsx`, `Form.Control size="sm"`) and the upload link's address
 * field (`pages/UploadLinksPage.tsx`, a field inside `InputGroup size="sm"`) both
 * did. jsdom has neither a viewport nor Bootstrap's stylesheet, so the guard reads
 * `app.css`: below `md` and on any touch device every small field is 16 px, and
 * nothing outside that block shrinks it back for a desktop.
 */

const app = readCss('src/styles/app.css')

/** The phone/touch block that lifts the small fields. */
const phone = ruleBody(
  app,
  /@media \(max-width: 767\.98px\), \(pointer: coarse\)\s*(?=\{)/,
  /\.form-control-sm/,
)

/** The declarations of the block's one rule, keyed by its selector list. */
function phoneRule(): { selectors: string[]; decls: Map<string, string> } {
  const match = /([^{}]+)\{([^{}]*)\}/.exec(phone ?? '')
  if (match === null) {
    return { selectors: [], decls: new Map() }
  }
  const selectors = match[1]
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split(',')
    .map((s) => s.replace(/\s+/g, ' ').trim())
  return { selectors, decls: declarations(match[2]) }
}

describe('small form fields on a phone', () => {
  it('ships a phone and touch block for them', () => {
    expect(phone).toBeDefined()
  })

  it('covers every way Bootstrap makes a field small', () => {
    const { selectors } = phoneRule()
    // `Form.Control size="sm"` (the task search) and `Form.Select size="sm"`.
    expect(selectors).toContain('.form-control-sm')
    expect(selectors).toContain('.form-select-sm')
    // A field inside `InputGroup size="sm"` (the upload link's address field).
    expect(selectors).toContain('.input-group-sm > .form-control')
    expect(selectors).toContain('.input-group-sm > .form-select')
  })

  it('sets them to the 16 px iOS does not zoom into', () => {
    expect(phoneRule().decls.get('font-size')).toBe('1rem')
  })

  it('leaves no other rule shrinking a small field', () => {
    // Desktop keeps Bootstrap's compact size by inheriting it, never through an
    // app rule that would also win on a phone.
    const shrinking = /\.(?:form-control-sm|form-select-sm|input-group-sm)[^{}]*\{[^{}]*font-size/g
    expect(app.match(shrinking) ?? []).toHaveLength(1)
  })
})
