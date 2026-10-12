import { describe, expect, it } from 'vitest'

import { blockBodyAt, declarations, readCss, ruleBody } from '../test/css'

/**
 * A tile whose thumbnail failed shows "no preview available" (`PhotoTile`'s
 * `.kk-tile__unavailable`). Its words used to be centred across the whole tile,
 * so the favourite heart in the bottom-end corner sat on the last of them, and
 * they inherited the tile link's underline; the first fix then fitted them to
 * the band between the corner controls so strictly that the default ten-column
 * wall's ~117×88 tile showed nothing at all — an empty well, indistinguishable
 * from a tile still loading.
 *
 * jsdom lays nothing out, so this guard does the layout's arithmetic itself: it
 * reads the reserves from the rules that size the corner controls, the fallback's
 * container queries and the glyph's placement out of the shipped stylesheet,
 * evaluates them for concrete tile sizes and checks the glyph's box against the
 * select circle's and the heart's.
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

  /** One declared value, whitespace collapsed, or a failure. */
  function value(decls: Map<string, string>, name: string): string {
    const raw = decls.get(name)
    if (raw === undefined) {
      throw new Error(`no ${name} declared`)
    }
    return raw.replace(/\s+/g, ' ')
  }

  const REM = 16
  const fallback = rule(tokens, /\.kk-tile__unavailable\s*(?=\{)/)
  const body = rule(tokens, /\.kk-tile__unavailable-body\s*(?=\{)/)
  const icon = rule(tokens, /\.kk-tile__unavailable-icon\s*(?=\{)/)
  const text = rule(tokens, /\.kk-tile__unavailable-text\s*(?=\{)/)
  const root = new Map([
    ...rule(tokens, /:root\s*(?=\{)/, /--kk-space-1\s*:/),
    ...rule(tokens, /:root\s*(?=\{)/, /--kk-font-size-caption\s*:/),
  ])
  const vars = new Map([...root, ...fallback])

  /**
   * Evaluates a CSS length — numbers in rem/px/%, `var()`, `calc`/`min`/`max`/
   * `clamp` and `+ - * /` — to pixels, `%` taken of `basis`.
   */
  function px(expr: string, basis: number): number {
    const tokensOf = expr.match(/var\(--[\w-]+\)|[\d.]+(?:rem|px|%)?|[a-z]+\(|[-+*/(),]/g) ?? []
    let at = 0
    const peek = (): string | undefined => tokensOf[at]
    const take = (): string => {
      if (at >= tokensOf.length) {
        throw new Error(`unexpected end of ${expr}`)
      }
      at += 1
      return tokensOf[at - 1]
    }
    const sum = (): number => {
      let acc = product()
      while (peek() === '+' || peek() === '-') {
        acc = take() === '+' ? acc + product() : acc - product()
      }
      return acc
    }
    const product = (): number => {
      let acc = atom()
      while (peek() === '*' || peek() === '/') {
        acc = take() === '*' ? acc * atom() : acc / atom()
      }
      return acc
    }
    const args = (): number[] => {
      const out = [sum()]
      while (take() === ',') {
        out.push(sum())
      }
      return out
    }
    const atom = (): number => {
      const token = take()
      const named = /^var\((--[\w-]+)\)$/.exec(token)
      if (named !== null) {
        const resolved = vars.get(named[1])
        if (resolved === undefined) {
          throw new Error(`no ${named[1]}`)
        }
        return px(resolved, basis)
      }
      const number = /^([\d.]+)(rem|px|%)?$/.exec(token)
      if (number !== null) {
        const n = Number(number[1])
        return number[2] === 'rem' ? n * REM : number[2] === '%' ? (n * basis) / 100 : n
      }
      if (token === '(' || token === 'calc(') {
        const inner = sum()
        take()
        return inner
      }
      const [a, b, c] = args()
      if (token === 'min(') {
        return Math.min(a, b)
      }
      if (token === 'max(') {
        return Math.max(a, b)
      }
      if (token === 'clamp(') {
        return Math.max(a, Math.min(b, c))
      }
      throw new Error(`cannot evaluate ${token} in ${expr}`)
    }
    return sum()
  }

  /** Each `@container kk-tile-unavailable` query: its prelude and its body. */
  const queries = [...tokens.matchAll(/@container kk-tile-unavailable ([^{]+)\{/g)].map((m) => ({
    prelude: m[1].trim(),
    body: blockBodyAt(tokens, m.index),
  }))

  /** Whether a query prelude matches a `width`×`height` tile (the container). */
  function matches(prelude: string, width: number, height: number): boolean {
    return prelude.split(/\s+and\s+/).every((feature) => {
      const min = /^\(min-(width|height): ([\d.]+)rem\)$/.exec(feature)
      if (min !== null) {
        return (min[1] === 'width' ? width : height) >= Number(min[2]) * REM
      }
      const below = /^\((width|height) < ([\d.]+)rem\)$/.exec(feature)
      if (below !== null) {
        return (below[1] === 'width' ? width : height) < Number(below[2]) * REM
      }
      throw new Error(`unknown container feature ${feature}`)
    })
  }

  /** What `selector` computes to on a `width`×`height` tile: base rule, then queries in order. */
  function computed(selector: string, base: Map<string, string>, width: number, height: number) {
    const out = new Map(base)
    for (const query of queries) {
      if (!matches(query.prelude, width, height)) {
        continue
      }
      const inner = ruleBody(query.body, new RegExp(`\\${selector}\\s*(?=\\{)`))
      if (inner !== undefined) {
        for (const [name, v] of declarations(inner)) {
          out.set(name, v)
        }
      }
    }
    return out
  }

  /** The min-width/min-height (rem) of the one query among `qs` whose body matches `layout`. */
  function thresholds(qs: { prelude: string; body: string }[], layout: RegExp): [number, number] {
    const query = qs.find((q) => layout.test(q.body))
    const sizes = /min-width: ([\d.]+)rem\) and \(min-height: ([\d.]+)rem/.exec(
      query?.prelude ?? '',
    )
    if (sizes === null) {
      throw new Error(`no word rung lays out ${layout.source}`)
    }
    return [Number(sizes[1]), Number(sizes[2])]
  }

  const check = value(rule(tokens, /\.kk-tile__check\s*(?=\{)/), 'height')
  const heart = value(rule(app, /\n\.kukatko-tap-target\s*(?=\{)/), 'min-height')
  const margin = px('var(--kk-space-1)', 0)

  it('fills the tile out of flow, as the container its content is fitted to', () => {
    expect(fallback.get('position')).toBe('absolute')
    expect(fallback.get('inset')).toBe('0')
    expect(fallback.get('container')).toBe('kk-tile-unavailable / size')
    // No padding of its own: the container (and the glyph's `%`) is the tile.
    expect(fallback.has('padding')).toBe(false)
  })

  it('reserves the select circle above the band and the heart below it', () => {
    expect(check).toBe('1.65rem')
    expect(heart).toBe('2.75rem')
    // Each corner control sits `--kk-space-1` in from the edge (`m-1`), so the
    // reserve is that margin, the control and a little air beyond it.
    expect(value(fallback, '--kk-tile-unavailable-check')).toBe(
      `calc(var(--kk-space-1) * 1.5 + ${check})`,
    )
    expect(value(fallback, '--kk-tile-unavailable-heart')).toBe(
      `calc(var(--kk-space-1) * 1.5 + ${heart})`,
    )
    expect(value(body, 'padding')).toBe(
      'var(--kk-tile-unavailable-check) var(--kk-space-2) var(--kk-tile-unavailable-heart)',
    )
  })

  /** A box in tile pixels. */
  interface Box {
    top: number
    left: number
    bottom: number
    right: number
  }

  /** Where the glyph is drawn: in the band beside/above the words, or on its own at `Box`. */
  type Glyph = { inBand: true } | ({ inBand: false } & Box)

  /** The glyph on a `width`×`height` tile, or undefined when it is not drawn. */
  function glyph(width: number, height: number): Glyph | undefined {
    const decls = computed('.kk-tile__unavailable-icon', icon, width, height)
    if (decls.get('display') === 'none') {
      return undefined
    }
    const size = px(value(decls, 'font-size'), 0)
    if (decls.get('position') !== 'absolute') {
      return { inBand: true }
    }
    const top = px(value(decls, 'top'), height)
    const left = px(value(decls, 'left'), width)
    return { inBand: false, top, left, bottom: top + size, right: left + size }
  }

  /** Whether `box` stays clear of the select circle and the heart on a `width`×`height` tile. */
  function clearOfControls(box: Box, width: number, height: number): boolean {
    const circle = {
      top: margin,
      left: margin,
      bottom: margin + px(check, 0),
      right: margin + px(check, 0),
    }
    const disc = {
      top: height - margin - px(heart, 0),
      left: width - margin - px(heart, 0),
      bottom: height - margin,
      right: width - margin,
    }
    const apart = (a: Box, b: Box): boolean =>
      a.right <= b.left || b.right <= a.left || a.bottom <= b.top || b.bottom <= a.top
    return apart(box, circle) && apart(box, disc)
  }

  // The default ten-column wall (~117×88 at 1440px, the ~110×80 of a narrower
  // window), its short and its square rungs, a phone tile and a panorama row.
  const glyphOnly: [number, number][] = [
    [110, 80],
    [117, 88],
    [125, 92],
    [144, 108],
    [125, 86],
    [90, 60],
    [300, 80],
    [74, 56],
  ]

  it.each(glyphOnly)('draws the glyph alone on a %i×%i tile, clear of both controls', (w, h) => {
    const box = glyph(w, h)
    expect(box).toBeDefined()
    if (box?.inBand !== false) {
      throw new Error('expected the glyph on its own')
    }
    expect(box.top).toBeGreaterThanOrEqual(0)
    expect(box.left).toBeGreaterThanOrEqual(0)
    expect(box.bottom).toBeLessThanOrEqual(h)
    expect(box.right).toBeLessThanOrEqual(w)
    expect(clearOfControls(box, w, h)).toBe(true)
    expect(computed('.kk-tile__unavailable-text', text, w, h).get('display')).toBe('none')
  })

  it('keeps a tile too narrow for the heart column clear of the heart at least', () => {
    const [w, h] = [60, 130]
    const box = glyph(w, h)
    if (box?.inBand !== false) {
      throw new Error('expected the glyph on its own')
    }
    expect(box.bottom).toBeLessThanOrEqual(h - margin - px(heart, 0))
    expect(box.top).toBeGreaterThanOrEqual(margin + px(check, 0))
  })

  it('shows the words only with the glyph, and only where two lines fit the band', () => {
    // The band is the tile less the two reserves and the side padding.
    const reserveHeight = px(
      'var(--kk-tile-unavailable-check) + var(--kk-tile-unavailable-heart)',
      0,
    )
    const sides = 2 * px('var(--kk-space-2)', 0)
    const twoLines =
      2 *
      parseFloat(value(root, '--kk-font-size-caption')) *
      parseFloat(value(root, '--kk-line-height-caption')) *
      REM
    const glyphSize = px('var(--kk-tile-unavailable-glyph)', 0)

    const revealing = queries.filter((q) => /unavailable-text\s*\{[^}]*display:/.test(q.body))
    expect(revealing).toHaveLength(2)
    for (const query of revealing) {
      // Wherever the words appear, the glyph rejoins them in the band…
      expect(query.body).toMatch(/\.kk-tile__unavailable-icon\s*\{[^}]*position:\s*static/)
      // …and a longer translation is cut at two lines rather than spilling.
      expect(query.body).toMatch(/\.kk-tile__unavailable-text\s*\{[^}]*-webkit-line-clamp:\s*2/)
    }

    // Beside the glyph: a band two lines tall and wide enough for glyph, gap and words.
    const [rowWidth, rowHeight] = thresholds(revealing, /flex-direction:\s*row/)
    expect(rowHeight * REM - reserveHeight).toBeGreaterThanOrEqual(twoLines)
    expect(rowWidth * REM - sides).toBeGreaterThanOrEqual(glyphSize + margin + 4 * REM)

    // Above the glyph: a band tall enough for the glyph, the gap and two lines.
    const [, stackHeight] = thresholds(revealing, /flex-direction:\s*column/)
    expect(stackHeight * REM - reserveHeight).toBeGreaterThanOrEqual(glyphSize + margin + twoLines)

    // Nothing anywhere hides the glyph.
    expect(tokens).not.toMatch(/\.kk-tile__unavailable-icon\s*\{[^}]*display:\s*none/)
  })

  it('never underlines it like a link', () => {
    expect(rule(tokens, /\.kk-tile__media\s*(?=\{)/).get('text-decoration')).toBe('none')
  })
})
