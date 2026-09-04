import { describe, expect, it } from 'vitest'

import {
  DEFAULT_TILE_RATIO,
  justifiedRows,
  LAST_ROW_MAX_STRETCH,
  MAX_TILE_RATIO,
  MIN_TILE_RATIO,
  rowHeightForColumns,
  rowOfTile,
  tileRatio,
} from './justifiedLayout'

/** The width a row actually occupies, gutters included. */
function rowWidth(widths: readonly number[], gap: number): number {
  return widths.reduce((sum, w) => sum + w, 0) + gap * (widths.length - 1)
}

describe('tileRatio', () => {
  it('is width over height for an unrotated photo', () => {
    expect(tileRatio(3000, 2000, 1)).toBeCloseTo(1.5)
  })

  it('swaps the sides for a quarter-turn orientation', () => {
    // The thumbnail has the rotation baked in, so a 3000×2000 file tagged 6 is
    // shown portrait and must be laid out as one.
    expect(tileRatio(3000, 2000, 6)).toBeCloseTo(2 / 3)
  })

  it('falls back to the default for unusable dimensions', () => {
    expect(tileRatio(0, 0)).toBe(DEFAULT_TILE_RATIO)
    expect(tileRatio(-4, 3)).toBe(DEFAULT_TILE_RATIO)
    expect(tileRatio(Number.NaN, 100)).toBe(DEFAULT_TILE_RATIO)
  })

  it('clamps a panorama and a strip alike', () => {
    expect(tileRatio(10000, 1000)).toBe(MAX_TILE_RATIO)
    expect(tileRatio(1000, 10000)).toBe(MIN_TILE_RATIO)
  })
})

describe('rowHeightForColumns', () => {
  it('reads a column count as that many landscape photos across', () => {
    // Five tiles of (1000 - 4×3)/5 = 197.6 px, each a 3:2 landscape.
    expect(rowHeightForColumns(1000, 5, 3)).toBeCloseTo(197.6 / 1.5, 1)
  })

  it('never returns a non-positive height', () => {
    expect(rowHeightForColumns(0, 5, 3)).toBeGreaterThan(0)
    expect(rowHeightForColumns(-100, 5, 3)).toBeGreaterThan(0)
    expect(rowHeightForColumns(500, 0, 3)).toBeGreaterThan(0)
  })
})

describe('justifiedRows', () => {
  const options = { containerWidth: 1000, targetRowHeight: 200, gap: 4 }

  it('fills every full row to exactly the container width', () => {
    const ratios = Array.from({ length: 40 }, (_, i) => [1.5, 0.75, 1.0, 2.0][i % 4] ?? 1.5)
    const rows = justifiedRows(ratios, options)
    expect(rows.length).toBeGreaterThan(1)
    for (const row of rows.slice(0, -1)) {
      expect(
        rowWidth(
          row.tiles.map((t) => t.width),
          options.gap,
        ),
      ).toBe(options.containerWidth)
    }
  })

  it('tiles the whole run once, in order', () => {
    const ratios = Array.from({ length: 37 }, (_, i) => 0.6 + (i % 5) * 0.4)
    const rows = justifiedRows(ratios, options)
    expect(rows.flatMap((row) => row.tiles.map((t) => t.index))).toEqual(ratios.map((_, i) => i))
    for (const row of rows) {
      expect(row.tiles[0]?.index).toBe(row.start)
    }
  })

  it('keeps every row near the target height', () => {
    const ratios = Array.from({ length: 60 }, (_, i) => (i % 3 === 0 ? 0.75 : 1.5))
    const rows = justifiedRows(ratios, options)
    for (const row of rows.slice(0, -1)) {
      expect(row.height).toBeGreaterThan(options.targetRowHeight * 0.5)
      expect(row.height).toBeLessThan(options.targetRowHeight * 1.6)
    }
  })

  it('gives a wide photo more room than a tall one in the same row', () => {
    const rows = justifiedRows([2, 0.5, 1.5, 1.5, 1.5, 1.5], options)
    const [wide, tall] = rows[0].tiles
    expect(wide.width).toBeGreaterThan(tall.width)
  })

  it('does not stretch a lone leftover photo across the wall', () => {
    // Three landscapes fill a row at this width; the fourth is left over on its
    // own and would have to be 1000 px wide (666 tall) to justify.
    const rows = justifiedRows(
      Array.from({ length: 4 }, () => 1.5),
      options,
    )
    const last = rows.at(-1)
    expect(last?.tiles).toHaveLength(1)
    expect(last?.height).toBe(options.targetRowHeight)
    expect(last?.tiles[0]?.width).toBeLessThan(options.containerWidth / 2)
  })

  it('justifies a last row that is nearly full', () => {
    const ratios = Array.from({ length: 9 }, () => 1.5)
    const rows = justifiedRows(ratios, options)
    const last = rows.at(-1)
    expect(last).toBeDefined()
    if (last !== undefined && last.tiles.length > 1) {
      expect(last.height).toBeLessThanOrEqual(options.targetRowHeight * LAST_ROW_MAX_STRETCH)
      expect(
        rowWidth(
          last.tiles.map((t) => t.width),
          options.gap,
        ),
      ).toBe(options.containerWidth)
    }
  })

  it('treats an unusable ratio as the default box rather than breaking its row', () => {
    // Every unusable ratio is laid out as the default 3:2 box, so the four sit
    // in the same rows four ordinary landscapes would.
    const rows = justifiedRows([Number.NaN, 0, -1, 1.5], options)
    const tiles = rows.flatMap((row) => row.tiles)
    expect(tiles.map((t) => t.index)).toEqual([0, 1, 2, 3])
    expect(tiles.every((t) => t.width > 0)).toBe(true)
    expect(rows[0].tiles.map((t) => t.width)).toEqual(
      justifiedRows([1.5, 1.5, 1.5, 1.5], options)[0].tiles.map((t) => t.width),
    )
  })

  it('lays a phone-width wall out without collapsing', () => {
    const narrow = { containerWidth: 393, targetRowHeight: 86, gap: 3 }
    const rows = justifiedRows(
      Array.from({ length: 12 }, () => 1.5),
      narrow,
    )
    expect(rows.length).toBeGreaterThan(1)
    for (const row of rows.slice(0, -1)) {
      expect(
        rowWidth(
          row.tiles.map((t) => t.width),
          narrow.gap,
        ),
      ).toBe(narrow.containerWidth)
      expect(row.height).toBeGreaterThan(0)
    }
  })

  it('never puts more photos in a row than the cap allows', () => {
    // A phone's row of portraits: at the target height alone it holds six 53px
    // tiles, which is the defect the cap exists for.
    const narrow = { containerWidth: 335, targetRowHeight: 73, gap: 3 }
    const portraits = Array.from({ length: 24 }, () => 0.75)
    expect(Math.max(...justifiedRows(portraits, narrow).map((r) => r.tiles.length))).toBe(6)

    const capped = justifiedRows(portraits, { ...narrow, maxTilesPerRow: 3 })
    expect(capped.length).toBeGreaterThan(1)
    for (const row of capped) {
      expect(row.tiles.length).toBeLessThanOrEqual(3)
    }
    // Fewer photos in the row means each of them is bigger, which is the point.
    expect(capped[0].height).toBeGreaterThan(73)
    expect(capped[0].tiles[0].width).toBeGreaterThan(100)
  })

  it('still fills a capped row edge to edge, in order', () => {
    const narrow = { containerWidth: 335, targetRowHeight: 73, gap: 3, maxTilesPerRow: 3 }
    const rows = justifiedRows(
      Array.from({ length: 13 }, (_, i) => [0.75, 1.5, 1.0][i % 3] ?? 1),
      narrow,
    )
    expect(rows.flatMap((row) => row.tiles.map((t) => t.index))).toEqual(
      Array.from({ length: 13 }, (_, i) => i),
    )
    for (const row of rows.slice(0, -1)) {
      expect(
        rowWidth(
          row.tiles.map((t) => t.width),
          narrow.gap,
        ),
      ).toBe(narrow.containerWidth)
    }
  })

  it('leaves a row the greedy rule would close early alone', () => {
    // Two panoramas already overshoot the target at this width, so the cap of
    // three never comes into it: the row closes exactly where it did before.
    const narrow = { containerWidth: 335, targetRowHeight: 73, gap: 3 }
    const panoramas = Array.from({ length: 9 }, () => 3)
    expect(justifiedRows(panoramas, { ...narrow, maxTilesPerRow: 3 })).toEqual(
      justifiedRows(panoramas, narrow),
    )
  })

  it('imposes no ceiling without a usable cap', () => {
    const ratios = Array.from({ length: 40 }, (_, i) => [1.5, 0.75, 1.0, 2.0][i % 4] ?? 1.5)
    const plain = justifiedRows(ratios, options)
    for (const cap of [undefined, Number.NaN, 0, -3]) {
      expect(justifiedRows(ratios, { ...options, maxTilesPerRow: cap })).toEqual(plain)
    }
  })

  it('returns nothing for an unmeasured container', () => {
    expect(justifiedRows([1.5, 1.5], { ...options, containerWidth: 0 })).toEqual([])
    expect(justifiedRows([1.5, 1.5], { ...options, targetRowHeight: 0 })).toEqual([])
    expect(justifiedRows([], options)).toEqual([])
  })
})

describe('rowOfTile', () => {
  const rows = justifiedRows(
    Array.from({ length: 50 }, (_, i) => (i % 4 === 0 ? 0.7 : 1.5)),
    { containerWidth: 1000, targetRowHeight: 200, gap: 4 },
  )

  it('finds the row holding each tile', () => {
    for (const [index, row] of rows.entries()) {
      for (const tile of row.tiles) {
        expect(rowOfTile(rows, tile.index)).toBe(index)
      }
    }
  })

  it('reports -1 outside the laid-out run', () => {
    expect(rowOfTile(rows, -1)).toBe(-1)
    expect(rowOfTile(rows, 50)).toBe(-1)
    expect(rowOfTile([], 0)).toBe(-1)
  })
})
