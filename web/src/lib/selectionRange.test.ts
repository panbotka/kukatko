import { describe, expect, it } from 'vitest'

import { rangeBetween } from './selectionRange'

/** The grid order the ranges are walked over. */
const ORDER = ['a', 'b', 'c', 'd', 'e']

describe('rangeBetween', () => {
  it('walks forward from the anchor to the clicked item, both included', () => {
    expect(rangeBetween('b', 'd', ORDER)).toEqual(['b', 'c', 'd'])
  })

  it('walks backwards when the clicked item precedes the anchor', () => {
    expect(rangeBetween('d', 'b', ORDER)).toEqual(['b', 'c', 'd'])
  })

  it('is the single item when the anchor is the item clicked', () => {
    expect(rangeBetween('c', 'c', ORDER)).toEqual(['c'])
  })

  it('has no range without an anchor', () => {
    expect(rangeBetween(null, 'c', ORDER)).toBeNull()
  })

  it('has no range when the anchor is no longer in the grid', () => {
    expect(rangeBetween('gone', 'c', ORDER)).toBeNull()
  })

  it('has no range when the clicked item is not in the grid', () => {
    expect(rangeBetween('b', 'gone', ORDER)).toBeNull()
  })

  it('has no range over an empty grid', () => {
    expect(rangeBetween('a', 'b', [])).toBeNull()
  })

  it('spans the loaded order it is given, not the positions behind it', () => {
    // The grid hands over only the photos it has loaded, so a range across a
    // hole in a windowed list covers what is known and nothing else.
    expect(rangeBetween('a', 'e', ['a', 'b', 'e'])).toEqual(['a', 'b', 'e'])
  })
})
