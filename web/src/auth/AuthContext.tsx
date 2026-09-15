import { createContext, useContext } from 'react'

import { canImport, canWrite, isAdmin, isMaintainer, type Role, type User } from '../services/auth'

/**
 * Lifecycle of the auth session: still loading, signed in, signed out — or
 * `unreachable`, meaning the backend never answered, so whether the visitor is
 * signed in is simply not known.
 *
 * `unreachable` exists because collapsing it into `unauthenticated` lied to the
 * reader. The installed app cold-launches from the service-worker cache with no
 * network, `GET /auth/me` fails at the transport, and every screen downstream
 * then acted as if the session had ended: the guard bounced them to a login form
 * no server could answer, which blamed their password for the outage.
 */
export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated' | 'unreachable'

/** Value exposed by {@link AuthProvider} via the {@link useAuth} hook. */
export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  role: Role | null
  /** Opaque token for authorizing media downloads; null when signed out. */
  downloadToken: string | null
  /** True when the current user may perform write actions (editor and above). */
  canWrite: boolean
  /**
   * True when the current user holds governance privileges — admin or higher. A
   * maintainer inherits every admin power, so it qualifies too.
   */
  isAdmin: boolean
  /**
   * True when the current user holds operations privileges — maintainer only:
   * imports, maintenance, system status, backup, restore, jobs and processing.
   */
  isMaintainer: boolean
  /** True when the current user may trigger imports (maintainer only). */
  canImport: boolean
  /**
   * How many times the signed-in user has changed their own profile picture
   * since this tab was loaded. It lives here because the picture is changed in
   * one place (the account page) and drawn in several others — the bar at the
   * top of that very page above all — and an `<img>` whose `src` never changes
   * is never re-requested, however stale the picture behind it has become.
   *
   * It is a counter rather than the picture's identity: what a reader needs is
   * "this is not the one you already have", and a reload gets the current
   * picture from the server anyway, which is why it starts at zero every time.
   */
  pictureVersion: number
  /**
   * Announces that the signed-in user's picture has just changed, so every
   * avatar of theirs on the screen redraws. Called by whatever performed the
   * change, after the server has confirmed it.
   */
  pictureChanged: () => void
  login: (username: string, password: string) => Promise<void>
  /**
   * Signs in with a passkey instead of a password: runs the discoverable WebAuthn
   * ceremony and, on success, publishes the session it yields — the very same one
   * a password login produces, so nothing downstream can tell the two apart.
   *
   * It rejects with a `PasskeyError` (`internal` reasons like a dismissed prompt
   * or an account still awaiting approval), which the sign-in screen translates.
   */
  loginWithPasskey: () => Promise<void>
  logout: () => Promise<void>
  /**
   * Re-reads the session from the backend — after a role change, or when the
   * offline screen offers to try again.
   *
   * It never rejects: an unreachable backend lands in the `unreachable` status
   * rather than in the caller's `catch`, which is the whole point of telling the
   * two failures apart.
   */
  refresh: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)

/**
 * Accesses the current auth state. Must be called within an {@link AuthProvider}.
 *
 * @throws Error when used outside of an `AuthProvider`.
 */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (ctx === null) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return ctx
}

/** Helper re-exports so consumers can derive capabilities from a role. */
export { canWrite, canImport, isAdmin, isMaintainer }
