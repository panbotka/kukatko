import { useCallback, useContext, useSyncExternalStore } from 'react'

import { AuthContext } from '../auth/AuthContext'

import { accountStorageKey } from './accountStorage'

/** Where the desktop navigation stands: the top bar, or a sidebar on the left. */
export type NavPosition = 'top' | 'left'

/** Every position the account page offers, in the order it offers them. */
export const NAV_POSITIONS: readonly NavPosition[] = ['top', 'left']

/** The position every account starts with — the top bar. */
export const DEFAULT_NAV_POSITION: NavPosition = 'top'

/**
 * localStorage key prefix under which an account's chosen navigation position
 * lives; the account's uid completes it (see {@link accountStorageKey}). Kept the
 * way the UI language is kept (`i18n/accountLanguage.ts`) and for the same
 * reasons: two people share one browser and the layout is theirs, not the
 * device's; it is a preference rather than a record of what the account did, so
 * sign-out leaves it alone; and there is no server-side copy.
 */
export const NAV_POSITION_PREFIX = 'kukatko.navPosition'

/** Narrows a stored string to a known position. */
function isNavPosition(value: string | null): value is NavPosition {
  return value !== null && (NAV_POSITIONS as readonly string[]).includes(value)
}

/**
 * The position `user` chose on this browser — the default when they never chose,
 * there is no user, storage is unavailable, or what is stored is not a position
 * this build knows.
 */
export function readNavPosition(user: string | undefined): NavPosition {
  const key = accountStorageKey(NAV_POSITION_PREFIX, user)
  if (key === null) {
    return DEFAULT_NAV_POSITION
  }
  try {
    const stored = window.localStorage.getItem(key)
    return isNavPosition(stored) ? stored : DEFAULT_NAV_POSITION
  } catch {
    return DEFAULT_NAV_POSITION
  }
}

/** Who to tell when this tab changes a position (the storage event skips its own tab). */
const listeners = new Set<() => void>()

/**
 * Remembers `user`'s position on this browser and tells every mounted reader, so
 * the shell moves the moment the account page is clicked — no reload. Best-effort:
 * a storage that refuses keeps the default for the next read, and a missing user
 * stores nothing.
 */
export function writeNavPosition(user: string | undefined, position: NavPosition): void {
  const key = accountStorageKey(NAV_POSITION_PREFIX, user)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, position)
  } catch {
    // Best-effort: ignore storage failures.
  }
  for (const listener of listeners) {
    listener()
  }
}

/**
 * Subscribes to position changes: this tab's writes, and another tab's through
 * the `storage` event, so two windows of one account agree.
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
 * The signed-in account's navigation position and the setter that changes it.
 * Read null-safely: outside a session (no account) it is always the default and
 * the setter stores nothing.
 */
export function useNavPosition(): [NavPosition, (position: NavPosition) => void] {
  const user = useContext(AuthContext)?.user?.uid
  const position = useSyncExternalStore(
    subscribe,
    () => readNavPosition(user),
    () => DEFAULT_NAV_POSITION,
  )
  const setPosition = useCallback(
    (next: NavPosition) => {
      writeNavPosition(user, next)
    },
    [user],
  )
  return [position, setPosition]
}
