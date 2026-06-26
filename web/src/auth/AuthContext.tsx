import { createContext, useContext } from 'react'

import { canWrite, type Role, type User } from '../services/auth'

/** Lifecycle of the auth session: still loading, signed in, or signed out. */
export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated'

/** Value exposed by {@link AuthProvider} via the {@link useAuth} hook. */
export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  role: Role | null
  /** Opaque token for authorizing media downloads; null when signed out. */
  downloadToken: string | null
  /** True when the current user may perform write actions. */
  canWrite: boolean
  /** True when the current user is an administrator. */
  isAdmin: boolean
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

/** Helper re-export so consumers can derive write capability from a role. */
export { canWrite }
