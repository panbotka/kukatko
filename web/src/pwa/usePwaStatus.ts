import { useCallback, useEffect, useState } from 'react'

import { applyServiceWorkerUpdate, registerServiceWorker } from './register'

/** What the app needs to know about its installed-app state. */
export interface PwaStatus {
  /** The browser reports no network connection. */
  offline: boolean
  /** A newly deployed service worker is installed and waiting to take over. */
  updateReady: boolean
  /** Activates the waiting worker and reloads onto the new shell. */
  applyUpdate: () => void
  /** Hides the update prompt for this session without reloading. */
  dismissUpdate: () => void
}

/** What {@link usePwaStatus} accepts; the defaults are what the app runs on. */
export interface UsePwaStatusOptions {
  /**
   * Whether to register the service worker. Defaults to "this is a production
   * build" — `vite dev` emits no worker at all (see build/pwa.ts), and the
   * disabled branch unregisters any worker left over from a production visit to
   * the same origin. Tests pass it explicitly.
   */
  enabled?: boolean
}

/**
 * Registers the service worker once per page load and reports the two states
 * the reader may need to hear about: being offline, and a new version waiting.
 *
 * Registration is deliberately owned by a hook rather than by `main.tsx`: the
 * update prompt is UI, and this keeps the worker's lifecycle next to the
 * component that renders it (see components/pwa/PwaStatus).
 */
export function usePwaStatus({ enabled }: UsePwaStatusOptions = {}): PwaStatus {
  const registerEnabled = enabled ?? import.meta.env.PROD
  const [offline, setOffline] = useState(
    () => typeof navigator !== 'undefined' && !navigator.onLine,
  )
  const [updateReady, setUpdateReady] = useState(false)

  useEffect(() => {
    void registerServiceWorker({
      enabled: registerEnabled,
      onUpdateReady: () => {
        setUpdateReady(true)
      },
    })
  }, [registerEnabled])

  useEffect(() => {
    const goOnline = () => {
      setOffline(false)
    }
    const goOffline = () => {
      setOffline(true)
    }
    window.addEventListener('online', goOnline)
    window.addEventListener('offline', goOffline)
    return () => {
      window.removeEventListener('online', goOnline)
      window.removeEventListener('offline', goOffline)
    }
  }, [])

  const applyUpdate = useCallback(() => {
    setUpdateReady(false)
    applyServiceWorkerUpdate()
  }, [])

  const dismissUpdate = useCallback(() => {
    setUpdateReady(false)
  }, [])

  return { offline, updateReady, applyUpdate, dismissUpdate }
}
