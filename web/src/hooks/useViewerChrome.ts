import { useEffect, useRef, useState } from 'react'

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

/**
 * The viewer's chrome surfaces — the ones `viewer.css` fades under
 * `[data-chrome='hidden']`. Keyboard focus inside any of them pins the chrome
 * visible, so a control reached with Tab never melts away (and stops taking
 * input) under the focus ring. Keep it in step with the idle rules in
 * `viewer.css`; `PhotoDetailPage.test.tsx` checks that every entry has one.
 */
export const VIEWER_CHROME_SELECTOR =
  '.kk-viewer__back, .kk-viewer__chrome, .kk-viewer__nav, .kk-viewer__dock'

/** Options for {@link useViewerChrome}. */
export interface UseViewerChromeOptions {
  /** Pin the chrome visible (the drawer or a menu is open). */
  paused: boolean
  /**
   * Whether this device is pointed at with a finger. Only then does the chrome
   * auto-hide at all: with a mouse a pointer resting on the page is no reason to
   * take the controls away, so the chrome simply stays (and neither the first-run
   * hold nor the hint applies). On a phone the photo gets the screen to itself
   * after an idle, and the only way back is a tap nobody announced — which is
   * what the hold and the hint are for.
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
 * Tracks whether keyboard focus sits inside the viewer chrome.
 *
 * Only *keyboard* focus counts: a tap on a button focuses it in some mobile
 * browsers, and pinning the chrome for that would stop it ever fading on a phone
 * after the first star given. So the last input decides — a key press arms it, a
 * pointer press disarms it — and focus arriving in the chrome pins only while
 * armed. Leaving the chrome (focus moving outside it, or nowhere) unpins.
 */
function useKeyboardFocusInChrome(): boolean {
  const [focused, setFocused] = useState(false)
  const keyboard = useRef(false)

  useEffect(() => {
    const inChrome = (target: EventTarget | null): boolean =>
      target instanceof Element && target.closest(VIEWER_CHROME_SELECTOR) !== null
    const onKeyDown = () => {
      keyboard.current = true
    }
    const onPointerDown = () => {
      keyboard.current = false
    }
    const onFocusIn = (event: FocusEvent) => {
      setFocused(keyboard.current && inChrome(event.target))
    }
    const onFocusOut = (event: FocusEvent) => {
      if (!inChrome(event.relatedTarget)) {
        setFocused(false)
      }
    }
    window.addEventListener('keydown', onKeyDown, true)
    window.addEventListener('pointerdown', onPointerDown, true)
    document.addEventListener('focusin', onFocusIn)
    document.addEventListener('focusout', onFocusOut)
    return () => {
      window.removeEventListener('keydown', onKeyDown, true)
      window.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('focusin', onFocusIn)
      document.removeEventListener('focusout', onFocusOut)
    }
  }, [])

  return focused
}

/**
 * The photo viewer's chrome: {@link useAutoHideChrome}, gated to touch devices,
 * plus the two things that make its disappearance survivable on a phone.
 *
 * With a hovering pointer the chrome never auto-hides: the pointer resting on
 * the page is not a reason to remove the controls, and a half-faded bar read as a
 * broken one. On a touch device it fades as a whole after the idle and a tap
 * brings it back; keyboard focus inside it (see {@link VIEWER_CHROME_SELECTOR})
 * pins it either way.
 *
 * A viewer whose controls have vanished looks broken to someone who does not
 * know that a tap brings them back, so the first time a touch device opens the
 * viewer this hook (a) holds the chrome up for {@link FIRST_RUN_HOLD_MS} before the idle countdown starts, and (b) shows a
 * hint the moment the chrome does fade, saying that a tap brings it back. The
 * hint goes after {@link CHROME_HINT_MS}, or the instant the chrome returns —
 * whichever comes first, because a hint about something that has just happened is
 * only clutter.
 *
 * Both are strictly one-off per device (`lib/viewerChromeHint` persists the flag):
 * a hint that reappears on the fiftieth photograph is worse than no hint, and the
 * gesture is learnt once. Neither changes the auto-hide itself — they change
 * *when* a reader is first told, not *whether* the photograph eventually gets the
 * screen to itself.
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
  const keyboardFocus = useKeyboardFocusInChrome()
  // Without touch the chrome is simply pinned: the timer gating rides `paused`,
  // the one switch the auto-hide already understands.
  const chrome = useAutoHideChrome({ paused: paused || hold || !touch || keyboardFocus })

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
