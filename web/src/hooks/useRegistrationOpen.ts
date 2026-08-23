import { useEffect, useState } from 'react'

import { fetchPublicSettings } from '../services/settings'

/**
 * What this instance says about self-service registration.
 *
 * `unknown` is not a synonym for `closed`: it means the question could not be
 * asked at all (an unreachable backend, a 5xx). The two callers answer it
 * differently on purpose — the sign-in screen hides the invitation, because
 * advertising a door that may not open is worse than not mentioning it, while
 * the registration screen still shows its form and lets the server have the
 * final word.
 */
export type RegistrationState = 'loading' | 'open' | 'closed' | 'unknown'

/**
 * Asks `GET /api/v1/settings/public` once whether registration is open.
 *
 * The endpoint is anonymous, so this works before anybody has signed in. It
 * fetches exactly once per mount and never polls: an administrator opening or
 * closing registration is rare, and both screens that read it are short-lived
 * (a visitor is on them for a minute at most).
 *
 * The request is aborted on unmount, and an aborted request leaves the state
 * alone — the component is gone, and `unknown` would be a claim about the
 * instance rather than about the cancellation.
 */
export function useRegistrationOpen(): RegistrationState {
  const [state, setState] = useState<RegistrationState>('loading')

  useEffect(() => {
    const controller = new AbortController()
    void fetchPublicSettings(controller.signal)
      .then((settings) => {
        if (!controller.signal.aborted) {
          setState(settings.registration_enabled ? 'open' : 'closed')
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState('unknown')
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  return state
}
