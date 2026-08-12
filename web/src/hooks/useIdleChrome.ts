import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * How long the player waits without any input before it takes its controls off
 * the picture. Three seconds is long enough to aim at a button you have just
 * revealed and short enough that the chrome is gone before the second slide.
 */
export const CHROME_IDLE_MS = 3000

/** Options for {@link useIdleChrome}. */
export interface UseIdleChromeOptions {
  /** How long without input before the chrome hides. Defaults to {@link CHROME_IDLE_MS}. */
  delayMs?: number
  /**
   * While true the chrome is pinned on screen and the countdown does not run —
   * the reader is *using* it (a panel is open, the pointer rests on it, focus is
   * inside it), and chrome that vanished from under a hand would be a trap.
   * Releasing the hold starts the countdown afresh.
   */
  held?: boolean
}

/** The chrome's visibility and the two ways it comes back. */
export interface IdleChrome {
  /** Whether the controls are currently on screen. */
  visible: boolean
  /** "Somebody is there": shows the chrome and restarts the countdown. */
  wake: () => void
  /** Shows the chrome if hidden, hides it if shown — what a tap on the picture does. */
  toggle: () => void
}

/**
 * Fades a player's chrome out when nothing happens, and brings it back when
 * something does.
 *
 * A slideshow on a television or a projector runs for an evening, and a control
 * bar that never leaves spends that whole evening competing with the photograph
 * for the same pixels. So the controls state their case at the start and then get
 * out of the way, exactly as every full-screen player does — but the hiding must
 * never be a trap, which is the whole reason this is a hook rather than a CSS
 * `:hover` rule:
 *
 *   - `wake()` is what a mouse movement or a key press calls: the chrome is back
 *     before the reader can look for it.
 *   - `toggle()` is what a tap calls, because a finger has no "movement" to
 *     report — on a touch screen the tap *is* the way to ask for the controls,
 *     and asking twice puts them away again.
 *   - `held` covers the cases where a countdown would be plain wrong: a settings
 *     panel open, the pointer resting on the bar, keyboard focus inside it.
 *
 * The visible state is the only thing that re-renders: `wake()` on every mouse
 * move sets `true` on an already-`true` state, which React bails out of, and the
 * countdown itself lives in a ref.
 */
export function useIdleChrome({
  delayMs = CHROME_IDLE_MS,
  held = false,
}: UseIdleChromeOptions = {}): IdleChrome {
  const [visible, setVisible] = useState(true)

  const timer = useRef<number | null>(null)
  const visibleRef = useRef(visible)
  visibleRef.current = visible
  const heldRef = useRef(held)
  heldRef.current = held

  const clear = useCallback((): void => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current)
      timer.current = null
    }
  }, [])

  // Restarts the countdown from now. A held chrome never gets one, so the timer
  // cannot fire the moment the hold is released.
  const arm = useCallback((): void => {
    clear()
    if (heldRef.current) {
      return
    }
    timer.current = window.setTimeout(() => {
      timer.current = null
      setVisible(false)
    }, delayMs)
  }, [clear, delayMs])

  const wake = useCallback((): void => {
    setVisible(true)
    arm()
  }, [arm])

  // Read through the ref rather than a functional update: hiding and showing
  // have to arm or clear the timer, and doing that inside an updater would run
  // it twice under StrictMode.
  const toggle = useCallback((): void => {
    if (visibleRef.current) {
      clear()
      setVisible(false)
      return
    }
    setVisible(true)
    arm()
  }, [arm, clear])

  // Taking the hold pins the chrome; releasing it starts a fresh countdown. The
  // same effect arms the first countdown on mount, so a show nobody touches ends
  // up showing only the photograph.
  useEffect(() => {
    if (held) {
      clear()
      setVisible(true)
    } else {
      arm()
    }
    return clear
  }, [held, arm, clear])

  return { visible, wake, toggle }
}
