import { beforeEach, describe, expect, it } from 'vitest'

import { clearSignedOutState } from './signOutStorage'

beforeEach(() => {
  window.localStorage.clear()
  window.sessionStorage.clear()
})

/**
 * What an account leaves behind, by storage, as the app writes it — per-account
 * copies for two accounts plus the global key an older build kept.
 */
const ACCOUNT_STATE = {
  local: [
    'kukatko.announcement.dismissedAt',
    'kukatko.announcement.dismissedAt.u1',
    'kukatko.announcement.dismissedAt.u2',
    'kukatko.whatsNew.dismissedSince',
    'kukatko.whatsNew.dismissedSince.u1',
    'kukatko.review.daily',
    'kukatko.review.daily.u1',
    'kukatko.review.daily.u2',
    'i18nextLng',
  ],
  session: [
    'kukatko.gridScroll',
    'kukatko.gridScroll.u1',
    'kukatko.gridScroll.u2',
    'kukatko.review.run',
    'kukatko.lastPickedLocation',
  ],
}

/** What survives by design: per-device display preferences and per-uid settings. */
const SURVIVORS = {
  local: [
    'kukatko.grid.density',
    'kukatko.review.density',
    'kukatko.slideshow.settings',
    'kukatko.viewer.chromeHintSeen',
    'kukatko.language.u1',
    'kukatko.pushPrompt.answered.u1',
    'kukatko.review.dailyish',
    'somebody-elses-key',
  ],
  session: ['kukatko.video.rate'],
}

describe('clearSignedOutState', () => {
  it('removes what the accounts saw, dismissed, played and scrolled, in both storages', () => {
    for (const key of [...ACCOUNT_STATE.local, ...SURVIVORS.local]) {
      window.localStorage.setItem(key, 'x')
    }
    for (const key of [...ACCOUNT_STATE.session, ...SURVIVORS.session]) {
      window.sessionStorage.setItem(key, 'x')
    }

    clearSignedOutState()

    for (const key of ACCOUNT_STATE.local) {
      expect(window.localStorage.getItem(key), key).toBeNull()
    }
    for (const key of ACCOUNT_STATE.session) {
      expect(window.sessionStorage.getItem(key), key).toBeNull()
    }
    for (const key of SURVIVORS.local) {
      expect(window.localStorage.getItem(key), key).toBe('x')
    }
    for (const key of SURVIVORS.session) {
      expect(window.sessionStorage.getItem(key), key).toBe('x')
    }
  })

  it('survives a storage that refuses to be read', () => {
    const hostile = {
      get length(): number {
        throw new Error('denied')
      },
    } as unknown as Storage
    window.sessionStorage.setItem('kukatko.gridScroll.u1', 'x')

    expect(() => {
      clearSignedOutState([hostile, window.sessionStorage])
    }).not.toThrow()
    expect(window.sessionStorage.getItem('kukatko.gridScroll.u1')).toBeNull()
  })
})
