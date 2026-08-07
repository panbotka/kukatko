import { type GridStateSnapshot } from 'react-virtuoso'

/**
 * sessionStorage key under which every remembered grid position lives. Session
 * storage rather than local: a position is worth restoring while the reader is
 * still in the tab they scrolled, and worth forgetting by the time they open the
 * app again tomorrow. It also keeps two tabs on the same library from dragging
 * each other around.
 */
const STORAGE_KEY = 'kukatko.gridScroll'

/**
 * How many views are remembered at once. A reader alternates between a handful
 * of grids (the library, an album, a person), so this is generous; the oldest
 * entries beyond it are dropped, which is what stops a long session of jumping
 * between filters from growing the store without bound.
 */
export const GRID_SCROLL_MAX_ENTRIES = 16

/**
 * Query params that name a *position* rather than a *result set*, and so must
 * not take part in the key: two URLs that differ only in these show the same
 * photos in the same order, and the position remembered under one is exactly the
 * position wanted under the other.
 */
const TRANSIENT_PARAMS = ['at', 'info']

/** One grid's remembered position. */
export interface GridScrollState {
  /**
   * react-virtuoso's own snapshot — the scroll offset plus the measurements it
   * needs to lay the grid out at that offset before anything is on screen. Handed
   * back through `restoreStateFrom` for a pixel-exact restore. Absent for a grid
   * that renders every tile itself (the person gallery), which has no virtuoso.
   */
  snapshot?: GridStateSnapshot
  /**
   * Window scroll offset, the restore target for a grid that is not virtualized.
   */
  scrollY: number
  /**
   * How many photos the list had loaded. A list that grows by appending pages
   * comes back holding only its first page — far too short a document to scroll
   * back into — so the position is only meaningful once this many photos are
   * loaded again. Zero for a windowed list (the library), which is as tall as the
   * whole result from its first response and needs no such catching up.
   */
  count: number
}

/** The whole store: one entry per view key, oldest first. */
type Store = Record<string, GridScrollState>

/**
 * Builds the key a position is remembered under: the path plus the query that
 * defines the *result set*, with the position-only params
 * ({@link TRANSIENT_PARAMS}) dropped and the rest sorted so the same view always
 * produces the same key however the URL was assembled. Changing a filter changes
 * the key, which is what keeps a position from being restored into an unrelated
 * result.
 */
export function gridScrollKey(pathname: string, search: string): string {
  const params = new URLSearchParams(search)
  for (const param of TRANSIENT_PARAMS) {
    params.delete(param)
  }
  params.sort()
  const query = params.toString()
  return query === '' ? pathname : `${pathname}?${query}`
}

/** Narrows unknown parsed JSON to something with readable properties. */
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/** Reads a finite number off a parsed record, or undefined if it is not one. */
function num(source: Record<string, unknown>, field: string): number | undefined {
  const value = source[field]
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

/**
 * Validates a parsed virtuoso snapshot. Storage is shared with older (and newer)
 * builds of the app, so a stored shape is untrusted input: anything not matching
 * is dropped rather than handed to virtuoso, which would restore a nonsense
 * layout from it.
 */
function parseSnapshot(value: unknown): GridStateSnapshot | undefined {
  if (!isRecord(value)) {
    return undefined
  }
  const { gap, item, viewport } = value
  if (!isRecord(gap) || !isRecord(item) || !isRecord(viewport)) {
    return undefined
  }
  const scrollTop = num(value, 'scrollTop')
  const column = num(gap, 'column')
  const row = num(gap, 'row')
  const itemHeight = num(item, 'height')
  const itemWidth = num(item, 'width')
  const viewportHeight = num(viewport, 'height')
  const viewportWidth = num(viewport, 'width')
  if (
    scrollTop === undefined ||
    column === undefined ||
    row === undefined ||
    itemHeight === undefined ||
    itemWidth === undefined ||
    viewportHeight === undefined ||
    viewportWidth === undefined
  ) {
    return undefined
  }
  return {
    gap: { column, row },
    item: { height: itemHeight, width: itemWidth },
    scrollTop,
    viewport: { height: viewportHeight, width: viewportWidth },
  }
}

/** Validates one parsed entry, returning null for anything unusable. */
function parseState(value: unknown): GridScrollState | null {
  if (!isRecord(value)) {
    return null
  }
  const scrollY = num(value, 'scrollY') ?? 0
  const count = num(value, 'count') ?? 0
  const snapshot = parseSnapshot(value.snapshot)
  if (scrollY < 0 || count < 0) {
    return null
  }
  return { count, scrollY, ...(snapshot === undefined ? {} : { snapshot }) }
}

/**
 * Reads the whole store. Failures (storage disabled, a half-written or foreign
 * value) yield an empty store: remembering a position is best-effort and must
 * never break a grid.
 */
function readStore(): Store {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return {}
    }
    const parsed: unknown = JSON.parse(raw)
    if (!isRecord(parsed)) {
      return {}
    }
    const store: Store = {}
    for (const [key, value] of Object.entries(parsed)) {
      const state = parseState(value)
      if (state !== null) {
        store[key] = state
      }
    }
    return store
  } catch {
    // Storage unavailable or holding something this build cannot read.
    return {}
  }
}

/** Reads the position remembered for one view, or null when there is none. */
export function readGridScroll(key: string): GridScrollState | null {
  if (key === '') {
    return null
  }
  return readStore()[key] ?? null
}

/**
 * Remembers a view's position, evicting the oldest entries once there are more
 * than {@link GRID_SCROLL_MAX_ENTRIES}. Re-writing a key moves it to the newest
 * end, so the views a reader keeps returning to are the ones that survive.
 */
export function writeGridScroll(key: string, state: GridScrollState): void {
  if (key === '') {
    return
  }
  const store = readStore()
  // Object keys keep insertion order, so deleting before setting is what makes
  // the plain object an LRU list.
  // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
  delete store[key]
  store[key] = state
  const keys = Object.keys(store)
  for (const stale of keys.slice(0, Math.max(0, keys.length - GRID_SCROLL_MAX_ENTRIES))) {
    // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
    delete store[stale]
  }
  try {
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(store))
  } catch {
    // Best-effort: a full or disabled storage costs the reader their position,
    // nothing more.
  }
}
