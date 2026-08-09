import { useEffect, useState } from 'react'

import { fetchWhatsNew, type WhatsNew } from '../services/whatsNew'

/**
 * Loads the digest of what happened in the library since the reader's previous
 * visit, for the panel above the library grid.
 *
 * It fetches exactly once per mount and never polls. That is not a performance
 * choice but a correctness one: the request is what stamps the reader's visit
 * server-side, and a digest that reloaded itself while being read would be a
 * panel that changes under the reader's eyes.
 *
 * Any failure is swallowed and the state stays null, so the panel simply does
 * not appear — the library must never break over a summary. The in-flight
 * request is aborted on unmount.
 *
 * @returns The digest (whose `has_news` may be false, meaning "show nothing"),
 *   or null while loading or after a failure.
 */
export function useWhatsNew(): WhatsNew | null {
  const [summary, setSummary] = useState<WhatsNew | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    fetchWhatsNew(controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) {
          setSummary(next)
        }
      })
      .catch(() => {
        // Silent: hide the panel on any failure so the library never errors.
      })
    return () => {
      controller.abort()
    }
  }, [])

  return summary
}
