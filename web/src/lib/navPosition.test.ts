import { act, renderHook } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'

import {
  DEFAULT_NAV_POSITION,
  readNavPosition,
  useNavPosition,
  writeNavPosition,
} from './navPosition'

beforeEach(() => {
  window.localStorage.clear()
})

describe('nav position storage', () => {
  it('starts every account on the top bar', () => {
    expect(DEFAULT_NAV_POSITION).toBe('top')
    expect(readNavPosition('u1')).toBe('top')
  })

  it('reads back what an account chose, under its own uid', () => {
    writeNavPosition('u1', 'left')

    expect(readNavPosition('u1')).toBe('left')
    expect(window.localStorage.getItem('kukatko.navPosition.u1')).toBe('left')
  })

  it('keeps two accounts on one browser apart', () => {
    writeNavPosition('u1', 'left')
    writeNavPosition('u2', 'top')

    expect(readNavPosition('u1')).toBe('left')
    expect(readNavPosition('u2')).toBe('top')
    expect(readNavPosition('u3')).toBe('top')
  })

  it('neither reads nor writes without an account', () => {
    writeNavPosition(undefined, 'left')

    expect(window.localStorage.length).toBe(0)
    expect(readNavPosition(undefined)).toBe('top')
  })

  it('reads a value this build does not know as the default', () => {
    window.localStorage.setItem('kukatko.navPosition.u1', 'bottom')

    expect(readNavPosition('u1')).toBe('top')
  })

  it('falls back to the top bar when storage refuses to work', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })

    expect(() => {
      writeNavPosition('u1', 'left')
    }).not.toThrow()
    expect(readNavPosition('u1')).toBe('top')
  })
})

describe('useNavPosition', () => {
  /** A provider for a session owned by `uid` (just enough of the context). */
  function wrapper(uid: string) {
    const value = { user: { uid } } as unknown as AuthContextValue
    return ({ children }: { children: ReactNode }) =>
      createElement(AuthContext.Provider, { value }, children)
  }

  it('moves every reader the moment the account chooses, without a reload', () => {
    const first = renderHook(() => useNavPosition(), { wrapper: wrapper('u1') })
    const second = renderHook(() => useNavPosition(), { wrapper: wrapper('u1') })
    expect(first.result.current[0]).toBe('top')

    act(() => {
      first.result.current[1]('left')
    })

    expect(first.result.current[0]).toBe('left')
    expect(second.result.current[0]).toBe('left')
  })

  it('follows a choice another tab stores', () => {
    const { result } = renderHook(() => useNavPosition(), { wrapper: wrapper('u1') })

    act(() => {
      window.localStorage.setItem('kukatko.navPosition.u1', 'left')
      window.dispatchEvent(new StorageEvent('storage', { key: 'kukatko.navPosition.u1' }))
    })

    expect(result.current[0]).toBe('left')
  })

  it('is the default outside a session, and stores nothing', () => {
    const { result } = renderHook(() => useNavPosition())

    act(() => {
      result.current[1]('left')
    })

    expect(result.current[0]).toBe('top')
    expect(window.localStorage.length).toBe(0)
  })
})
