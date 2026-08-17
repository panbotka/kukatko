import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

/** Public surface of {@link useLeaveGuard}. */
export interface LeaveGuard {
  /** True while a held-back navigation waits for the reader's answer. */
  asking: boolean
  /** Lets the held-back navigation through. */
  confirm: () => void
  /** Drops it and stays where we are. */
  cancel: () => void
}

/**
 * Holds a click back from leaving a page that has unsaved work in the browser,
 * and asks the browser to warn on a tab close.
 *
 * The upload queue lives entirely in this tab: the files, their progress and the
 * abort controllers are React state, so any navigation away — an in-app link or
 * closing the tab — ends the batch mid-flight and the photos never arrive. That
 * was survivable while the upload had to be started by hand; now that picking
 * files starts it, a reader can be uploading without ever having pressed
 * anything, so it has to be said out loud before it is lost.
 *
 * Two mechanisms, because the browser only owns one of them:
 *
 * - **Tab close / reload:** `beforeunload` with `preventDefault()`, which is all
 *   modern browsers act on (the deprecated `returnValue` assignment adds
 *   nothing and the linter rejects it). The wording is the browser's own — a
 *   page cannot supply it.
 * - **In-app navigation:** a capture-phase click listener that catches the
 *   anchor before react-router's own handler runs. `preventDefault()` there is
 *   enough to stop the `Link` (it bails on an already-prevented event), and the
 *   target is remembered so {@link LeaveGuard.confirm} can navigate to it after
 *   the fact. The app has no data router, so react-router's `useBlocker` is not
 *   available — and a click listener also catches the plain `<a>`s of the
 *   footer and the tab bar, which a router-level blocker would not.
 *
 * Only ordinary in-app link activations are held: a modified click (a new tab),
 * an explicit `target`, a `download`, an external origin, and a link back to the
 * page we are already on all pass straight through, because none of them
 * destroys the queue. A navigation made in code (`navigate(...)`) is not a click
 * and is likewise not held — including the one `confirm` itself performs.
 */
export function useLeaveGuard(active: boolean): LeaveGuard {
  const navigate = useNavigate()
  const location = useLocation()
  const [pending, setPending] = useState<string | null>(null)

  // The current in-app URL, read from the router rather than `window.location`
  // so this is right under a memory router too.
  const here = `${location.pathname}${location.search}${location.hash}`
  const hereRef = useRef(here)
  hereRef.current = here

  useEffect(() => {
    if (!active) {
      // Nothing left to lose: drop a question that is no longer worth asking
      // (the batch finished while the dialog was up).
      setPending(null)
      return
    }

    const onBeforeUnload = (event: BeforeUnloadEvent): void => {
      event.preventDefault()
    }

    const onClick = (event: MouseEvent): void => {
      if (event.defaultPrevented || event.button !== 0) {
        return
      }
      // A modified click opens elsewhere and leaves this tab — and its upload —
      // exactly where it is.
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return
      }
      const target = event.target
      if (!(target instanceof Element)) {
        return
      }
      const anchor = target.closest('a')
      const href = anchor?.getAttribute('href')
      if (anchor === null || href === null || href === undefined || href === '') {
        return
      }
      if (anchor.hasAttribute('download')) {
        return
      }
      const explicitTarget = anchor.getAttribute('target')
      if (explicitTarget !== null && explicitTarget !== '' && explicitTarget !== '_self') {
        return
      }
      const url = new URL(href, window.location.href)
      if (url.origin !== window.location.origin) {
        return
      }
      const to = `${url.pathname}${url.search}${url.hash}`
      if (to === hereRef.current) {
        return
      }
      event.preventDefault()
      setPending(to)
    }

    window.addEventListener('beforeunload', onBeforeUnload)
    // Capture, so this runs before the `Link`'s own bubble-phase handler.
    document.addEventListener('click', onClick, true)
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload)
      document.removeEventListener('click', onClick, true)
    }
  }, [active])

  const confirm = useCallback((): void => {
    if (pending !== null) {
      void navigate(pending)
    }
    setPending(null)
  }, [navigate, pending])

  const cancel = useCallback((): void => {
    setPending(null)
  }, [])

  return { asking: pending !== null, confirm, cancel }
}
