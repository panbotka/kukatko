import { useEffect, useState } from 'react'

import { markChromeHintSeen, readChromeHintSeen } from '../lib/viewerChromeHint'

import { type AutoHideChrome, useAutoHideChrome } from './useAutoHideChrome'

/**
 * How long the chrome is pinned up the first time a touch device opens the
 * viewer, before the ordinary idle countdown even starts.
 *
 * Once, and once only: a first-time reader gets time to see that there IS a top
 * bar and a dock before they melt away, which is half of what makes their
 * disappearance readable rather than alarming.
 */
export const FIRST_RUN_HOLD_MS = 4000

/** How long the hint itself stays on screen once the chrome has gone. */
export const CHROME_HINT_MS = 4000

/** Options for {@link useViewerChrome}. */
export interface UseViewerChromeOptions {
  /** Pin the chrome visible (the drawer or a menu is open). */
  paused: boolean
  /**
   * Whether this device is pointed at with a finger. With a mouse the chrome is
   * self-healing — the smallest move brings it back — so neither the first-run
   * hold nor the hint applies; on a phone the only way back is a tap nobody
   * announced, which is the whole reason this hook exists.
   */
  touch: boolean
}

/** The auto-hiding chrome plus the one-time "tap to bring it back" hint. */
export interface ViewerChrome extends AutoHideChrome {
  /** Whether the one-time hint should be on screen right now. */
  hintVisible: boolean
}

/** Where the one-time hint is in its life. */
type HintPhase = 'waiting' | 'showing' | 'done'

/**
 * The photo viewer's chrome: {@link useAutoHideChrome} plus the two things that
 * make its disappearance survivable on a phone.
 *
 * A viewer whose controls have vanished looks broken. With a mouse that never
 * happens — any move brings them back — but a touch device has no such gesture,
 * so the first time one opens the viewer this hook (a) holds the chrome up for
 * {@link FIRST_RUN_HOLD_MS} before the idle countdown starts, and (b) shows a
 * hint the moment the chrome does fade, saying that a tap brings it back. The
 * hint goes after {@link CHROME_HINT_MS}, or the instant the chrome returns —
 * whichever comes first, because a hint about something that has just happened is
 * only clutter.
 *
 * Both are strictly one-off per device (`lib/viewerChromeHint` persists the flag):
 * a hint that reappears on the fiftieth photograph is worse than no hint, and the
 * gesture is learnt once. The auto-hide itself is untouched — this changes *when*
 * a reader is first told, not *whether* the photograph eventually gets the screen
 * to itself.
 */
export function useViewerChrome({ paused, touch }: UseViewerChromeOptions): ViewerChrome {
  // Read once, at mount. The flag is written by this very hook, so re-reading it
  // later would only ever find our own write and cut the hint short.
  const [owed] = useState(() => !readChromeHintSeen())
  const [holding, setHolding] = useState(owed)
  const [phase, setPhase] = useState<HintPhase>(owed ? 'waiting' : 'done')

  // The first-run hold rides `paused`, which is what the chrome already
  // understands: pinned visible, no timer, and the ordinary countdown starts by
  // itself the moment the hold is released.
  const hold = holding && owed && touch
  const chrome = useAutoHideChrome({ paused: paused || hold })

  useEffect(() => {
    if (!hold) {
      return undefined
    }
    const timer = window.setTimeout(() => {
      setHolding(false)
    }, FIRST_RUN_HOLD_MS)
    return () => {
      window.clearTimeout(timer)
    }
  }, [hold])

  useEffect(() => {
    // The chrome has faded for the first time on this device: say how to get it
    // back, and record that it has been said.
    if (touch && phase === 'waiting' && !chrome.visible) {
      markChromeHintSeen()
      setPhase('showing')
    }
  }, [touch, phase, chrome.visible])

  useEffect(() => {
    if (phase !== 'showing') {
      return undefined
    }
    if (chrome.visible) {
      setPhase('done')
      return undefined
    }
    const timer = window.setTimeout(() => {
      setPhase('done')
    }, CHROME_HINT_MS)
    return () => {
      window.clearTimeout(timer)
    }
  }, [phase, chrome.visible])

  return { visible: chrome.visible, wake: chrome.wake, hintVisible: phase === 'showing' }
}
