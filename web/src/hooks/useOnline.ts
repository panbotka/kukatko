import { useSyncExternalStore } from 'react'

/** Subscribes to the browser's connectivity changes. */
function subscribe(onChange: () => void): () => void {
  window.addEventListener('online', onChange)
  window.addEventListener('offline', onChange)
  return () => {
    window.removeEventListener('online', onChange)
    window.removeEventListener('offline', onChange)
  }
}

/** The browser's current belief about the network. */
function snapshot(): boolean {
  return navigator.onLine
}

/**
 * Whether the browser believes it is online (`navigator.onLine`, kept current by
 * the `online`/`offline` events). "Online" only means a network is attached, not
 * that the server answers — which is why the upload page still treats a failed
 * request on its own, and uses this only to stop starting files while it is
 * certainly pointless.
 */
export function useOnline(): boolean {
  return useSyncExternalStore(subscribe, snapshot, () => true)
}
