import { type ReactNode, useEffect, useState } from 'react'

import { fetchCapabilities } from '../services/capabilities'

import {
  CAPABILITIES_DEFAULT,
  type CapabilitiesState,
  CapabilitiesContext,
} from './CapabilitiesContext'

/**
 * How often the feature flags are refreshed while the tab is visible, so the
 * semantic-search hint appears or disappears within about a minute of the
 * embeddings box going on- or offline. It matches the backend probe interval;
 * a tighter cadence would only add requests without surfacing changes sooner.
 */
const REFRESH_INTERVAL_MS = 60_000

/**
 * Provides the instance feature flags to the app. On mount — and then on an
 * interval and whenever the tab becomes visible again — it loads them from
 * `GET /api/v1/capabilities`, so the UI reflects the embeddings box coming
 * on- or offline without a reload. A failed fetch leaves the last known flags in
 * place; the flags are a hint, never load-bearing, so an offline backend simply
 * keeps the last state rather than erroring.
 *
 * What a failed fetch must NOT do is look like a successful one reporting an
 * absent feature, so `known` flips to true only on a response that arrived and
 * stays true afterwards: once the instance has told us what it can do, a later
 * network blip leaves that answer standing rather than reverting the UI to
 * "nothing works".
 */
export function CapabilitiesProvider({ children }: { children: ReactNode }) {
  const [capabilities, setCapabilities] = useState<CapabilitiesState>(CAPABILITIES_DEFAULT)

  useEffect(() => {
    let active = true
    let controller: AbortController | null = null
    let timer: number | null = null

    const load = () => {
      // Skip work for a hidden tab; a visibilitychange re-triggers it on return.
      if (document.hidden) {
        return
      }
      controller?.abort()
      const current = new AbortController()
      controller = current
      void fetchCapabilities(current.signal)
        .then((next) => {
          // Ignore a resolved response from a superseded/aborted request.
          if (active && controller === current) {
            setCapabilities({ ...next, known: true })
          }
        })
        .catch(() => {
          // Swallow: an unreachable backend keeps the last known flags.
        })
    }
    const startTimer = () => {
      timer ??= window.setInterval(load, REFRESH_INTERVAL_MS)
    }
    const stopTimer = () => {
      if (timer !== null) {
        window.clearInterval(timer)
        timer = null
      }
    }
    const onVisibilityChange = () => {
      if (document.hidden) {
        stopTimer()
      } else {
        load()
        startTimer()
      }
    }

    load()
    if (!document.hidden) {
      startTimer()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      active = false
      controller?.abort()
      stopTimer()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [])

  return (
    <CapabilitiesContext.Provider value={capabilities}>{children}</CapabilitiesContext.Provider>
  )
}
