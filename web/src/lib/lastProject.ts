import { accountStorageKey } from './accountStorage'

/**
 * localStorage key prefix under which the project an account was last in on
 * this browser lives (its slug); the account's uid completes it (see
 * {@link accountStorageKey}). It is what an instance page (`/account`,
 * `/system…`) offers as the way back, so it is per account — two people share
 * one browser and each came from their own library — and per browser rather
 * than the server's `last_project_slug`, which another device moves. Kept
 * across sign-out, like the navigation position: it is only a slug, a key with
 * another account's uid in it is never read, and signing back in should still
 * know the way back.
 */
export const LAST_PROJECT_PREFIX = 'kukatko.lastProject'

/**
 * The slug of the project `user` was last in on this browser, or null when
 * nothing is remembered, there is no user, or storage is unavailable. The slug
 * is not checked here: whether it still names a project the account may enter
 * is the reader's question (see `project/backToProject.ts`).
 */
export function readLastProject(user: string | undefined): string | null {
  const key = accountStorageKey(LAST_PROJECT_PREFIX, user)
  if (key === null) {
    return null
  }
  try {
    const stored = window.localStorage.getItem(key)
    return stored === null || stored === '' ? null : stored
  } catch {
    return null
  }
}

/**
 * Remembers `slug` as the project `user` was last in on this browser.
 * Best-effort: a storage that refuses keeps whatever it had, and a missing user
 * or an empty slug stores nothing.
 */
export function writeLastProject(user: string | undefined, slug: string): void {
  const key = accountStorageKey(LAST_PROJECT_PREFIX, user)
  if (key === null || slug === '') {
    return
  }
  try {
    window.localStorage.setItem(key, slug)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
