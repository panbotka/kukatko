import { describe, expect, it } from 'vitest'

import {
  nextSheetState,
  SHEET_FULL_GAP_PX,
  SHEET_PEEK_PX,
  sheetHeight,
  sheetStops,
  snapSheet,
} from './bottomSheet'

describe('sheetStops', () => {
  it('peeks at a fixed height, rises to half the stage and to all of it bar a sliver', () => {
    expect(sheetStops(700)).toEqual({
      peek: SHEET_PEEK_PX,
      half: 350,
      full: 700 - SHEET_FULL_GAP_PX,
    })
  })

  it('collapses the stops onto each other on a stage too short for three', () => {
    const stops = sheetStops(100)
    expect(stops.peek).toBe(100)
    expect(stops.half).toBe(100)
    expect(stops.full).toBe(100)
  })

  it('never inverts: half sits between peek and full', () => {
    const stops = sheetStops(200)
    expect(stops.peek).toBeLessThanOrEqual(stops.half)
    expect(stops.half).toBeLessThanOrEqual(stops.full)
  })
})

describe('snapSheet', () => {
  const stops = sheetStops(700) // 148 / 350 / 676

  it('settles a slow release on the nearest stop', () => {
    expect(snapSheet(160, 0, stops)).toBe('peek')
    expect(snapSheet(300, 0.1, stops)).toBe('half')
    expect(snapSheet(600, -0.1, stops)).toBe('full')
  })

  it('carries a flick one stop on in its direction', () => {
    // Barely lifted off the peek, but flicked up: to half.
    expect(snapSheet(170, 1, stops)).toBe('half')
    // Past half and flicked up: to full.
    expect(snapSheet(400, 1, stops)).toBe('full')
    // Just under full, flicked down: to half.
    expect(snapSheet(650, -1, stops)).toBe('half')
    // Below half, flicked down: to peek.
    expect(snapSheet(300, -1, stops)).toBe('peek')
  })

  it('stops at the ends', () => {
    expect(snapSheet(stops.full, 2, stops)).toBe('full')
    expect(snapSheet(stops.peek, -2, stops)).toBe('peek')
  })
})

describe('nextSheetState / sheetHeight', () => {
  it('steps up through the stops and back down to the peek', () => {
    expect(nextSheetState('peek')).toBe('half')
    expect(nextSheetState('half')).toBe('full')
    expect(nextSheetState('full')).toBe('peek')
  })

  it('reads the height of a stop', () => {
    const stops = sheetStops(700)
    expect(sheetHeight('half', stops)).toBe(350)
  })
})
