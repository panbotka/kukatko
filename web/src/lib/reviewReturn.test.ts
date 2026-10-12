import { describe, expect, it } from 'vitest'

import { reviewReturnPath, reviewReturnState } from './reviewReturn'

describe('reviewReturn', () => {
  it('round-trips the game location, source included', () => {
    const state = reviewReturnState('/review', '?source=people')

    expect(reviewReturnPath(state)).toBe('/review?source=people')
    expect(reviewReturnPath(reviewReturnState('/review', ''))).toBe('/review')
  })

  it('round-trips the game inside a project, project kept', () => {
    const state = reviewReturnState('/p/v/review', '?source=people')

    expect(reviewReturnPath(state)).toBe('/p/v/review?source=people')
    expect(reviewReturnPath(reviewReturnState('/p/v/review', ''))).toBe('/p/v/review')
  })

  it('reads anything that is not the game as "not from the game"', () => {
    expect(reviewReturnPath(undefined)).toBeUndefined()
    expect(reviewReturnPath(null)).toBeUndefined()
    expect(reviewReturnPath('/review')).toBeUndefined()
    expect(reviewReturnPath({})).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: 42 })).toBeUndefined()
    // A grid's photo handoff is not a way back to the game.
    expect(reviewReturnPath({ uid: 'p1', previewUrl: '/t/p1' })).toBeUndefined()
    // Only the game's own route: never another page, never another origin.
    expect(reviewReturnPath({ reviewReturn: '/reviewers' })).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: '/photos' })).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: '/p/v/photos' })).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: '/p/v' })).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: 'https://evil.example/review' })).toBeUndefined()
    expect(reviewReturnPath({ reviewReturn: '//evil.example/review' })).toBeUndefined()
  })
})
