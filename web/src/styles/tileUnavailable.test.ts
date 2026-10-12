import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * A tile whose thumbnail failed shows "no preview available" (`PhotoTile`'s
 * `.kk-tile__unavailable`). Its words used to be centred across the whole tile,
 * so the favourite heart in the bottom-end corner sat on the last of them, and
 * they inherited the tile link's underline. jsdom lays nothing out, so this
 * guard pins what keeps them apart: the fallback's top and bottom insets reserve
 * the select circle's and the heart's full height, read here from the rules that
 * size those controls — growing either without growing the reserve fails here.
 */
describe('the no-preview fallback', () => {
  const tokens = readCss('src/styles/tokens.css')
  const app = readCss('src/styles/app.css')

  /** The declarations of the first rule matching `prelude`, or a failure. */
  function rule(css: string, prelude: RegExp, contains?: RegExp): Map<string, string> {
    const body = ruleBody(css, prelude, contains)
    if (body === undefined) {
      throw new Error(`no rule matches ${prelude.source}`)
    }
    return declarations(body)
  }

  const fallback = rule(tokens, /\.kk-tile__unavailable\s*(?=\{)/)

  it('fills the tile out of flow, as the container its content is fitted to', () => {
    expect(fallback.get('position')).toBe('absolute')
    expect(fallback.get('inset')).toBe('0')
    expect(fallback.get('container')).toBe('kk-tile-unavailable / size')
  })

  it('reserves the select circle above the band and the heart below it', () => {
    const check = rule(tokens, /\.kk-tile__check\s*(?=\{)/).get('height')
    const heart = rule(app, /\n\.kukatko-tap-target\s*(?=\{)/).get('min-height')
    expect(check).toBe('1.65rem')
    expect(heart).toBe('2.75rem')

    const padding = fallback.get('padding')?.replace(/\s+/g, ' ')
    // Each corner control sits `--kk-space-1` in from the edge (`m-1`), so the
    // reserve is that margin, the control and a little air beyond it.
    expect(padding).toBe(
      `calc(var(--kk-space-1) * 1.5 + ${String(check)}) var(--kk-space-2) ` +
        `calc(var(--kk-space-1) * 1.5 + ${String(heart)})`,
    )
  })

  it('shows the words only where two lines of them fit the band', () => {
    expect(rule(tokens, /\.kk-tile__unavailable-text\s*(?=\{)/).get('display')).toBe('none')
    expect(rule(tokens, /\.kk-tile__unavailable-icon\s*(?=\{)/).get('display')).toBe('none')

    const root = rule(tokens, /:root\s*(?=\{)/, /--kk-font-size-caption\s*:/)
    const twoLines =
      2 *
      parseFloat(root.get('--kk-font-size-caption') ?? 'NaN') *
      parseFloat(root.get('--kk-line-height-caption') ?? 'NaN')
    const reveal =
      /@container kk-tile-unavailable \(min-width: 5rem\) and \(min-height: ([\d.]+)rem\)\s*(?=\{)/
    const match = reveal.exec(tokens)
    if (match === null) {
      throw new Error('tokens.css reveals the words under no @container rule')
    }
    expect(Number(match[1])).toBeGreaterThanOrEqual(twoLines)
    // …and a longer translation is cut at those two lines rather than spilling.
    expect(ruleBody(tokens, reveal, /unavailable-text/)).toMatch(
      /\.kk-tile__unavailable-text\s*\{[^}]*-webkit-line-clamp:\s*2/,
    )
  })

  it('never underlines it like a link', () => {
    expect(rule(tokens, /\.kk-tile__media\s*(?=\{)/).get('text-decoration')).toBe('none')
  })
})
