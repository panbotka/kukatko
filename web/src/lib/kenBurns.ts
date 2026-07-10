import { type CSSProperties } from 'react'

/**
 * A Ken Burns motion: the transform endpoints of the slow zoom-and-pan played
 * across a single slide. `x`/`y` are percentages of the image's own (unscaled)
 * box; the transform applies them as `translate(x%, y%) scale(scale)`, so the
 * translation is *not* multiplied by the scale.
 */
export interface KenBurnsMotion {
  /** Scale at the start of the slide. */
  fromScale: number
  /** Horizontal offset at the start of the slide, in % of the image width. */
  fromX: number
  /** Vertical offset at the start of the slide, in % of the image height. */
  fromY: number
  /** Scale at the end of the slide. */
  toScale: number
  /** Horizontal offset at the end of the slide, in % of the image width. */
  toX: number
  /** Vertical offset at the end of the slide, in % of the image height. */
  toY: number
  /** How long the motion runs — the full length of the slide, in ms. */
  durationMs: number
}

/** A style object carrying the `--kb-*` custom properties `slideshow.css` reads. */
export type KenBurnsStyle = CSSProperties & Record<`--kb-${string}`, string>

/**
 * The scale the image never drops below. The image covers the stage exactly at
 * scale 1, so a slide needs headroom on both endpoints to have anything to pan
 * into; at 1.05 even the "zoomed out" endpoint can drift a little.
 */
const MIN_SCALE = 1.05

/** The smallest and largest "zoomed in" endpoint. Varied per photo for texture. */
const MAX_SCALE_MIN = 1.16
const MAX_SCALE_STEP = 0.02
const MAX_SCALE_STEPS = 5

/**
 * Fraction of the theoretically available pan slack we actually use. At scale
 * `s` the image overhangs the stage by `(s - 1) / 2` on each side, so a pan of
 * exactly that much would land an edge on the stage border. Staying at 90 %
 * keeps a margin against sub-pixel rounding.
 */
const PAN_SAFETY = 0.9

/** The pan directions, as unit steps on each axis: the 4 edges and 4 corners. */
const PAN_DIRECTIONS: readonly (readonly [number, number])[] = [
  [1, 0],
  [1, 1],
  [0, 1],
  [-1, 1],
  [-1, 0],
  [-1, -1],
  [0, -1],
  [1, -1],
]

/** Rounds to 4 decimals so the emitted CSS stays short and comparisons are stable. */
function round(value: number): number {
  return Math.round(value * 10000) / 10000
}

/**
 * FNV-1a, 32-bit. A tiny, well-mixed, dependency-free string hash: the same uid
 * always yields the same bits, so replaying an album looks identical.
 */
function hash32(value: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < value.length; i += 1) {
    h ^= value.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return h >>> 0
}

/**
 * The pan slack available at a given scale, in % of the image box: how far the
 * image may be translated before an edge would expose the stage behind it.
 */
export function panLimit(scale: number): number {
  return ((scale - 1) / 2) * 100
}

/**
 * Derives the Ken Burns motion for a slide deterministically from the photo's
 * uid: the same uid always produces the same zoom direction, pan direction and
 * zoom depth, so a re-played album looks exactly the same. `durationMs` is the
 * slide interval, so the motion spans the whole time the photo is on screen.
 *
 * The endpoints are chosen so the image covers the stage throughout. Both the
 * scale and the offsets interpolate linearly, and each endpoint keeps its
 * offset within {@link panLimit} of its own scale; because both sides of that
 * inequality are linear in the animation's progress, it therefore holds at
 * every frame in between. No edge is ever revealed.
 */
export function kenBurnsMotion(uid: string, intervalMs: number): KenBurnsMotion {
  const h = hash32(uid)

  // Independent bit slices: direction (low 3), zoom sense (bit 3), depth (rest).
  const [dx, dy] = PAN_DIRECTIONS[h % PAN_DIRECTIONS.length]
  const zoomIn = ((h >>> 3) & 1) === 0
  const maxScale = MAX_SCALE_MIN + ((h >>> 4) % MAX_SCALE_STEPS) * MAX_SCALE_STEP

  const fromScale = zoomIn ? MIN_SCALE : maxScale
  const toScale = zoomIn ? maxScale : MIN_SCALE

  // Pan across the direction: start behind the centre, end ahead of it. Each
  // endpoint uses the slack of its *own* scale, so neither over-travels.
  const fromAmp = PAN_SAFETY * panLimit(fromScale)
  const toAmp = PAN_SAFETY * panLimit(toScale)

  return {
    fromScale: round(fromScale),
    fromX: round(-dx * fromAmp),
    fromY: round(-dy * fromAmp),
    toScale: round(toScale),
    toX: round(dx * toAmp),
    toY: round(dy * toAmp),
    durationMs: intervalMs,
  }
}

/**
 * Renders a {@link kenBurnsMotion} as the `--kb-*` custom properties consumed by
 * the `slideshow-kenburns` keyframes. Keeping the keyframes in CSS (and only the
 * endpoints in JS) lets the browser run the animation off the main thread.
 */
export function kenBurnsStyle(uid: string, intervalMs: number): KenBurnsStyle {
  const motion = kenBurnsMotion(uid, intervalMs)
  return {
    '--kb-from-scale': String(motion.fromScale),
    '--kb-from-x': `${motion.fromX}%`,
    '--kb-from-y': `${motion.fromY}%`,
    '--kb-to-scale': String(motion.toScale),
    '--kb-to-x': `${motion.toX}%`,
    '--kb-to-y': `${motion.toY}%`,
    '--kb-duration': `${motion.durationMs}ms`,
  }
}
