import { describe, expect, it } from 'vitest'

import { kenBurnsMotion, kenBurnsStyle, panLimit } from './kenBurns'

/** A spread of uids wide enough to exercise every hash slice (direction/zoom/depth). */
const UIDS = Array.from({ length: 200 }, (_, i) => `pq${String(i).padStart(6, '0')}xyz`)

describe('kenBurnsMotion', () => {
  it('runs for the whole slide: the duration is the interval', () => {
    for (const intervalMs of [1000, 2000, 3000, 5000, 10000, 15000, 30000]) {
      expect(kenBurnsMotion('pq0001', intervalMs).durationMs).toBe(intervalMs)
    }
  })

  it('changes only the duration when the interval changes', () => {
    const short = kenBurnsMotion('pq0001', 2000)
    const long = kenBurnsMotion('pq0001', 30000)

    expect(short.durationMs).toBe(2000)
    expect(long.durationMs).toBe(30000)
    expect({ ...short, durationMs: 0 }).toEqual({ ...long, durationMs: 0 })
  })

  it('is deterministic: the same uid always yields the same motion', () => {
    for (const uid of UIDS) {
      expect(kenBurnsMotion(uid, 5000)).toEqual(kenBurnsMotion(uid, 5000))
    }
  })

  it('varies between photos, so a show does not look mechanical', () => {
    const motions = UIDS.map((uid) => JSON.stringify(kenBurnsMotion(uid, 5000)))
    const distinct = new Set(motions)

    // 8 pan directions × 2 zoom senses × 5 zoom depths = 80 possible motions.
    expect(distinct.size).toBeGreaterThan(20)
  })

  it('always zooms: one endpoint is scaled further in than the other', () => {
    for (const uid of UIDS) {
      const { fromScale, toScale } = kenBurnsMotion(uid, 5000)
      expect(fromScale).not.toBe(toScale)
    }
  })

  it('always pans: the endpoints differ on at least one axis', () => {
    for (const uid of UIDS) {
      const { fromX, fromY, toX, toY } = kenBurnsMotion(uid, 5000)
      expect(fromX !== toX || fromY !== toY).toBe(true)
    }
  })

  it('never reveals an edge: both endpoints stay within the pan slack of their scale', () => {
    for (const uid of UIDS) {
      const m = kenBurnsMotion(uid, 5000)

      // The image only covers the stage while it is scaled up.
      expect(m.fromScale).toBeGreaterThan(1)
      expect(m.toScale).toBeGreaterThan(1)

      // Offsets and scale both interpolate linearly, so bounding the endpoints
      // bounds every frame in between.
      expect(Math.abs(m.fromX)).toBeLessThanOrEqual(panLimit(m.fromScale))
      expect(Math.abs(m.fromY)).toBeLessThanOrEqual(panLimit(m.fromScale))
      expect(Math.abs(m.toX)).toBeLessThanOrEqual(panLimit(m.toScale))
      expect(Math.abs(m.toY)).toBeLessThanOrEqual(panLimit(m.toScale))
    }
  })

  it('never reveals an edge at any intermediate frame either', () => {
    const lerp = (a: number, b: number, p: number) => a + (b - a) * p

    for (const uid of UIDS.slice(0, 40)) {
      const m = kenBurnsMotion(uid, 5000)
      for (let step = 0; step <= 20; step += 1) {
        const p = step / 20
        const scale = lerp(m.fromScale, m.toScale, p)
        const limit = panLimit(scale)
        expect(Math.abs(lerp(m.fromX, m.toX, p))).toBeLessThanOrEqual(limit)
        expect(Math.abs(lerp(m.fromY, m.toY, p))).toBeLessThanOrEqual(limit)
      }
    }
  })
})

describe('panLimit', () => {
  it('is zero at scale 1 — an unscaled image has no slack to pan into', () => {
    expect(panLimit(1)).toBe(0)
  })

  it('grows with the scale: half the overhang, as a percentage', () => {
    expect(panLimit(1.2)).toBeCloseTo(10)
    expect(panLimit(1.5)).toBeCloseTo(25)
  })
})

describe('kenBurnsStyle', () => {
  it('emits the custom properties the keyframes read, with units', () => {
    const style = kenBurnsStyle('pq0001', 7500)
    const motion = kenBurnsMotion('pq0001', 7500)

    expect(style).toEqual({
      '--kb-from-scale': String(motion.fromScale),
      '--kb-from-x': `${motion.fromX}%`,
      '--kb-from-y': `${motion.fromY}%`,
      '--kb-to-scale': String(motion.toScale),
      '--kb-to-x': `${motion.toX}%`,
      '--kb-to-y': `${motion.toY}%`,
      '--kb-duration': '7500ms',
    })
  })

  it('derives the animation duration from the interval setting', () => {
    expect(kenBurnsStyle('pq0001', 1000)['--kb-duration']).toBe('1000ms')
    expect(kenBurnsStyle('pq0001', 30000)['--kb-duration']).toBe('30000ms')
  })
})
