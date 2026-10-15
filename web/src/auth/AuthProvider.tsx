import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react'

import i18n from '../i18n'
import { useAccountLanguage } from '../i18n/accountLanguage'
import { entryProjectSlug } from '../project/paths'
import { dropBrowserSubscription, heldPushSubscription } from '../pwa/push'
import { setCurrentProjectSlug } from '../services/api'
import * as authService from '../services/auth'
import { signInWithPasskey } from '../services/passkeys'
import { canCurate, canWrite, isAdmin, NetworkError, type AuthSession } from '../services/auth'

import { AuthContext, type AuthContextValue, type AuthStatus } from './AuthContext'
import { clearSignedOutState } from './signOutStorage'

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
 *
 * It also owns the browser state that belongs to a session: the UI language
 * follows the signed-in account (see {@link useAccountLanguage}), and signing
 * out removes what the account left in storage (see
 * {@link clearSignedOutState}) and this browser's push subscription, so the
 * next person on this browser starts clean.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>(INITIAL_STATE)

  // The project is set before the state, so the first render of a signed-in
  // tree can already build project-scoped URLs (see services/api). It is the
  // entry rule's pick; ProjectProvider then replaces it with the project the
  // address names, before any page below it renders.
  const applySession = useCallback((session: AuthSession | null) => {
    setCurrentProjectSlug(entryProjectSlug(session))
    setState({ status: session ? 'authenticated' : 'unauthenticated', session })
  }, [])

  /**
   * Publishes a session a sign-in returned. A sign-in answer carries no
   * projects (only `GET /auth/me` does), so the session is re-read to learn
   * which project to work in; should that read find no session after all, the
   * sign-in answer stands.
   */
  const applySignIn = useCallback(
    async (session: AuthSession) => {
      applySession(session.projects ? session : ((await authService.fetchMe()) ?? session))
    },
    [applySession],
  )

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
      await applySignIn(await authService.login(username, password))
    },
    [applySignIn],
  )

  /**
   * The passkey counterpart of {@link login}. It differs only in how the session
   * is obtained: the ceremony runs in `services/passkeys`, and what comes back is
   * the same `AuthSession` the password endpoint returns, applied the same way.
   */
  const loginWithPasskey = useCallback(async () => {
    await applySignIn(await signInWithPasskey())
  }, [applySignIn])

  // The storage is cleared whether or not the server heard the sign-out: the
  // person clicking it is leaving this browser either way. So is the push
  // subscription, in both places: the request names it, so the server forgets
  // it with the session, and the browser drops its half afterwards — or the
  // next person on a shared device would receive this account's notifications.
  // Neither half can hold the sign-out up: a failed lookup sends no endpoint,
  // and the drop is not awaited.
  const logout = useCallback(async () => {
    const subscription = await heldPushSubscription().catch(() => null)
    try {
      await authService.logout(subscription?.endpoint ?? null)
    } finally {
      if (subscription) {
        void dropBrowserSubscription(subscription).catch(() => undefined)
      }
      clearSignedOutState()
      applySession(null)
    }
  }, [applySession])

  useAccountLanguage(i18n, state.status, state.session?.user.uid)

  const refresh = useCallback(() => loadSession(), [loadSession])

  // The count of profile-picture changes made in this tab; see
  // AuthContextValue.pictureVersion for why the avatars need one.
  const [pictureVersion, setPictureVersion] = useState(0)
  const pictureChanged = useCallback(() => {
    setPictureVersion((previous) => previous + 1)
  }, [])

  const value = useMemo<AuthContextValue>(() => {
    const user = state.session?.user ?? null
    const instanceRole = user
      ? (state.session?.instance_role ?? user.instance_role ?? 'user')
      : null
    // Outside a project there is no project role: only a maintainer, an admin
    // of every project, holds one anyway. `useAuth` replaces it with the
    // current project's role.
    const role = instanceRole === 'maintainer' ? 'maintainer' : null
    const lastProject = state.session?.last_project_slug ?? ''
    return {
      status: state.status,
      // The account record carries no linked person; `useAuth` puts the current
      // project's on it.
      user: user === null ? null : { ...user, subject_uid: null },
      role,
      instanceRole,
      projects: state.session?.projects ?? [],
      lastProjectSlug: lastProject === '' ? null : lastProject,
      downloadToken: state.session?.download_token ?? null,
      canCurate: role !== null && canCurate(role),
      canWrite: role !== null && canWrite(role),
      isAdmin: role !== null && isAdmin(role),
      isMaintainer: instanceRole === 'maintainer',
      canImport: instanceRole === 'maintainer',
      pictureVersion,
      pictureChanged,
      login,
      loginWithPasskey,
      logout,
      refresh,
    }
  }, [state, pictureVersion, pictureChanged, login, loginWithPasskey, logout, refresh])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
