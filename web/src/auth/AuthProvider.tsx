import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'

import * as authService from '../services/auth'
import {
  canImport,
  canWrite,
  isAdmin,
  isMaintainer,
  NetworkError,
  type AuthSession,
} from '../services/auth'

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
 *
 * It publishes four statuses, not three: a backend it could not reach is
 * reported as `unreachable`, never as signed out. See {@link AuthStatus}.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>(INITIAL_STATE)

  const applySession = useCallback((session: AuthSession | null) => {
    setState({ status: session ? 'authenticated' : 'unauthenticated', session })
  }, [])

  /**
   * Reads `GET /auth/me` and files the outcome under the right status.
   *
   * The catch is where the two kinds of failure part company. A
   * {@link NetworkError} means the question never reached the server, so nothing
   * is known about the session and the app must say so — `unreachable`. Anything
   * else (a 5xx, a body that would not parse) *did* come from the server, so the
   * old "treat it as signed out and let the UI recover" behaviour still stands.
   *
   * Aborts are ours: the mount effect cancels on unmount, and that is not news.
   */
  const loadSession = useCallback(
    async (signal?: AbortSignal) => {
      try {
        applySession(await authService.fetchMe(signal))
      } catch (error: unknown) {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        setState({
          status: error instanceof NetworkError ? 'unreachable' : 'unauthenticated',
          session: null,
        })
      }
    },
    [applySession],
  )

  useEffect(() => {
    const controller = new AbortController()
    void loadSession(controller.signal)
    return () => {
      controller.abort()
    }
  }, [loadSession])

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

  const refresh = useCallback(() => loadSession(), [loadSession])

  const value = useMemo<AuthContextValue>(() => {
    const user = state.session?.user ?? null
    return {
      status: state.status,
      user,
      role: user?.role ?? null,
      downloadToken: state.session?.download_token ?? null,
      canWrite: user ? canWrite(user.role) : false,
      isAdmin: user ? isAdmin(user.role) : false,
      isMaintainer: user ? isMaintainer(user.role) : false,
      canImport: user ? canImport(user.role) : false,
      login,
      logout,
      refresh,
    }
  }, [state, login, logout, refresh])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
