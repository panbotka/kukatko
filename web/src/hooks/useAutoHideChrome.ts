import { useCallback, useEffect, useRef, useState } from 'react'

/** Idle time before the chrome fades, in milliseconds. */
const DEFAULT_IDLE_MS = 2600

/** Options for {@link useAutoHideChrome}. */
export interface UseAutoHideChromeOptions {
  /** Idle time before the chrome fades (ms). Defaults to {@link DEFAULT_IDLE_MS}. */
  idleMs?: number
  /**
   * When true the chrome is pinned visible and the idle timer never runs — for
   * when a control must stay reachable (the info drawer is open, so its toggle
   * and the actions beside it may not melt away under the user's hand).
   */
  paused?: boolean
}

/** The chrome's current visibility plus a manual wake. */
export interface AutoHideChrome {
  /** Whether the chrome should be shown (true) or faded out (false). */
  visible: boolean
  /**
   * Show the chrome now and restart the idle countdown. Call it from the
   * surface's own pointer/touch handlers so a tap on the photo reveals the
   * controls even where a global listener would miss it.
   */
  wake: () => void
}

/**
 * Auto-hiding chrome for an immersive surface (the full-bleed photo viewer). The
 * controls start visible, fade after `idleMs` of no pointer/keyboard/touch
 * activity, and reappear on the very next move, tap, key or focus — the way a
 * photo viewer recedes so the image owns the screen yet the controls are always
 * one gesture away.
 *
 * Activity is watched globally (pointer move/down, key, touch) so any interaction
 * anywhere wakes it. Visibility is tracked through a ref and only committed to
 * state on an actual change, so the flood of `pointermove` events never triggers
 * a re-render per frame. While `paused` the chrome is pinned visible and no timer
 * runs. The reduced-motion contract is honoured by the CSS transition (built on
 * the duration tokens, which collapse to ~0 under `prefers-reduced-motion`), so
 * this hook only decides *whether* the chrome shows, never *how* it animates.
 */
export function useAutoHideChrome(options: UseAutoHideChromeOptions = {}): AutoHideChrome {
  const { idleMs = DEFAULT_IDLE_MS, paused = false } = options
  const [visible, setVisible] = useState(true)
  const visibleRef = useRef(true)
  const timer = useRef<number | null>(null)

  const clearTimer = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current)
      timer.current = null
    }
  }, [])

  const commit = useCallback((next: boolean) => {
    if (visibleRef.current !== next) {
      visibleRef.current = next
      setVisible(next)
    }
  }, [])

  const wake = useCallback(() => {
    commit(true)
    clearTimer()
    if (!paused) {
      timer.current = window.setTimeout(() => {
        commit(false)
      }, idleMs)
    }
  }, [commit, clearTimer, idleMs, paused])

  useEffect(() => {
    // Paused: pin the chrome visible and run no timer — a control that must stay
    // reachable never fades under the pointer.
    if (paused) {
      commit(true)
      clearTimer()
      return
    }
    wake()
    const onActivity = () => {
      wake()
    }
    window.addEventListener('pointermove', onActivity)
    window.addEventListener('pointerdown', onActivity)
    window.addEventListener('keydown', onActivity)
    window.addEventListener('touchstart', onActivity)
    return () => {
      window.removeEventListener('pointermove', onActivity)
      window.removeEventListener('pointerdown', onActivity)
      window.removeEventListener('keydown', onActivity)
      window.removeEventListener('touchstart', onActivity)
      clearTimer()
    }
  }, [wake, commit, clearTimer, paused])

  return { visible, wake }
}
