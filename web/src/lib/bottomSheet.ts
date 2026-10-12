import { type SheetState } from './mapView'

/**
 * How tall the peeking sheet is, in CSS pixels: the grab handle, the header
 * line with the count, and one strip of thumbnails (see `MapAreaPanel`). Enough
 * to say what is in the area and offer the first photos without hiding the map.
 */
export const SHEET_PEEK_PX = 148

/** Map left showing above the fully raised sheet, so it still reads as a sheet over a map. */
export const SHEET_FULL_GAP_PX = 24

/**
 * A release faster than this (px/ms, ~one screen in a second) is a flick: it
 * moves the sheet one stop in the flick's direction regardless of where it was
 * let go, the way a native sheet answers a quick swipe.
 */
export const SHEET_FLICK_VELOCITY = 0.5

/** The heights (CSS px) of the sheet's three stops in a stage of a given height. */
export interface SheetStops {
  peek: number
  half: number
  full: number
}

/** Stops in their order of height. */
const ORDER: readonly SheetState[] = ['peek', 'half', 'full']

/**
 * The sheet's three heights inside a stage `stageHeight` tall: peeking at a
 * fixed height, half the stage, and the whole stage bar a sliver of map. On a
 * stage too short for the three to differ, they collapse onto each other
 * rather than invert.
 */
export function sheetStops(stageHeight: number, peek = SHEET_PEEK_PX): SheetStops {
  const usable = Math.max(0, stageHeight)
  const peekHeight = Math.min(peek, usable)
  const full = Math.max(peekHeight, usable - SHEET_FULL_GAP_PX)
  const half = Math.min(full, Math.max(peekHeight, Math.round(usable / 2)))
  return { peek: peekHeight, half, full }
}

/** The height of a stop. */
export function sheetHeight(state: SheetState, stops: SheetStops): number {
  return stops[state]
}

/** The stop after `state` when the handle is pressed: up and up, then back down. */
export function nextSheetState(state: SheetState): SheetState {
  switch (state) {
    case 'peek':
      return 'half'
    case 'half':
      return 'full'
    default:
      return 'peek'
  }
}

/**
 * Where a dragged sheet comes to rest. `height` is where it was let go;
 * `velocity` is how fast it was moving then, in px/ms with **positive meaning
 * growing** (dragged upwards). A flick moves one stop past the last stop the
 * sheet had cleared in its direction; a slow release settles on the nearest
 * stop.
 */
export function snapSheet(height: number, velocity: number, stops: SheetStops): SheetState {
  if (Math.abs(velocity) >= SHEET_FLICK_VELOCITY) {
    if (velocity > 0) {
      return ORDER.find((state) => stops[state] > height) ?? 'full'
    }
    return [...ORDER].reverse().find((state) => stops[state] < height) ?? 'peek'
  }
  let best: SheetState = 'peek'
  let bestDistance = Number.POSITIVE_INFINITY
  for (const state of ORDER) {
    const distance = Math.abs(stops[state] - height)
    if (distance < bestDistance) {
      best = state
      bestDistance = distance
    }
  }
  return best
}
