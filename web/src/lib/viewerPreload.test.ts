import { describe, expect, it } from 'vitest'

import { isPreloadable, preloadUids } from './viewerPreload'

describe('isPreloadable', () => {
  it('warms an image, a live photo and a photo of unstated kind', () => {
    expect(isPreloadable({ uid: 'a', mediaType: 'image' })).toBe(true)
    expect(isPreloadable({ uid: 'b', mediaType: 'live' })).toBe(true)
    expect(isPreloadable({ uid: 'c' })).toBe(true)
  })

  it('never warms a video', () => {
    expect(isPreloadable({ uid: 'v', mediaType: 'video' })).toBe(false)
  })

  it('has nothing to warm at a list end', () => {
    expect(isPreloadable(null)).toBe(false)
  })
})

describe('preloadUids', () => {
  it('warms the photo on stage and one neighbour each side', () => {
    expect(preloadUids('b', { uid: 'a' }, { uid: 'c' })).toEqual(['b', 'a', 'c'])
  })

  it('leaves out a neighbour that is a video', () => {
    expect(preloadUids('b', { uid: 'a', mediaType: 'video' }, { uid: 'c' })).toEqual(['b', 'c'])
  })

  it('leaves out an absent neighbour at either end of the list', () => {
    expect(preloadUids('a', null, { uid: 'b' })).toEqual(['a', 'b'])
    expect(preloadUids('z', { uid: 'y' }, null)).toEqual(['z', 'y'])
  })

  it('names the photo on stage once even when a neighbour repeats it', () => {
    expect(preloadUids('a', { uid: 'a' }, { uid: 'a' })).toEqual(['a'])
  })

  it('warms nothing while the photo on stage is unknown', () => {
    expect(preloadUids('', null, null)).toEqual([])
  })

  it('skips an empty neighbour uid rather than requesting the page itself', () => {
    expect(preloadUids('a', { uid: '' }, null)).toEqual(['a'])
  })
})
