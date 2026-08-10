import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useLightbox } from './useLightbox'

const ITEMS = ['a', 'b', 'c']

describe('useLightbox', () => {
  it('starts closed', () => {
    const { result } = renderHook(() => useLightbox(ITEMS))

    expect(result.current.isOpen).toBe(false)
    expect(result.current.item).toBeNull()
    expect(result.current.index).toBe(-1)
  })

  it('opens on an item and closes again', () => {
    const { result } = renderHook(() => useLightbox(ITEMS))

    act(() => {
      result.current.open(1)
    })
    expect(result.current.item).toBe('b')
    expect(result.current.isOpen).toBe(true)

    act(() => {
      result.current.close()
    })
    expect(result.current.isOpen).toBe(false)
    expect(result.current.item).toBeNull()
  })

  it('steps through the list and stops at both ends', () => {
    // Wrapping would silently re-show the first photo, which reads as a stuck
    // control rather than as "you have seen them all".
    const { result } = renderHook(() => useLightbox(ITEMS))

    act(() => {
      result.current.open(0)
    })
    expect(result.current.hasPrev).toBe(false)

    act(() => {
      result.current.prev()
    })
    expect(result.current.item).toBe('a')

    act(() => {
      result.current.next()
      result.current.next()
    })
    expect(result.current.item).toBe('c')
    expect(result.current.hasNext).toBe(false)

    act(() => {
      result.current.next()
    })
    expect(result.current.item).toBe('c')
  })

  it('follows the item at its index when the list is replaced in place', () => {
    // Confirming rebuilds the working list with a new status on the same
    // candidate; the lightbox must show the updated one, not a stale copy.
    const { rerender, result } = renderHook(({ items }) => useLightbox(items), {
      initialProps: { items: ITEMS },
    })

    act(() => {
      result.current.open(1)
    })
    rerender({ items: ['a', 'B!', 'c'] })

    expect(result.current.item).toBe('B!')
  })

  it('closes itself when the list shrinks past the open item', () => {
    // Rejecting the last candidate removes it; pointing past the end would
    // render an empty stage.
    const { rerender, result } = renderHook(({ items }) => useLightbox(items), {
      initialProps: { items: ITEMS },
    })

    act(() => {
      result.current.open(2)
    })
    rerender({ items: ['a'] })

    expect(result.current.isOpen).toBe(false)
    expect(result.current.item).toBeNull()
  })

  it('gives the focus back to whatever opened it', () => {
    // The card you came from is where the keyboard has to land, or a keyboard
    // user is dropped at the top of the document after every look.
    const card = document.createElement('button')
    document.body.append(card)
    card.focus()
    const { result } = renderHook(() => useLightbox(ITEMS))

    act(() => {
      result.current.open(0)
    })
    // Something inside the overlay takes the focus while it is open.
    act(() => {
      document.body.focus()
    })

    act(() => {
      result.current.close()
    })

    expect(document.activeElement).toBe(card)
    card.remove()
  })
})
