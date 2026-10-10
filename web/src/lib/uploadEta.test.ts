import { describe, expect, it } from 'vitest'

import {
  batchBytes,
  bytesFraction,
  ETA_MIN_DATA_MS,
  etaPhrase,
  INITIAL_THROUGHPUT,
  remainingSeconds,
  sampleThroughput,
  type ThroughputState,
} from './uploadEta'

/** A queued file of `size` bytes in `status`. */
function item(size: number, status: string, progress = 0, interrupted?: boolean) {
  return { file: { size }, status, progress, interrupted }
}

/** Feeds `bytesAt(second)` once a second for `seconds` seconds. */
function feed(seconds: number, bytesAt: (second: number) => number): ThroughputState {
  let state = INITIAL_THROUGHPUT
  for (let second = 0; second <= seconds; second += 1) {
    state = sampleThroughput(state, second * 1000, bytesAt(second))
  }
  return state
}

describe('batchBytes', () => {
  it('counts settled files whole, a running one by its progress, a waiting one not at all', () => {
    const bytes = batchBytes([
      item(100, 'created'),
      item(200, 'duplicate'),
      item(300, 'error'),
      item(400, 'uploading', 0.5),
      item(500, 'queued'),
    ])
    expect(bytes).toEqual({ done: 800, total: 1500 })
    expect(bytesFraction(bytes)).toBeCloseTo(800 / 1500)
  })

  it('does not count an interrupted file as sent, since it goes again from zero', () => {
    expect(batchBytes([item(100, 'error', 0.7, true), item(100, 'created')])).toEqual({
      done: 100,
      total: 200,
    })
  })

  it('is zero for an empty batch', () => {
    expect(bytesFraction(batchBytes([]))).toBe(0)
  })
})

describe('sampleThroughput', () => {
  it('says nothing until a few seconds of data are in', () => {
    const early = feed(ETA_MIN_DATA_MS / 1000 - 1, (s) => s * 1000)
    expect(early.rate).toBeNull()
    expect(remainingSeconds(early, 10_000)).toBeNull()

    const enough = feed(ETA_MIN_DATA_MS / 1000, (s) => s * 1000)
    expect(enough.rate).toBeCloseTo(1000)
    expect(remainingSeconds(enough, 10_000)).toBeCloseTo(10)
  })

  it('smooths a sudden change in speed instead of jumping to it', () => {
    // 1000 B/s for 30 s, then one second at ten times the speed.
    let state = feed(30, (s) => s * 1000)
    expect(state.rate).toBeCloseTo(1000)
    state = sampleThroughput(state, 31_000, 30_000 + 10_000)
    expect(state.rate).not.toBeNull()
    // The window rate moved by ~45 %, the smoothed one by a fifth of that.
    expect(state.rate ?? 0).toBeGreaterThan(1000)
    expect(state.rate ?? 0).toBeLessThan(1150)
  })

  it('forgets measurements older than the window', () => {
    // Slow for a minute, then fast for half a minute: the old pace has left the window.
    let state = feed(60, (s) => s * 100)
    for (let s = 61; s <= 90; s += 1) {
      state = sampleThroughput(state, s * 1000, 6000 + (s - 60) * 10_000)
    }
    expect(state.samples.every((sample) => sample.at >= 70_000)).toBe(true)
    expect(state.rate ?? 0).toBeGreaterThan(9000)
  })

  it('restarts the window but keeps the estimate when the count goes down', () => {
    const state = feed(10, (s) => s * 1000)
    const reset = sampleThroughput(state, 11_000, 2000)
    expect(reset.samples).toEqual([{ at: 11_000, bytes: 2000 }])
    expect(reset.rate).toBe(state.rate)
  })

  it('has no estimate while nothing moves', () => {
    const stalled = feed(10, () => 5000)
    expect(stalled.rate).toBe(0)
    expect(remainingSeconds(stalled, 1000)).toBeNull()
  })
})

describe('etaPhrase', () => {
  it('rounds to units a person would say', () => {
    expect(etaPhrase(5)).toEqual({ unit: 'lessThanMinute' })
    expect(etaPhrase(59)).toEqual({ unit: 'lessThanMinute' })
    expect(etaPhrase(60)).toEqual({ unit: 'minutes', count: 1 })
    expect(etaPhrase(170)).toEqual({ unit: 'minutes', count: 3 })
    expect(etaPhrase(9 * 60 + 20)).toEqual({ unit: 'minutes', count: 9 })
    expect(etaPhrase(12 * 60)).toEqual({ unit: 'minutes', count: 10 })
    expect(etaPhrase(23 * 60)).toEqual({ unit: 'minutes', count: 25 })
    expect(etaPhrase(80 * 60)).toEqual({ unit: 'minutes', count: 80 })
    expect(etaPhrase(100 * 60)).toEqual({ unit: 'hours', count: 2 })
    expect(etaPhrase(4.4 * 3600)).toEqual({ unit: 'hours', count: 4 })
  })

  it('changes its wording rarely as the estimate drifts', () => {
    // Two minutes of drift read as at most two phrasings, never one per second.
    const phrases = new Set<string>()
    for (let s = 22 * 60; s < 24 * 60; s += 1) {
      phrases.add(JSON.stringify(etaPhrase(s)))
    }
    expect(phrases.size).toBeLessThanOrEqual(2)
  })
})
