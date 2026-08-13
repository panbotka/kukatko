import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

/**
 * The timeline rail is `position: fixed` at the right edge of the viewport; the
 * page's content is a Bootstrap container whose `max-width` steps at each
 * breakpoint. Between two steps the container keeps growing with the window
 * while its max-width does not, so its right edge walks under the rail — and the
 * rail, being on top, took the clicks that belonged to the last tile in a row and
 * to the buttons above the grid. Measured on production before the fix: 56px of
 * overlap at a 1000px window, 44 at 1024, 46 at 1200, 36 at 1400.
 *
 * The fix reserves the rail's lane out of the page's own width (`.kukatko-page`),
 * so the container is laid out in what is left instead of being trusted to fit
 * beside it. jsdom evaluates no media queries and lays nothing out, so this guard
 * reads the shipped stylesheet, rebuilds the container geometry from it, and
 * sweeps the widths — one width would have been passed by the old CSS too, since
 * the bug is only present between breakpoints.
 */

const REM_PX = 16

/** Resolves the `4.25rem` / `68px` length forms these rules use. */
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

/**
 * Bootstrap 5's `.container` steps: the viewport width at which each one starts,
 * and the max-width it pins the container to from there. This is the shape that
 * makes the bug: the container's width is a staircase while the window's is a
 * ramp, so the gap between them collapses to nothing just above every step.
 */
const CONTAINER_STEPS: readonly { from: number; maxWidth: number }[] = [
  { from: 576, maxWidth: 540 },
  { from: 768, maxWidth: 720 },
  { from: 992, maxWidth: 960 },
  { from: 1200, maxWidth: 1140 },
  { from: 1400, maxWidth: 1320 },
]

/** A classic (space-taking) scrollbar, which the library grid always has. */
const SCROLLBAR_PX = 15

/**
 * The container's right edge for a window of `windowPx` when the page reserves
 * `lanePx` on its right, plus the right edge of the viewport itself — the fixed
 * rail hangs off that one, so every edge the sweep compares against is measured
 * back from it.
 *
 * The container is `width: 100%` with a stepped `max-width` and auto margins, so
 * it fills what is left of the page and centres in it.
 */
function edges(windowPx: number, lanePx: number): { containerRight: number; available: number } {
  const available = windowPx - SCROLLBAR_PX
  const step = [...CONTAINER_STEPS].reverse().find((candidate) => windowPx >= candidate.from)
  const pageWidth = available - lanePx
  const containerWidth = Math.min(step?.maxWidth ?? pageWidth, pageWidth)
  return { containerRight: (pageWidth - containerWidth) / 2 + containerWidth, available }
}

describe('timeline lane', () => {
  const css = readCss('src/styles/app.css')
  const root = declarations(ruleBody(css, /:root\s*(?=\{)/) ?? '')
  const railWidth = lengthPx(root.get('--kk-timeline-rail-width'))
  const bubbleLane = lengthPx(root.get('--kk-timeline-bubble-width'))
  const lane = railWidth + bubbleLane

  it('reserves the rail and its bubble out of the page, from `sm` up', () => {
    // The lane is the sum of the two, so widening either one widens what the
    // page holds open for it — they cannot drift apart.
    expect(root.get('--kk-timeline-lane')).toBe(
      'calc(var(--kk-timeline-rail-width) + var(--kk-timeline-bubble-width))',
    )

    // The reservation follows the rail being mounted, not a page claiming it
    // renders one: the rail declines to render for a timeline that is loading,
    // empty or too short in time, and the lane must go with it.
    const sm = ruleBody(css, /@media\s*\(min-width:\s*576px\)/, /\.kukatko-page/) ?? ''
    const page = declarations(
      ruleBody(sm, /\.kukatko-page:has\(\.kukatko-timeline\)\s*(?=\{)/) ?? '',
    )
    expect(page.get('padding-right')).toBe(
      'calc(var(--kk-timeline-lane) + env(safe-area-inset-right, 0px))',
    )

    // And the rail is exactly as wide as the half of the lane named after it.
    const rail = declarations(ruleBody(css, /\.kukatko-timeline\s*(?=\{)/) ?? '')
    expect(rail.get('width')).toBe('var(--kk-timeline-rail-width)')

    // Neither half may be trimmed below what it holds: the rail has to contain a
    // four-digit year label with its mark, and the bubble the widest month label
    // any locale prints — `May 2026`, measured at 72px — plus its own margin. A
    // lane shorter than its contents is the bug again, in miniature.
    expect(railWidth).toBeGreaterThanOrEqual(68)
    expect(bubbleLane).toBeGreaterThanOrEqual(72 + lengthPx('0.75rem'))

    // The bubble hangs to the left of the rail (`right: 100%`) with its own
    // margin, and is capped so that margin plus box cannot exceed its half of
    // the lane whatever a locale prints in it.
    const bubble = declarations(ruleBody(css, /\.kukatko-timeline-current\s*(?=\{)/) ?? '')
    expect(bubble.get('right')).toBe('100%')
    expect(bubble.get('margin-right')).toBe('var(--kk-space-3)')
    expect(bubble.get('max-width')).toBe(
      'calc(var(--kk-timeline-bubble-width) - var(--kk-space-3))',
    )
  })

  it('reproduces the overlap the unreserved layout had', () => {
    // The model is only worth sweeping if it describes the real page, so pin it
    // against what was measured in the browser before the fix (px counted with
    // `document.elementFromPoint`, hence the one-pixel tolerance).
    const measured = new Map([
      [1000, 56],
      [1024, 44],
      [1100, 6],
      [1200, 46],
      [1250, 20],
      [1400, 36],
      [1440, 16],
    ])
    for (const [windowPx, overlap] of measured) {
      const { containerRight, available } = edges(windowPx, 0)
      expect(Math.abs(containerRight - (available - railWidth) - overlap)).toBeLessThanOrEqual(1)
    }
    // And on the widths that were already clean, so the model does not simply
    // predict overlap everywhere.
    for (const windowPx of [1292, 1471, 1600, 1920]) {
      const { containerRight, available } = edges(windowPx, 0)
      expect(containerRight).toBeLessThanOrEqual(available - railWidth + 1)
    }
  })

  it('keeps the content clear of the rail at every desktop width', () => {
    // Every width from the smallest the reservation applies at up to 4K, not the
    // one that happened to be checked by hand: the bug lived between breakpoints,
    // so a single width proves nothing.
    const failures: number[] = []
    for (let windowPx = 576; windowPx <= 3840; windowPx += 1) {
      const { containerRight, available } = edges(windowPx, lane)
      if (containerRight > available - lane) {
        failures.push(windowPx)
      }
    }
    expect(failures).toEqual([])

    // The widths the report named, spelled out, so a regression says which one
    // it reached. The bubble's left edge is the nearer of the two — the rail's
    // own sits another `bubbleLane` to the right of it.
    for (const windowPx of [1000, 1024, 1100, 1200, 1250, 1292, 1400, 1440, 1512, 1920]) {
      const { containerRight, available } = edges(windowPx, lane)
      const bubbleLeft = available - railWidth - bubbleLane
      expect({ windowPx, clear: containerRight <= bubbleLeft }).toEqual({ windowPx, clear: true })
    }
  })

  it('leaves the phone layout alone', () => {
    // A phone reserves the lane on the grid instead — the rail there is narrower,
    // starts at the grid's own top edge, and the page cannot spare the width in
    // its header rows. The `sm` floor on the page rule is what keeps the two
    // arrangements apart.
    const phone = ruleBody(css, /@media\s*\(max-width:\s*575\.98px\)/, /\.kukatko-timeline\s/) ?? ''
    expect(phone).not.toContain('.kukatko-page')
    const grid = declarations(ruleBody(phone, /\.kukatko-grid-timeline-lane\s*(?=\{)/) ?? '')
    expect(lengthPx(grid.get('padding-right'))).toBe(40)
    const rail = declarations(ruleBody(phone, /\.kukatko-timeline\s*(?=\{)/) ?? '')
    expect(lengthPx(rail.get('width'))).toBe(40)
  })
})
