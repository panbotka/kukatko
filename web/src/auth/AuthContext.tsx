import { createContext, useContext } from 'react'

import { canImport, canWrite, isAdmin, isMaintainer, type Role, type User } from '../services/auth'

/** Lifecycle of the auth session: still loading, signed in, or signed out. */
export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated'

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
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  /** Re-fetches the session from the backend (e.g. after role changes). */
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
