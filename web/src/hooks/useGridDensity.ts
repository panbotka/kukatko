import { useEffect, useSyncExternalStore } from 'react'

import {
  type GridDensity,
  initialColumns,
  readStoredDensity,
  sanitizeDensity,
  writeDensity,
} from '../lib/gridDensity'

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
 * primitive (a column count, or `null` when nothing usable is stored): React
 * compares snapshots with `Object.is`, so re-reading the same value never looks
 * like a change and never loops.
 */
function getSnapshot(): number | null {
  return readStoredDensity()
}

/** Pins the grid to a column count, persists it and re-renders every grid. */
export function setGridDensity(density: number): void {
  writeDensity(sanitizeDensity(density))
  for (const listener of listeners) {
    listener()
  }
}

/** Result of {@link useGridDensity}: the current density plus its setter. */
export interface UseGridDensityResult {
  /** The current column count, always a concrete number in 1..GRID_COLUMNS_MAX. */
  density: GridDensity
  /** Pins a new column count and persists it. */
  setDensity: (density: number) => void
}

/**
 * The user's photo-grid density, shared by every grid on the page and persisted
 * per device. It is deliberately *not* URL state: it is a display preference
 * about this screen, not part of the view a link reproduces.
 *
 * On first use — nothing usable stored, or a legacy `'auto'` to migrate — the
 * value is seeded once from the current viewport width and persisted, so it is
 * stable from then on and a later window resize never moves it. After that the
 * value is exactly what the user set with the control. A cross-tab `storage`
 * event keeps every open tab on the same count.
 */
export function useGridDensity(): UseGridDensityResult {
  const stored = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  // First use on this device: no numeric preference yet (empty storage or a
  // legacy `'auto'`). Seed it once from the current viewport width — auto's only
  // remaining job — and persist it so the count stays put across resizes.
  useEffect(() => {
    if (readStoredDensity() === null) {
      setGridDensity(initialColumns())
    }
  }, [])

  // Until that seed lands (the effect runs after the first paint) the effective
  // value is the same width-derived count, so the very first render already
  // shows it and there is no flash to a placeholder default.
  const density = stored ?? initialColumns()
  return { density, setDensity: setGridDensity }
}
