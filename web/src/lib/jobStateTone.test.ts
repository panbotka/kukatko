import { describe, expect, it } from 'vitest'

import { countToneAlerts, countToneClass, toneForJobState, type CountTone } from './jobStateTone'

const ALL_TONES: CountTone[] = ['queued', 'running', 'failed', 'done', 'plain']

describe('toneForJobState', () => {
  it('maps the lifecycle states onto the four meanings a reader has', () => {
    expect(toneForJobState('queued')).toBe('queued')
    expect(toneForJobState('running')).toBe('running')
    expect(toneForJobState('done')).toBe('done')
  })

  it('treats a dead letter as the same bad news as a retryable failure', () => {
    expect(toneForJobState('failed')).toBe('failed')
    expect(toneForJobState('dead')).toBe('failed')
  })

  it('leaves a state it has never heard of uncoloured rather than guessing', () => {
    expect(toneForJobState('quarantined')).toBe('plain')
    expect(toneForJobState('')).toBe('plain')
  })
})

describe('countToneClass', () => {
  it('never colours a zero, whatever it counts', () => {
    for (const tone of ALL_TONES) {
      const cls = countToneClass(tone, 0)
      expect(cls).toBe('text-secondary')
      expect(cls).not.toMatch(/text-(info|primary|danger)/)
    }
  })

  it('colours the three states somebody can act on', () => {
    expect(countToneClass('queued', 12)).toContain('kk-count--queued')
    expect(countToneClass('running', 1)).toContain('kk-count--running')
    expect(countToneClass('failed', 3)).toContain('kk-count--failed')
  })

  it('marks running work as moving, and only running work', () => {
    expect(countToneClass('running', 1)).toContain('kk-count--running')
    for (const tone of ALL_TONES.filter((t) => t !== 'running')) {
      expect(countToneClass(tone, 7)).not.toContain('kk-count--running')
    }
    // Nothing is running, so nothing should be pulsing.
    expect(countToneClass('running', 0)).not.toContain('kk-count--running')
  })

  it('lets finished work recede — it is history, not a call to action', () => {
    expect(countToneClass('done', 41594)).toBe('text-secondary')
  })

  it('leaves a plain library count in the body colour', () => {
    expect(countToneClass('plain', 20930)).toBe('')
  })
})

describe('countToneAlerts', () => {
  it('flags a failure that is actually there', () => {
    expect(countToneAlerts('failed', 1)).toBe(true)
  })

  it('stays quiet for a cleared failure and for every other state', () => {
    expect(countToneAlerts('failed', 0)).toBe(false)
    for (const tone of ALL_TONES.filter((t) => t !== 'failed')) {
      expect(countToneAlerts(tone, 9)).toBe(false)
    }
  })
})
