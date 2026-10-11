import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The trash's permanent-delete panel stacks on a phone and lies on one line
 * from `md` up. jsdom applies no stylesheet and has no viewport, so the page
 * test can only prove the labelled field and the buttons exist; that the label
 * sits above the field, that "Vyprázdnit koš" spans a phone's width and that the
 * field is never shrunk below the 16 px iOS zooms into is read from the CSS.
 */

const app = readCss('src/styles/app.css')

/** The phone rule (mobile-first: the base, outside any media query). */
function base(selector: string): Map<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return declarations(ruleBody(app, new RegExp(`(?:^|\\n)${escaped}\\s*(?=\\{)`)) ?? '')
}

/** The same selector's rule inside the `md`+ block. */
function desktop(selector: string): Map<string, string> {
  const block = ruleBody(app, /@media \(min-width: 768px\)\s*(?=\{)/, /\.kk-trash-purge/)
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return declarations(ruleBody(block ?? '', new RegExp(`${escaped}\\s*(?=\\{)`)) ?? '')
}

describe('trash purge panel', () => {
  it('puts the label above the field and the empty-trash button full width on a phone', () => {
    expect(base('.kk-trash-purge').get('flex-direction')).toBe('column')
    expect(base('.kk-trash-purge__older').get('flex-direction')).toBe('column')
    // Field, unit and button stay one row under the label rather than wrapping.
    expect(base('.kk-trash-purge__controls').get('flex-wrap')).toBe('nowrap')
    expect(base('.kk-trash-purge__days').get('flex-wrap')).toBe('nowrap')
    expect(base('.kk-trash-purge__empty').get('width')).toBe('100%')
  })

  it('keeps the day count at 16 px so a phone does not zoom into it', () => {
    expect(base('.kk-trash-purge__days .form-control').get('font-size')).toBe('1rem')
  })

  it('lays the panel out as one line with empty trash ranged right from md up', () => {
    expect(desktop('.kk-trash-purge').get('flex-direction')).toBe('row')
    expect(desktop('.kk-trash-purge__older').get('flex-direction')).toBe('row')
    const empty = desktop('.kk-trash-purge__empty')
    expect(empty.get('width')).toBe('auto')
    expect(empty.get('margin-inline-start')).toBe('auto')
  })
})
