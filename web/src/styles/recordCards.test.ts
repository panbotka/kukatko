import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The phone form of a wide admin table (`components/RecordTable.tsx`) leans on two
 * rules that jsdom can never exercise — it evaluates no media queries and loads no
 * Bootstrap — so the guards read `app.css` and assert the declarations themselves.
 *
 * What they pin: the card's action row clears the finger-target floor (the whole
 * reason a phone gets cards instead of a sideways-scrolling table), and the audit
 * log's raw JSON payload is confined to its own box instead of setting the width
 * of the listing around it.
 */

const REM_PX = 16
/** The finger-friendly minimum a touch target has to clear. */
const TOUCH_FLOOR_PX = 44

/** Resolves the `2.75rem` / `44px` length forms these rules use. */
function lengthPx(value: string | undefined): number {
  if (value === undefined) {
    throw new Error('length is not declared')
  }
  const match = /^(-?[\d.]+)(rem|px)?$/.exec(value)
  if (match === null) {
    throw new Error(`unsupported length: ${value}`)
  }
  return Number(match[1]) * (match[2] === 'rem' ? REM_PX : 1)
}

describe('stacked record cards', () => {
  const css = readCss('src/styles/app.css')

  it('lifts the card action row to the finger-target floor', () => {
    const actions = declarations(ruleBody(css, /\.kk-record-card__actions\s+\.btn\s*(?=\{)/) ?? '')
    // Unconditional, not inside a `pointer: coarse` block: a narrow window on a
    // laptop gets the same card, and a `size="sm"` button in it is a worse target
    // than the table row the card replaced.
    expect(lengthPx(actions.get('min-height'))).toBeGreaterThanOrEqual(TOUCH_FLOOR_PX)
  })

  it('confines the audit payload to its own box', () => {
    const payload = declarations(ruleBody(css, /\.kk-audit-payload\s*(?=\{)/) ?? '')
    // Pretty-printed JSON has no soft break opportunities, so an unwrapped <pre>
    // would set the scroll width of the whole listing.
    expect(payload.get('white-space')).toBe('pre-wrap')
    expect(payload.get('overflow-wrap')).toBe('anywhere')
    // And the one token too long to break scrolls inside the block, not with it.
    expect(payload.get('max-width')).toBe('100%')
    expect(payload.get('overflow-x')).toBe('auto')
  })
})
