import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react'

import {
  type GridDensity,
  type GridDensityScope,
  initialColumns,
  LIBRARY_GRID_SCOPE,
  readStoredDensity,
  sanitizeDensity,
  writeDensity,
} from '../lib/gridDensity'

/** Components currently subscribed to the density, re-rendered on every change. */
const listeners = new Set<() => void>()

/**
 * Subscribes a component to density changes: both the ones this tab makes and
 * the ones another tab makes (the browser's `storage` event), so every open
 * Kukátko on the device agrees on the column count. Every subscriber is woken by
 * every change regardless of scope — each then re-reads its own key, and a
 * snapshot that did not move is a no-op to React.
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
 * Pins a grid to a column count, persists it under the scope's key and re-renders
 * every grid. The scope defaults to the photo library, the grid that had this
 * setting first.
 */
export function setGridDensity(
  density: number,
  scope: GridDensityScope = LIBRARY_GRID_SCOPE,
): void {
  writeDensity(sanitizeDensity(density, scope), scope)
  for (const listener of listeners) {
    listener()
  }
}

/** Result of {@link useGridDensity}: the current density plus its setter. */
export interface UseGridDensityResult {
  /** The current column count, always a concrete number in 1..GRID_COLUMNS_MAX. */
  density: GridDensity
  /** Pins a new column count and persists it under this hook's scope. */
  setDensity: (density: number) => void
}

/**
 * The user's grid density for one scope, shared by every grid of that scope on
 * the page and persisted per device. It is deliberately *not* URL state: it is a
 * display preference about this screen, not part of the view a link reproduces.
 *
 * On first use — nothing usable stored, or a legacy `'auto'` to migrate — the
 * value is seeded once from the current viewport width and persisted, so it is
 * stable from then on and a later window resize never moves it. After that the
 * value is exactly what the user set with the control. A cross-tab `storage`
 * event keeps every open tab on the same count.
 *
 * Scopes do not share a number: the photo library (the default) and the
 * `/outliers` review grid each keep their own, because a comfortable density for
 * browsing photographs is not a comfortable density for judging faces. See
 * `lib/gridDensity` `GridDensityScope`.
 */
export function useGridDensity(scope: GridDensityScope = LIBRARY_GRID_SCOPE): UseGridDensityResult {
  // localStorage is the single source of truth — no in-memory copy to keep in
  // sync. That is safe for `useSyncExternalStore` only because the snapshot is a
  // primitive (a column count, or `null` when nothing usable is stored): React
  // compares snapshots with `Object.is`, so re-reading the same value never looks
  // like a change and never loops. The scope is rebuilt from its fields rather
  // than used by identity, so an inline `{…}` at a call site cannot thrash the
  // effect below.
  const { storageKey, tileMinPx, gapPx } = scope
  const resolved = useMemo<GridDensityScope>(
    () => ({ storageKey, tileMinPx, gapPx }),
    [storageKey, tileMinPx, gapPx],
  )

  const getSnapshot = useCallback(() => readStoredDensity(resolved), [resolved])
  const stored = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  // First use on this device: no numeric preference yet (empty storage or a
  // legacy `'auto'`). Seed it once from the current viewport width — auto's only
  // remaining job — and persist it so the count stays put across resizes.
  useEffect(() => {
    if (readStoredDensity(resolved) === null) {
      setGridDensity(initialColumns(resolved), resolved)
    }
  }, [resolved])

  const setDensity = useCallback(
    (next: number) => {
      setGridDensity(next, resolved)
    },
    [resolved],
  )

  // Until that seed lands (the effect runs after the first paint) the effective
  // value is the same width-derived count, so the very first render already
  // shows it and there is no flash to a placeholder default.
  const density = stored ?? initialColumns(resolved)
  return { density, setDensity }
}
