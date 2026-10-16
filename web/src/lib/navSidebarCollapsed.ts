import { useCallback, useContext, useSyncExternalStore } from 'react'

import { AuthContext } from '../auth/AuthContext'

import { accountStorageKey } from './accountStorage'

/**
 * localStorage key prefix under which an account remembers whether its left
 * sidebar is folded to the icon-only rail; the account's uid completes it (see
 * {@link accountStorageKey}). Kept exactly like the navigation position
 * (`lib/navPosition.ts`) and for the same reasons: two people share one browser
 * and the layout is theirs, not the device's; it is a preference, so sign-out
 * leaves it alone; and there is no server-side copy. Only the sidebar reads it:
 * an account on the top bar keeps whatever it stored, so choosing the left
 * again brings the rail back as it was.
 */
export const NAV_SIDEBAR_COLLAPSED_PREFIX = 'kukatko.navSidebarCollapsed'

/** What is stored for a folded sidebar; anything else reads as expanded. */
const COLLAPSED = 'collapsed'

/** What is stored for an unfolded sidebar. */
const EXPANDED = 'expanded'

/** Every account starts with the sidebar unfolded: icon and label per row. */
export const DEFAULT_NAV_SIDEBAR_COLLAPSED = false

/**
 * Whether `user` folded the sidebar on this browser — the default (expanded)
 * when they never did, there is no user, or storage is unavailable.
 */
export function readNavSidebarCollapsed(user: string | undefined): boolean {
  const key = accountStorageKey(NAV_SIDEBAR_COLLAPSED_PREFIX, user)
  if (key === null) {
    return DEFAULT_NAV_SIDEBAR_COLLAPSED
  }
  try {
    const stored = window.localStorage.getItem(key)
    return stored === null ? DEFAULT_NAV_SIDEBAR_COLLAPSED : stored === COLLAPSED
  } catch {
    return DEFAULT_NAV_SIDEBAR_COLLAPSED
  }
}

/** Who to tell when this tab folds or unfolds (the storage event skips its own tab). */
const listeners = new Set<() => void>()

/**
 * Remembers `user`'s fold on this browser and tells every mounted reader, so the
 * shell's lane follows the click. Best-effort: a storage that refuses keeps the
 * default for the next read, and a missing user stores nothing.
 */
export function writeNavSidebarCollapsed(user: string | undefined, collapsed: boolean): void {
  const key = accountStorageKey(NAV_SIDEBAR_COLLAPSED_PREFIX, user)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, collapsed ? COLLAPSED : EXPANDED)
  } catch {
    // Best-effort: ignore storage failures.
  }
  for (const listener of listeners) {
    listener()
  }
}

/**
 * Subscribes to fold changes: this tab's writes, and another tab's through the
 * `storage` event, so two windows of one account agree.
 */
function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  window.addEventListener('storage', listener)
  return () => {
    listeners.delete(listener)
    window.removeEventListener('storage', listener)
  }
}

/**
 * Whether the signed-in account's sidebar is folded to the rail, and the setter
 * that changes it. Read null-safely: outside a session (no account) it is always
 * the default and the setter stores nothing.
 */
export function useNavSidebarCollapsed(): [boolean, (collapsed: boolean) => void] {
  const user = useContext(AuthContext)?.user?.uid
  const collapsed = useSyncExternalStore(
    subscribe,
    () => readNavSidebarCollapsed(user),
    () => DEFAULT_NAV_SIDEBAR_COLLAPSED,
  )
  const setCollapsed = useCallback(
    (next: boolean) => {
      writeNavSidebarCollapsed(user, next)
    },
    [user],
  )
  return [collapsed, setCollapsed]
}
