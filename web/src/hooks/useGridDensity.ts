import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react'

import {
  GRID_COLUMNS_MAX,
  type GridDensity,
  type GridDensityScope,
  initialColumns,
  LIBRARY_GRID_SCOPE,
  maxColumnsForViewport,
  maxTilesPerRowForViewport,
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
 * Subscribes a component to the viewport's column ceiling. A resize (or a phone
 * rotating) can move the grid between the breakpoint caps, and unlike the stored
 * density that change reaches no `storage` event — so it needs its own listener.
 * The snapshot is the ceiling itself, not the width, so dragging a window edge
 * re-renders only when the grid would actually change shape.
 */
function subscribeViewport(onStoreChange: () => void): () => void {
  window.addEventListener('resize', onStoreChange)
  window.addEventListener('orientationchange', onStoreChange)
  return () => {
    window.removeEventListener('resize', onStoreChange)
    window.removeEventListener('orientationchange', onStoreChange)
  }
}

/** The ceiling to assume with no viewport to measure (SSR): no ceiling at all. */
function serverMaxColumns(): number {
  return GRID_COLUMNS_MAX
}

/** The row cap to assume with no viewport to measure (SSR): none at all. */
function serverMaxTilesPerRow(): undefined {
  return undefined
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
  /**
   * The column count actually in effect: the stored preference, lowered to what
   * this viewport can carry. Always a concrete number in
   * `GRID_COLUMNS_MIN..GRID_COLUMNS_MAX`.
   */
  density: GridDensity
  /** Pins a new column count and persists it under this hook's scope. */
  setDensity: (density: number) => void
  /**
   * The most columns this viewport will render (see `GRID_COLUMN_CAPS`) — the
   * ceiling a picker must not offer to step past.
   */
  maxColumns: number
  /**
   * The user's stored choice, before the viewport ceiling. Equals {@link density}
   * on a wide screen; on a narrow one it is the value that comes back when the
   * window widens again.
   */
  storedDensity: GridDensity
  /**
   * The most photographs one row of a **justified** wall may hold on this
   * viewport, or `undefined` where it imposes no ceiling at all. A fixed-column
   * grid needs none — {@link density} is already the whole answer for it — but a
   * justified one reads the density as a target height, and a row of portraits
   * at a phone's target height holds twice the pinned count. See
   * `lib/gridDensity.maxTilesPerRowForWidth`.
   */
  maxTilesPerRow: number | undefined
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
 * The one thing that does move with the window is the *ceiling*: a viewport too
 * narrow to carry the chosen count renders fewer columns (`GRID_COLUMN_CAPS` —
 * at most 3 below 576 px, 4 below 768 px), because one stored number is shared
 * by the laptop that set it and the phone that has to live with it. The clamp is
 * display-only: `density` is what the grid renders, `storedDensity` what the
 * user chose, and widening the window brings the latter straight back. A
 * justified wall gets that ceiling a second time as `maxTilesPerRow`, because
 * there the density is only a target row height and a narrow row would otherwise
 * sail past the count the screen can carry.
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

  // The narrow-viewport ceiling is the other half of the effective count. It is
  // read from the live viewport rather than from storage, so it never touches
  // the persisted preference: a phone renders fewer columns, a laptop the user's
  // own number, and the same browser profile can do both without a fight.
  const maxColumns = useSyncExternalStore(
    subscribeViewport,
    maxColumnsForViewport,
    serverMaxColumns,
  )
  // The same ceiling for the justified wall, where "columns" is a target row
  // height rather than a track count: read separately (both snapshots are
  // primitives, so neither can loop) so a caller is never left to work the row
  // cap out of a column count itself.
  const maxTilesPerRow = useSyncExternalStore(
    subscribeViewport,
    maxTilesPerRowForViewport,
    serverMaxTilesPerRow,
  )

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
  const storedDensity = stored ?? initialColumns(resolved)
  const density = Math.min(storedDensity, maxColumns)
  return { density, setDensity, maxColumns, storedDensity, maxTilesPerRow }
}
