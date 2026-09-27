import { type i18n as I18n } from 'i18next'
import { useEffect } from 'react'

import { type AuthStatus } from '../auth/AuthContext'
import { accountStorageKey } from '../lib/accountStorage'

import { supportedLngs } from './index'

/** One of the UI languages the app ships. */
export type SupportedLng = (typeof supportedLngs)[number]

/** The language every account starts in, and the one a signed-out screen shows. */
export const DEFAULT_LNG: SupportedLng = 'cs'

/**
 * localStorage key prefix under which an account's chosen UI language lives;
 * the account's uid completes it (see {@link accountStorageKey}). Per account
 * because two people share one browser and the language is theirs, not the
 * device's: one of them switching to English must not hand the other an
 * English UI. Deliberately *not* cleared at sign-out — it is a preference, not a
 * record of what the account has done, and signing back in should find it.
 * There is no server-side copy; it stays browser state.
 */
export const LANGUAGE_PREFIX = 'kukatko.language'

/**
 * The key an older build kept the language under: i18next's language-detector
 * cache, one per browser. Nothing reads it any more; sign-out removes it.
 */
export const LEGACY_LANGUAGE_KEY = 'i18nextLng'

/** Narrows a stored string to a supported language. */
function isSupportedLng(value: string | null): value is SupportedLng {
  return value !== null && (supportedLngs as readonly string[]).includes(value)
}

/**
 * The language `user` chose on this browser, or null when they never chose,
 * there is no user, storage is unavailable, or what is stored is not a language
 * this build ships — all of which mean "the default".
 */
export function readLanguagePreference(user: string | undefined): SupportedLng | null {
  const key = accountStorageKey(LANGUAGE_PREFIX, user)
  if (key === null) {
    return null
  }
  try {
    const stored = window.localStorage.getItem(key)
    return isSupportedLng(stored) ? stored : null
  } catch {
    return null
  }
}

/**
 * Remembers `user`'s language on this browser. Best-effort: a storage that
 * refuses costs the choice at the next sign-in, and a missing user stores
 * nothing (the switch still applies for as long as the page lives).
 */
export function writeLanguagePreference(user: string | undefined, lng: SupportedLng): void {
  const key = accountStorageKey(LANGUAGE_PREFIX, user)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, lng)
  } catch {
    // Best-effort: ignore storage failures.
  }
}

/**
 * Puts the UI in the language of whoever the session belongs to: the account's
 * own stored choice once it is signed in, the default once nobody is. It acts
 * only when the answer is known — while the session is loading, or the server
 * cannot be reached, the language is left as it is — and only when the account
 * changes, so a switch made on the account page is not undone by a re-render.
 */
export function useAccountLanguage(i18n: I18n, status: AuthStatus, user: string | undefined) {
  useEffect(() => {
    if (status === 'authenticated') {
      void i18n.changeLanguage(readLanguagePreference(user) ?? DEFAULT_LNG)
    } else if (status === 'unauthenticated') {
      void i18n.changeLanguage(DEFAULT_LNG)
    }
  }, [i18n, status, user])
}
