import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'

import * as authService from '../services/auth'
import { canImport, canWrite, type AuthSession } from '../services/auth'

import { AuthContext, type AuthContextValue, type AuthStatus } from './AuthContext'

interface AuthState {
  status: AuthStatus
  session: AuthSession | null
}

const INITIAL_STATE: AuthState = { status: 'loading', session: null }

/**
 * Provides authentication state to the app. On mount it loads the current
 * session from `GET /auth/me`, then exposes `login`/`logout`/`refresh` plus
 * derived role helpers through {@link useAuth}.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>(INITIAL_STATE)

  const applySession = useCallback((session: AuthSession | null) => {
    setState({ status: session ? 'authenticated' : 'unauthenticated', session })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    authService
      .fetchMe(controller.signal)
      .then(applySession)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        // Treat an unreachable backend as signed out so the UI can recover.
        applySession(null)
      })
    return () => {
      controller.abort()
    }
  }, [applySession])

  const login = useCallback(
    async (username: string, password: string) => {
      const session = await authService.login(username, password)
      applySession(session)
    },
    [applySession],
  )

  const logout = useCallback(async () => {
    try {
      await authService.logout()
    } finally {
      applySession(null)
    }
  }, [applySession])

  const refresh = useCallback(async () => {
    const session = await authService.fetchMe()
    applySession(session)
  }, [applySession])

  const value = useMemo<AuthContextValue>(() => {
    const user = state.session?.user ?? null
    return {
      status: state.status,
      user,
      role: user?.role ?? null,
      downloadToken: state.session?.download_token ?? null,
      canWrite: user ? canWrite(user.role) : false,
      isAdmin: user?.role === 'admin',
      canImport: user ? canImport(user.role) : false,
      login,
      logout,
      refresh,
    }
  }, [state, login, logout, refresh])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
