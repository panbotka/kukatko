import { useEffect, useRef, useState } from 'react'

import { type DirectoryUser, listDirectory } from '../services/directory'

/** Fetch lifecycle of the directory. */
export type DirectoryStatus = 'idle' | 'loading' | 'ready' | 'error'

/** What {@link useDirectory} exposes. */
export interface UseDirectoryResult {
  status: DirectoryStatus
  /** The people of the library, in the directory's order; empty until loaded. */
  users: DirectoryUser[]
}

/**
 * The people of the library (`GET /users`), fetched the first time `wanted`
 * is true and kept for the component's life. A comment composer asks for it
 * when the reader first types `@`, not when the thread mounts: paging through
 * a hundred photos must not fetch the directory a hundred times for a list
 * almost nobody opens. A failure is remembered as such and never retried in
 * place — the list simply offers nobody, and the typed text stays text.
 */
export function useDirectory(wanted: boolean): UseDirectoryResult {
  const [status, setStatus] = useState<DirectoryStatus>('idle')
  const [users, setUsers] = useState<DirectoryUser[]>([])
  // The one fetch's controller: set when it starts, aborted only on unmount —
  // the list closing while the directory is still on its way must not cancel
  // it, or the next `@` would start from nothing again.
  const inFlight = useRef<AbortController | null>(null)

  useEffect(
    () => () => {
      inFlight.current?.abort()
    },
    [],
  )

  useEffect(() => {
    if (!wanted || inFlight.current !== null) {
      return
    }
    const controller = new AbortController()
    inFlight.current = controller
    setStatus('loading')
    listDirectory(controller.signal)
      .then((list) => {
        if (!controller.signal.aborted) {
          setUsers(list)
          setStatus('ready')
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setStatus('error')
        }
      })
  }, [wanted])

  return { status, users }
}
