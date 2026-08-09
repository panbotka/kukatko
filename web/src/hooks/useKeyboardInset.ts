import { useEffect, useState } from 'react'

/**
 * Anything under this is measurement noise (a browser toolbar collapsing, a
 * rounding difference between the two viewports) rather than a keyboard.
 */
const NOISE_PX = 80

/**
 * Above this zoom factor the visual viewport is short because the user pinched
 * into the photo, not because a keyboard came up — lifting the drawer then would
 * yank the panel around mid-gesture.
 */
const MAX_SCALE = 1.05

/**
 * The visual viewport, or null where there is none.
 *
 * The DOM types promise the property is always there (`VisualViewport | null`),
 * but it is genuinely *absent* in older browsers and in jsdom — so the widened
 * read is what makes the guards below honest rather than dead code that throws
 * the first time it runs somewhere without one.
 */
function viewport(): VisualViewport | null {
  return (window as { visualViewport?: VisualViewport | null }).visualViewport ?? null
}

/** The pixels currently occluded at the bottom of the layout viewport. */
function measure(): number {
  const vv = viewport()
  if (vv === null || vv.scale > MAX_SCALE) {
    return 0
  }
  const occluded = window.innerHeight - vv.height - vv.offsetTop
  return occluded > NOISE_PX ? Math.round(occluded) : 0
}

/**
 * How many pixels the on-screen keyboard covers at the bottom of the window.
 *
 * A phone keyboard does not shrink the layout viewport — on iOS Safari it simply
 * slides over it — so anything anchored to the bottom edge (the photo viewer's
 * bottom sheet, and with it the comment composer) ends up *behind* the keyboard the
 * moment the user taps into the input. The visual viewport, on the other hand, does
 * shrink, and the gap between the two is exactly the keyboard's height.
 *
 * The value is meant to be fed into a CSS custom property and used to lift whatever
 * must stay reachable. It is 0 on every desktop browser and on any browser without
 * `visualViewport`, so the layout is untouched unless a keyboard is actually up.
 */
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(0)

  useEffect(() => {
    const vv = viewport()
    if (vv === null) {
      return undefined
    }
    const update = (): void => {
      setInset(measure())
    }
    update()
    // `scroll` matters as much as `resize`: iOS reports the keyboard by offsetting
    // the visual viewport, which arrives as a scroll of it rather than a resize.
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => {
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
    }
  }, [])

  return inset
}
