import { describe, expect, it } from 'vitest'

import { handoffPreviewUrl } from './photoHandoff'

describe('handoffPreviewUrl', () => {
  it('takes the address the grid painted for this very photo', () => {
    const state = { uid: 'p1', previewUrl: 'https://media.example/p1.jpg?sig=abc' }
    expect(handoffPreviewUrl(state, 'p1')).toBe('https://media.example/p1.jpg?sig=abc')
  })

  it('discards a handoff belonging to another photo', () => {
    const state = { uid: 'p1', previewUrl: 'https://media.example/p1.jpg' }
    expect(handoffPreviewUrl(state, 'p2')).toBeUndefined()
  })

  it('discards an empty address rather than pointing an image at the page', () => {
    expect(handoffPreviewUrl({ uid: 'p1', previewUrl: '' }, 'p1')).toBeUndefined()
  })

  it('discards anything that is not a handoff', () => {
    expect(handoffPreviewUrl(null, 'p1')).toBeUndefined()
    expect(handoffPreviewUrl(undefined, 'p1')).toBeUndefined()
    expect(handoffPreviewUrl('p1', 'p1')).toBeUndefined()
    expect(handoffPreviewUrl({ uid: 'p1' }, 'p1')).toBeUndefined()
    expect(handoffPreviewUrl({ uid: 1, previewUrl: 'x' }, 'p1')).toBeUndefined()
  })
})
