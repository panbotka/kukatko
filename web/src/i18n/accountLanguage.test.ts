import { renderHook } from '@testing-library/react'
import { createInstance, type i18n as I18n } from 'i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type AuthStatus } from '../auth/AuthContext'

import {
  readLanguagePreference,
  useAccountLanguage,
  writeLanguagePreference,
} from './accountLanguage'
import { initOptions } from './index'

/** A throwaway i18next on the app's own options, so the app's instance is untouched. */
async function freshInstance(): Promise<I18n> {
  const instance = createInstance()
  await instance.init(initOptions)
  return instance
}

beforeEach(() => {
  window.localStorage.clear()
})

describe('language preference storage', () => {
  it('reads back what an account chose', () => {
    writeLanguagePreference('u1', 'en')

    expect(readLanguagePreference('u1')).toBe('en')
    expect(window.localStorage.getItem('kukatko.language.u1')).toBe('en')
  })

  it('reads another account’s choice as no choice at all', () => {
    writeLanguagePreference('u1', 'en')

    expect(readLanguagePreference('u2')).toBeNull()
  })

  it('neither reads nor writes without an account', () => {
    writeLanguagePreference(undefined, 'en')

    expect(window.localStorage.length).toBe(0)
    expect(readLanguagePreference(undefined)).toBeNull()
  })

  it('reads a value this build does not ship as no choice', () => {
    window.localStorage.setItem('kukatko.language.u1', 'de')

    expect(readLanguagePreference('u1')).toBeNull()
  })

  it('survives storage that refuses to work', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })

    expect(() => {
      writeLanguagePreference('u1', 'en')
    }).not.toThrow()
    expect(readLanguagePreference('u1')).toBeNull()
  })
})

describe('useAccountLanguage', () => {
  /** Mounts the hook on `instance` for a session in `status` owned by `user`. */
  function mount(instance: I18n, status: AuthStatus, user?: string) {
    return renderHook(
      ({ s, u }: { s: AuthStatus; u?: string }) => {
        useAccountLanguage(instance, s, u)
      },
      { initialProps: { s: status, u: user } },
    )
  }

  it('puts the UI in the signed-in account’s own language', async () => {
    const instance = await freshInstance()
    writeLanguagePreference('u1', 'en')

    mount(instance, 'authenticated', 'u1')

    await vi.waitFor(() => {
      expect(instance.language).toBe('en')
    })
  })

  it('gives the next account on the browser the default, not the previous one’s choice', async () => {
    const instance = await freshInstance()
    writeLanguagePreference('u1', 'en')
    const view = mount(instance, 'authenticated', 'u1')
    await vi.waitFor(() => {
      expect(instance.language).toBe('en')
    })

    view.rerender({ s: 'authenticated', u: 'u2' })

    await vi.waitFor(() => {
      expect(instance.language).toBe('cs')
    })
  })

  it('returns to the default once nobody is signed in', async () => {
    const instance = await freshInstance()
    await instance.changeLanguage('en')

    mount(instance, 'unauthenticated')

    await vi.waitFor(() => {
      expect(instance.language).toBe('cs')
    })
  })

  it('leaves the language alone while the session is unknown', async () => {
    const instance = await freshInstance()
    await instance.changeLanguage('en')

    const view = mount(instance, 'loading')
    view.rerender({ s: 'unreachable', u: undefined })

    expect(instance.language).toBe('en')
  })
})
