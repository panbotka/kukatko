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

/** A held-back leave: an in-app URL to go to, or a history step to repeat. */
type Pending = { kind: 'navigate'; to: string } | { kind: 'traverse'; delta: number }

/** The guarded history entry: its `history.state` and its full URL. */
interface GuardedEntry {
  state: unknown
  href: string
}

/** The router's position stamp (`idx`) on a history state, if it carries one. */
function historyIndex(state: unknown): number | null {
  if (typeof state !== 'object' || state === null || !('idx' in state)) {
    return null
  }
  const { idx } = state
  return typeof idx === 'number' ? idx : null
}

/** The active guard's Back/Forward handler, if any page is guarded right now. */
let traversalHolder: ((event: PopStateEvent) => void) | null = null
let listeningForTraversals = false

/**
 * Installs the one `popstate` listener every {@link useLeaveGuard} holds
 * Back/Forward through. Idempotent. **Call it before the router mounts**
 * (`main.tsx` does, and so must a test): listeners on `window` run in the order
 * they were added, so only one added before react-router's own runs first and
 * can stop it. A capture flag would not help — Chrome ignores it for the order
 * on `window` (measured: a bubble listener added first ran before a capture one
 * added later) — so this is a plain listener, and jsdom then orders it the way
 * Chrome does.
 */
export function listenForHistoryLeaves(): void {
  if (listeningForTraversals) {
    return
  }
  listeningForTraversals = true
  window.addEventListener('popstate', (event) => {
    traversalHolder?.(event)
  })
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
 * Three mechanisms, because the browser owns only one of them outright:
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
 * - **Back / Forward:** the `popstate` listener of {@link listenForHistoryLeaves},
 *   added before react-router's own so it runs first and stops it with
 *   `stopImmediatePropagation()`, so the router never learns the entry moved and
 *   the page — and its queue — stays mounted. The browser has already moved by
 *   then, so the guard steps back to the guarded entry (`history.go` by the
 *   distance the router's `idx` stamps give) and asks; {@link LeaveGuard.confirm}
 *   repeats the step for real and lets it through to the router, so Back still
 *   means Back and Forward keeps working. An entry without an `idx` (pushed
 *   outside the router) has no known distance: the guarded entry is pushed back
 *   on top and confirm navigates to the URL the browser went to. A full-document
 *   Back is a page unload and is `beforeunload`'s.
 *
 * Only ordinary in-app link activations are held: a modified click (a new tab),
 * an explicit `target`, a `download`, an external origin, and a link back to the
 * page we are already on all pass straight through, because none of them
 * destroys the queue. A navigation made in code (`navigate(...)`) is not a click
 * and is likewise not held — including the one `confirm` itself performs — but
 * a `navigate(-1)` is a history traversal and is held like the Back button.
 */
export function useLeaveGuard(active: boolean): LeaveGuard {
  const navigate = useNavigate()
  const location = useLocation()
  const [pending, setPending] = useState<Pending | null>(null)

  // The current in-app URL, read from the router rather than `window.location`
  // so this is right under a memory router too.
  const here = `${location.pathname}${location.search}${location.hash}`
  const hereRef = useRef(here)
  hereRef.current = here

  // The guarded history entry as the browser holds it — its state (the router's
  // `idx` stamp) and full URL — taken once the router has written it, so a Back
  // can be measured against it and undone.
  const entryRef = useRef<GuardedEntry | null>(null)
  useEffect(() => {
    entryRef.current = { state: window.history.state as unknown, href: window.location.href }
  }, [here])

  // The guard's own traversals: the step back onto the guarded entry (swallowed
  // too), and the confirmed one (let through to the router).
  const restoringRef = useRef(false)
  const passingRef = useRef(false)

  useEffect(() => {
    if (!active) {
      // Nothing left to lose: drop a question that is no longer worth asking
      // (the batch finished while the dialog was up).
      setPending(null)
      return
    }
    restoringRef.current = false
    passingRef.current = false

    const onPopState = (event: PopStateEvent): void => {
      if (restoringRef.current) {
        // The guard stepping back onto its own entry: nothing moved for the router.
        restoringRef.current = false
        event.stopImmediatePropagation()
        return
      }
      if (passingRef.current) {
        passingRef.current = false
        return
      }
      const entry = entryRef.current
      if (entry === null) {
        return
      }
      const from = historyIndex(entry.state)
      const to = historyIndex(event.state)
      if (from !== null && to !== null) {
        if (from === to) {
          return
        }
        event.stopImmediatePropagation()
        restoringRef.current = true
        window.history.go(from - to)
        setPending({ kind: 'traverse', delta: to - from })
        return
      }
      // No known distance: put the guarded entry back on top and remember where
      // the browser went.
      event.stopImmediatePropagation()
      const target = `${window.location.pathname}${window.location.search}${window.location.hash}`
      window.history.pushState(entry.state, '', entry.href)
      setPending({ kind: 'navigate', to: target })
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
      setPending({ kind: 'navigate', to })
    }

    traversalHolder = onPopState
    window.addEventListener('beforeunload', onBeforeUnload)
    // Capture, so this runs before the `Link`'s own bubble-phase handler.
    document.addEventListener('click', onClick, true)
    return () => {
      if (traversalHolder === onPopState) {
        traversalHolder = null
      }
      window.removeEventListener('beforeunload', onBeforeUnload)
      document.removeEventListener('click', onClick, true)
    }
  }, [active])

  const confirm = useCallback((): void => {
    if (pending?.kind === 'navigate') {
      void navigate(pending.to)
    } else if (pending?.kind === 'traverse') {
      passingRef.current = true
      window.history.go(pending.delta)
    }
    setPending(null)
  }, [navigate, pending])

  const cancel = useCallback((): void => {
    setPending(null)
  }, [])

  return { asking: pending !== null, confirm, cancel }
}
