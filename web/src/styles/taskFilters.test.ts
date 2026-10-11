import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The work queue's filter chips are a sideways-scrolling strip on a phone and a
 * wrapping row from `md` up. jsdom applies no stylesheet and has no viewport,
 * so the component test can only prove the strip and its pressed chip exist;
 * that the strip actually scrolls rather than wraps on a narrow screen — and
 * wraps again on a wide one — is a fact about the shipped CSS, read here.
 */

const app = readCss('src/styles/app.css')

/** The phone rule (mobile-first: the base, outside any media query). */
function base(selector: string): Map<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return declarations(ruleBody(app, new RegExp(`(?:^|\\n)${escaped}\\s*(?=\\{)`)) ?? '')
}

/** The same selector's rule inside the `md`+ block. */
function desktop(selector: string): Map<string, string> {
  const block = ruleBody(app, /@media \(min-width: 768px\)\s*(?=\{)/, /\.kk-task-filters__chips/)
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return declarations(ruleBody(block ?? '', new RegExp(`${escaped}\\s*(?=\\{)`)) ?? '')
}

describe('task filter strip', () => {
  it('scrolls the chips sideways on a phone instead of wrapping them', () => {
    const chips = base('.kk-task-filters__chips')
    expect(chips.get('flex-wrap')).toBe('nowrap')
    expect(chips.get('overflow-x')).toBe('auto')
    // No scrollbar: the trailing fade is the hint that there is more.
    expect(chips.get('scrollbar-width')).toBe('none')
    expect(chips.get('mask-image')).toContain('transparent')

    // A chip keeps its width and its one line rather than being squeezed.
    const chip = base('.kk-task-filters__chips > .btn')
    expect(chip.get('flex-shrink')).toBe('0')
    expect(chip.get('white-space')).toBe('nowrap')
  })

  it('stacks the search under the strip on a phone', () => {
    expect(base('.kk-task-filters').get('flex-direction')).toBe('column')
  })

  it('wraps the chips as one row with the search beside them from md up', () => {
    const filters = desktop('.kk-task-filters')
    expect(filters.get('flex-direction')).toBe('row')
    expect(filters.get('flex-wrap')).toBe('wrap')

    const chips = desktop('.kk-task-filters__chips')
    expect(chips.get('flex-wrap')).toBe('wrap')
    expect(chips.get('overflow-x')).toBe('visible')
    expect(chips.get('mask-image')).toBe('none')

    expect(desktop('.kk-task-filters__search').get('margin-inline-start')).toBe('auto')
  })
})
