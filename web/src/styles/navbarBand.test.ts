import { describe, expect, it } from 'vitest'

import { NAV_DRAWER_QUERY } from '../hooks/useIsNarrowViewport'
import { blockBodyAt, declarations, readCss, ruleBody } from '../test/css'

/**
 * The tight desktop band, where the inline navigation bar comes back but the
 * `.container` around it is at its narrowest.
 *
 * Below `lg` the shell folds the bar into the drawer, so nothing there can
 * overflow. From `lg` the bar returns into a 960px container, and a maintainer's
 * row of ten labelled items measures ~958px before the user menu has spelled out
 * anything longer than "admin" — it fits by accident, and a real display name
 * pushes the menu back off the edge (1006px for "Tomáš Kozák", measured on the
 * staging box). The stylesheet therefore drops the bar's decorative glyphs for
 * this one band, which buys ~170px and costs nothing: every icon in the bar is
 * `aria-hidden` beside a label that names the destination.
 *
 * The two halves of that arrangement live in different files — the breakpoint in
 * `useIsNarrowViewport.ts`, the diet in `app.css` — so the guards below check
 * they still meet. A drawer breakpoint moved without the band (or the other way
 * round) would leave a strip of widths with an inline bar, its icons and no room
 * for them, which is exactly the bug this replaced.
 */

const css = readCss('src/styles/app.css')

/** The largest width the navigation is still folded into the drawer at. */
const drawerMax = Number(/max-width:\s*([\d.]+)px/.exec(NAV_DRAWER_QUERY)?.[1])

/** The first width the inline bar is rendered at, i.e. where the band opens. */
const barMin = Math.ceil(drawerMax)

/**
 * The `min-width` (in px) of the first `@media` block whose prelude matches
 * `prelude` and whose body satisfies `contains` — the prelude of the same block
 * `ruleBody` would return, which is what makes the number checkable.
 */
function mediaMin(prelude: RegExp, contains: RegExp): number {
  const scan = new RegExp(prelude.source, 'g')
  let match = scan.exec(css)
  while (match !== null) {
    if (contains.test(blockBodyAt(css, match.index + match[0].length))) {
      return Number(/min-width:\s*([\d.]+)px/.exec(match[0])?.[1])
    }
    match = scan.exec(css)
  }
  throw new Error(`no @media block matching ${prelude.source} contains ${contains.source}`)
}

describe('the navbar band between the drawer and a roomy container', () => {
  it('opens exactly where the drawer breakpoint closes', () => {
    // 991.98 → 992: the first width with an inline bar is the first width on a
    // diet. No gap, no overlap.
    expect(barMin).toBe(992)
    expect(mediaMin(/@media \(min-width: [\d.]+px\) and \(max-width: [\d.]+px\)/, /\.bi/)).toBe(
      barMin,
    )
  })

  it("drops the bar's glyphs, not its labels", () => {
    const band = ruleBody(css, /@media \(min-width: 992px\) and \(max-width: 1199\.98px\)/, /\.bi/)
    expect(band).toBeDefined()
    expect(
      declarations(ruleBody(band ?? '', /\.kukatko-navbar \.nav-link > \.bi/) ?? '').get('display'),
    ).toBe('none')
    // The words are what make the destinations findable (see `navItems`), so
    // nothing here may hide the links themselves or shrink them into glyphs.
    expect(band).not.toMatch(/font-size:\s*0/)
    expect(band).not.toMatch(/\.nav-link\s*\{[^}]*display:\s*none/)
  })

  it('flips the search trigger to the head of the bar at the same width', () => {
    // Below the breakpoint the trigger hugs the trailing edge, paired with the
    // hamburger; from it, it leads the bar. Keyed to the drawer breakpoint, or
    // the collapsed row grows a hole between its two controls.
    expect(mediaMin(/@media \(min-width: [\d.]+px\)(?! and)/, /\.kukatko-search-trigger/)).toBe(
      barMin,
    )
  })
})
