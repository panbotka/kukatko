import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ToastContext, type ToastApi } from '../components/toast/ToastContext'
import i18n from '../i18n'
import type { PublicInvite } from '../services/invites'

import { InvitePage } from './InvitePage'

vi.mock('../services/invites', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/invites')>()
  return { ...actual, fetchPublicInvite: vi.fn(), acceptInvite: vi.fn() }
})

const { fetchPublicInvite, acceptInvite, InviteApiError } = await import('../services/invites')
const fetchMock = vi.mocked(fetchPublicInvite)
const acceptMock = vi.mocked(acceptInvite)

const toastShow = vi.fn<ToastApi['show']>()
const refresh = vi.fn<AuthContextValue['refresh']>()

/** What `GET /invite/<code>` answers, overridable. */
function invite(overrides: Partial<PublicInvite> = {}): PublicInvite {
  return {
    project_name: 'Veselice',
    role: 'curator',
    registration_role: 'curator',
    signed_in: false,
    ...overrides,
  }
}

/** Prints where the router is, path and query, for asserting on navigation. */
function Where() {
  const location = useLocation()
  return <div data-testid="where">{`${location.pathname}${location.search}`}</div>
}

function renderPage(code = 'AB3DEF7H') {
  const auth = { status: 'unauthenticated', refresh } as unknown as AuthContextValue
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <ToastContext.Provider value={{ show: toastShow }}>
          <MemoryRouter initialEntries={[`/invite/${code}`]}>
            <Routes>
              <Route path="/invite/:code" element={<InvitePage />} />
              <Route path="*" element={<Where />} />
            </Routes>
          </MemoryRouter>
        </ToastContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  fetchMock.mockReset()
  acceptMock.mockReset()
  toastShow.mockReset()
  refresh.mockReset()
  refresh.mockResolvedValue(undefined)
})

describe('InvitePage', () => {
  it('lets a signed-in account accept and lands in the project', async () => {
    fetchMock.mockResolvedValue(invite({ signed_in: true }))
    acceptMock.mockResolvedValue({ slug: 'v', role: 'curator', changed: true })
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByTestId('invite-lead')).toHaveTextContent(
      'Pozvánka do projektu Veselice jako kurátor',
    )
    expect(screen.getByText('Veselice').tagName).toBe('STRONG')
    expect(screen.queryByRole('link', { name: 'Zaregistrovat se' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Přijmout pozvánku' }))

    expect(acceptMock).toHaveBeenCalledWith('AB3DEF7H')
    expect(await screen.findByTestId('where')).toHaveTextContent('/p/v/')
    // The new membership has to be in the session before its project opens.
    expect(refresh).toHaveBeenCalled()
    expect(toastShow).toHaveBeenCalledWith({
      message: 'Vítejte v projektu Veselice.',
      variant: 'success',
    })
  })

  it('says so when accepting changed nothing', async () => {
    fetchMock.mockResolvedValue(invite({ signed_in: true }))
    acceptMock.mockResolvedValue({ slug: 'v', role: 'admin', changed: false })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Přijmout pozvánku' }))

    await waitFor(() => {
      expect(toastShow).toHaveBeenCalledWith({
        message: 'V projektu Veselice už jste.',
        variant: 'success',
      })
    })
  })

  it('offers registering or signing in to a visitor without a session', async () => {
    fetchMock.mockResolvedValue(invite({ role: 'admin', registration_role: 'editor' }))
    renderPage()

    const register = await screen.findByRole('link', { name: 'Zaregistrovat se' })
    expect(register).toHaveAttribute('href', '/register?invite=AB3DEF7H')
    const login = screen.getByRole('link', { name: 'Přihlásit se' })
    const target = new URL(login.getAttribute('href') ?? '', 'http://x')
    expect(target.pathname).toBe('/login')
    expect(target.searchParams.get('returnTo')).toBe('/invite/AB3DEF7H')
    expect(screen.queryByRole('button', { name: 'Přijmout pozvánku' })).not.toBeInTheDocument()
    // Registering grants less than accepting would: the page says which role.
    expect(screen.getByText(/dostanete roli editor\./)).toBeInTheDocument()
  })

  it('shows the generic not-found page for an unusable code', async () => {
    fetchMock.mockRejectedValue(new InviteApiError(404, 'not_found'))
    renderPage('NOPE2345')

    expect(await screen.findByTestId('not-found-page')).toBeInTheDocument()
    expect(screen.queryByTestId('invite-page')).not.toBeInTheDocument()
  })

  it('turns into the not-found page when the invite died before accepting', async () => {
    fetchMock.mockResolvedValue(invite({ signed_in: true }))
    acceptMock.mockRejectedValue(new InviteApiError(404, 'not_found'))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Přijmout pozvánku' }))

    expect(await screen.findByTestId('not-found-page')).toBeInTheDocument()
    expect(toastShow).not.toHaveBeenCalled()
  })
})
