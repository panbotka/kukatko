import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { AlbumTile } from '../components/organize/AlbumTile'
import { Layout } from '../components/Layout'
import type { AlbumCount } from '../services/organize'

import i18n from './index'

/** An admin auth context so role-gated nav links all render and get translated. */
function adminAuth(): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'admin', display_name: 'Admin', role: 'admin' },
    role: 'admin',
    downloadToken: null,
    canWrite: true,
    isAdmin: true,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** Builds an album-with-count fixture for the plural-rendering checks. */
function album(count: number): AlbumCount {
  return {
    uid: `al${count}`,
    slug: `album-${count}`,
    title: `Album ${count}`,
    description: '',
    type: 'album',
    private: false,
    order_by: 'added',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: count,
  }
}

/**
 * Renders a representative shell — the full navbar (every role-gated link) plus
 * album tiles exercising plural counts — under the given i18next instance.
 */
function renderScreens(instance: typeof i18n) {
  return render(
    <I18nextProvider i18n={instance}>
      <AuthContext.Provider value={adminAuth()}>
        <MemoryRouter initialEntries={['/']}>
          <Layout />
          <AlbumTile album={album(1)} />
          <AlbumTile album={album(3)} />
          <AlbumTile album={album(5)} />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

afterEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('representative screens render without missing translations', () => {
  for (const lng of ['cs', 'en'] as const) {
    it(`renders the navbar and album tiles in ${lng} with no missing keys`, async () => {
      // A clone with saveMissing surfaces any key i18next cannot resolve (raw
      // key, wrong namespace, or an absent plural form) via the handler.
      const missing: string[] = []
      const probe = i18n.cloneInstance({
        saveMissing: true,
        missingKeyHandler: (_lngs, _ns, key) => {
          missing.push(key)
        },
      })
      await probe.changeLanguage(lng)

      renderScreens(probe)

      expect(missing, `missing translation keys in ${lng}`).toEqual([])
    })
  }
})

describe('Czech pluralization', () => {
  it('uses the correct Czech plural form for 1, 3 and 5 photos', async () => {
    await i18n.changeLanguage('cs')
    renderScreens(i18n)
    expect(screen.getByText('1 fotka')).toBeInTheDocument()
    expect(screen.getByText('3 fotky')).toBeInTheDocument()
    expect(screen.getByText('5 fotek')).toBeInTheDocument()
  })

  it('uses English singular/plural for 1 vs 5 photos', async () => {
    await i18n.changeLanguage('en')
    renderScreens(i18n)
    expect(screen.getByText('1 photo')).toBeInTheDocument()
    expect(screen.getByText('5 photos')).toBeInTheDocument()
  })
})

describe('language switch updates all visible text', () => {
  it('swaps navbar labels from Czech to English and back', async () => {
    await i18n.changeLanguage('cs')
    const { rerender } = renderScreens(i18n)
    // The library destinations now live inside the "Procházet" (Browse) group,
    // whose always-visible dropdown toggle reflects the active language.
    expect(screen.getByRole('button', { name: 'Procházet' })).toBeInTheDocument()

    await i18n.changeLanguage('en')
    rerender(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={adminAuth()}>
          <MemoryRouter initialEntries={['/']}>
            <Layout />
            <AlbumTile album={album(1)} />
            <AlbumTile album={album(3)} />
            <AlbumTile album={album(5)} />
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>,
    )
    expect(screen.getByRole('button', { name: 'Browse' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Procházet' })).not.toBeInTheDocument()
  })
})
