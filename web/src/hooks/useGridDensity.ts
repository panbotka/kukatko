import { useSyncExternalStore } from 'react'

import {
  defaultDensityForViewport,
  type GridDensity,
  readStoredDensity,
  sanitizeDensity,
  writeDensity,
} from '../lib/gridDensity'

import { useIsNarrowViewport } from './useIsNarrowViewport'

/** Components currently subscribed to the density, re-rendered on every change. */
const listeners = new Set<() => void>()

/**
 * Subscribes a component to density changes: both the ones this tab makes and
 * the ones another tab makes (the browser's `storage` event), so every open
 * Kukátko on the device agrees on the column count.
 */
function subscribe(onStoreChange: () => void): () => void {
  listeners.add(onStoreChange)
  window.addEventListener('storage', onStoreChange)
  return () => {
    listeners.delete(onStoreChange)
    window.removeEventListener('storage', onStoreChange)
  }
}

/**
 * localStorage is the single source of truth — no in-memory copy to keep in
 * sync. That is safe for `useSyncExternalStore` only because the snapshot is a
 * primitive (a column count, `'auto'`, or `null` when nothing is stored): React
 * compares snapshots with `Object.is`, so re-reading the same value never looks
 * like a change and never loops.
 */
function getSnapshot(): GridDensity | null {
  return readStoredDensity()
}

/** Pins the grid to a column count (or `'auto'`), persists it and re-renders every grid. */
export function setGridDensity(density: GridDensity): void {
  writeDensity(sanitizeDensity(density))
  for (const listener of listeners) {
    listener()
  }
}

/** Result of {@link useGridDensity}: the current density plus its setter. */
export interface UseGridDensityResult {
  /** The persisted column count, or `'auto'` for the responsive default. */
  density: GridDensity
  /** Pins a new column count and persists it. */
  setDensity: (density: GridDensity) => void
}

/**
 * The user's photo-grid density, shared by every grid on the page and persisted
 * per device. It is deliberately *not* URL state: it is a display preference
 * about this screen, not part of the view a link reproduces.
 *
 * Until the user picks a density, the effective value is viewport-aware — one
 * photo per row on a phone, the responsive multi-column default on wider
 * screens — via {@link defaultDensityForViewport}. The moment a choice is stored
 * it wins on every viewport, so the per-device preference always survives.
 */
export function useGridDensity(): UseGridDensityResult {
  const stored = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  const narrow = useIsNarrowViewport()
  const density = stored ?? defaultDensityForViewport(narrow)
  return { density, setDensity: setGridDensity }
}
