import { useEffect, useState } from 'react'

import { fetchPublicSettings, type PublicSettings } from '../services/settings'

/**
 * What the sign-in screen managed to learn about this instance.
 *
 * `unknown` is not a synonym for "everything off": it means the question could
 * not be asked at all (an unreachable backend, a 5xx). Each caller decides what
 * that silence means for the affordance it is about to draw, which is why the
 * state carries the failure rather than a set of defaulted booleans.
 */
export type PublicSettingsState =
  | { status: 'loading' }
  | { status: 'ready'; settings: PublicSettings }
  | { status: 'unknown' }

/**
 * Asks `GET /api/v1/settings/public` once what an anonymous visitor may know
 * about this instance: whether registration is open, and whether it can run a
 * passkey ceremony.
 *
 * The endpoint is anonymous, so this works before anybody has signed in — which
 * is the whole reason those two flags live there and not only behind
 * `GET /capabilities`. It fetches exactly once per mount and never polls: neither
 * fact changes often (one is an administrator's rare decision, the other is
 * fixed for the life of the process), and the screens that read them are
 * short-lived.
 *
 * The request is aborted on unmount, and an aborted request leaves the state
 * alone — the component is gone, and `unknown` would be a claim about the
 * instance rather than about the cancellation.
 */
export function usePublicSettings(): PublicSettingsState {
  const [state, setState] = useState<PublicSettingsState>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    void fetchPublicSettings(controller.signal)
      .then((settings) => {
        if (!controller.signal.aborted) {
          setState({ status: 'ready', settings })
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState({ status: 'unknown' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  return state
}
