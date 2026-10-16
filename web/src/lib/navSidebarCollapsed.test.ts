import { act, renderHook } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { clearSignedOutState } from '../auth/signOutStorage'

import {
  DEFAULT_NAV_SIDEBAR_COLLAPSED,
  readNavSidebarCollapsed,
  useNavSidebarCollapsed,
  writeNavSidebarCollapsed,
} from './navSidebarCollapsed'

beforeEach(() => {
  window.localStorage.clear()
})

describe('sidebar fold storage', () => {
  it('starts every account expanded', () => {
    expect(DEFAULT_NAV_SIDEBAR_COLLAPSED).toBe(false)
    expect(readNavSidebarCollapsed('u1')).toBe(false)
  })

  it('round-trips the fold under the account’s own uid', () => {
    writeNavSidebarCollapsed('u1', true)
    expect(readNavSidebarCollapsed('u1')).toBe(true)
    expect(window.localStorage.getItem('kukatko.navSidebarCollapsed.u1')).toBe('collapsed')

    writeNavSidebarCollapsed('u1', false)
    expect(readNavSidebarCollapsed('u1')).toBe(false)
    expect(window.localStorage.getItem('kukatko.navSidebarCollapsed.u1')).toBe('expanded')
  })

  it('keeps two accounts on one browser apart', () => {
    writeNavSidebarCollapsed('u1', true)

    expect(readNavSidebarCollapsed('u1')).toBe(true)
    expect(readNavSidebarCollapsed('u2')).toBe(false)
  })

  it('neither reads nor writes without an account', () => {
    writeNavSidebarCollapsed(undefined, true)

    expect(window.localStorage.length).toBe(0)
    expect(readNavSidebarCollapsed(undefined)).toBe(false)
  })

  it('reads a value this build does not know as expanded', () => {
    window.localStorage.setItem('kukatko.navSidebarCollapsed.u1', 'sideways')

    expect(readNavSidebarCollapsed('u1')).toBe(false)
  })

  it('falls back to expanded when storage refuses to work', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })

    expect(() => {
      writeNavSidebarCollapsed('u1', true)
    }).not.toThrow()
    expect(readNavSidebarCollapsed('u1')).toBe(false)
  })

  it('survives sign-out', () => {
    writeNavSidebarCollapsed('u1', true)

    clearSignedOutState()

    expect(readNavSidebarCollapsed('u1')).toBe(true)
  })
})

describe('useNavSidebarCollapsed', () => {
  /** A provider for a session owned by `uid` (just enough of the context). */
  function wrapper(uid: string) {
    const value = { user: { uid } } as unknown as AuthContextValue
    return ({ children }: { children: ReactNode }) =>
      createElement(AuthContext.Provider, { value }, children)
  }

  it('moves every reader the moment the account folds, without a reload', () => {
    const first = renderHook(() => useNavSidebarCollapsed(), { wrapper: wrapper('u1') })
    const second = renderHook(() => useNavSidebarCollapsed(), { wrapper: wrapper('u1') })
    expect(first.result.current[0]).toBe(false)

    act(() => {
      first.result.current[1](true)
    })

    expect(first.result.current[0]).toBe(true)
    expect(second.result.current[0]).toBe(true)
  })

  it('follows a fold another tab stores', () => {
    const { result } = renderHook(() => useNavSidebarCollapsed(), { wrapper: wrapper('u1') })

    act(() => {
      window.localStorage.setItem('kukatko.navSidebarCollapsed.u1', 'collapsed')
      window.dispatchEvent(new StorageEvent('storage', { key: 'kukatko.navSidebarCollapsed.u1' }))
    })

    expect(result.current[0]).toBe(true)
  })

  it('is the default outside a session, and stores nothing', () => {
    const { result } = renderHook(() => useNavSidebarCollapsed())

    act(() => {
      result.current[1](true)
    })

    expect(result.current[0]).toBe(false)
    expect(window.localStorage.length).toBe(0)
  })
})
