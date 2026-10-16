import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import {
  MemoryRouter,
  Route,
  Routes,
  createRoutesFromChildren,
  useLocation,
} from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { RequireImport, RequireRole } from '../auth/ProtectedRoute'
import i18n from '../i18n'
import {
  ApiError,
  type AuthSession,
  type InstanceRole,
  NetworkError,
  type ProjectMembership,
} from '../services/auth'
import { PasskeyError } from '../services/passkeys'

import { LoginPage } from './LoginPage'

vi.mock('../services/settings', () => ({
  fetchPublicSettings: vi.fn(),
  fetchProjectPublicSettings: vi.fn(),
}))

// Only the feature probe is faked: jsdom has no WebAuthn at all, and without a
// stand-in the passkey button could never render in a test. The ceremony itself
// runs behind `loginWithPasskey` on the auth context, which every test here
// controls directly.
vi.mock('../services/passkeys', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/passkeys')>()
  return { ...actual, isPasskeySupported: vi.fn() }
})

const { fetchProjectPublicSettings, fetchPublicSettings } = await import('../services/settings')
const settingsMock = vi.mocked(fetchPublicSettings)
const projectSettingsMock = vi.mocked(fetchProjectPublicSettings)
const { isPasskeySupported } = await import('../services/passkeys')
const supportedMock = vi.mocked(isPasskeySupported)

/**
 * A stand-in for the app's guarded route table, shaped like the real one: the
 * front door, a project's pages under `/p/:slug` with their role gates, the
 * maintainer-only operations and the legacy catch-all. The login page reads the
 * gates off the `RequireRole`/`RequireImport` elements, so these are the real
 * guards; the wiring of the real table is App.test's business.
 */
const GUARDED_ROUTES = createRoutesFromChildren(
  <>
    <Route path="/" element={<div />} />
    <Route path="/share-target" element={<div />} />
    <Route path="/p/:slug" element={<div />}>
      <Route path="photos/:uid" element={<div />} />
      <Route element={<div />}>
        <Route index element={<div />} />
        <Route path="albums" element={<div />} />
        <Route element={<RequireImport />}>
          <Route path="import" element={<div />} />
        </Route>
        <Route element={<RequireRole role="admin" />}>
          <Route path="members" element={<div />} />
        </Route>
        <Route path="*" element={<div />} />
      </Route>
    </Route>
    <Route element={<div />}>
      <Route path="/account" element={<div />} />
      <Route element={<RequireRole role="maintainer" />}>
        <Route path="/system" element={<div />} />
        <Route path="/system/projects" element={<div />} />
      </Route>
      <Route path="*" element={<div />} />
    </Route>
  </>,
)

/** A membership of the project `slug`, approved and active unless overridden. */
function membership(slug: string, overrides: Partial<ProjectMembership> = {}): ProjectMembership {
  return {
    uid: `pr-${slug}`,
    slug,
    name: slug,
    active: true,
    role: 'admin',
    approved: true,
    ...overrides,
  }
}

/**
 * What a sign-in resolves to: the session `GET /auth/me` returned. By default an
 * ordinary account that is admin of the one project `v`.
 */
function session(
  projects: ProjectMembership[] = [membership('v')],
  instanceRole: InstanceRole = 'user',
): AuthSession {
  return {
    user: {
      uid: 'us-alice',
      username: 'alice',
      display_name: 'Alice',
      email: 'alice@example.test',
      disabled: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    download_token: 'dl',
    instance_role: instanceRole,
    projects,
    last_project_slug: '',
  }
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    status: 'unauthenticated',
    user: null,
    role: null,
    instanceRole: null,
    projects: [],
    lastProjectSlug: null,
    downloadToken: null,
    canCurate: false,
    canWrite: false,
    isAdmin: false,
    isMaintainer: false,
    canImport: false,
    pictureVersion: 0,
    pictureChanged: vi.fn(),
    login: vi.fn(),
    loginWithPasskey: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    ...overrides,
  }
}

/** Surfaces where login sent the user, query string and hash included. */
function LocationProbe() {
  const { pathname, search, hash } = useLocation()
  return <span data-testid="location">{`${pathname}${search}${hash}`}</span>
}

/**
 * Mounts the login page. `state` is what the route guard stashes when it bounces
 * a visitor here — the location they actually asked for; `search` is the login
 * page's own query (an invitation page's `?returnTo=`).
 */
function renderLogin(
  value: AuthContextValue,
  state?: { from: { pathname: string; search: string; hash?: string } },
  search = '',
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[{ pathname: '/login', search, state }]}>
          <Routes>
            <Route path="/login" element={<LoginPage routes={GUARDED_ROUTES} />} />
            <Route path="/" element={<div>home page</div>} />
            <Route path="/system/*" element={<div>system page</div>} />
            <Route path="/p/:slug/*" element={<div>project page</div>} />
            <Route path="/share-target" element={<div>share page</div>} />
            <Route path="/invite/:code" element={<div>invite page</div>} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

describe('LoginPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    settingsMock.mockResolvedValue({
      passkeys_enabled: false,
      mail_enabled: true,
    })
    // A browser that has WebAuthn is the interesting default; the instance flag
    // above still keeps the button off unless a test turns it on.
    supportedMock.mockReturnValue(true)
    // Most of these tests are about signing in, not about who may join: a
    // closed project is the quieter default and keeps the card unchanged.
    projectSettingsMock.mockReset().mockResolvedValue({
      project_name: 'Rodina',
      registration_enabled: false,
    })
  })

  it('opens in the shared frame: the Kukátko wordmark over a card titled Sign in', () => {
    renderLogin(authValue())

    expect(screen.getByTestId('auth-wordmark')).toHaveTextContent('Kukátko')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Sign in')
  })

  it('does not call login when fields are empty (client-side validation)', async () => {
    const user = userEvent.setup()
    const value = authValue()
    renderLogin(value)

    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(value.login).not.toHaveBeenCalled()
  })

  it('submits trimmed credentials when fields are filled', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(session())
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), '  alice  ')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(login).toHaveBeenCalledWith('alice', 'secret')
  })

  it('renders the invalid-credentials message on a 401', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new ApiError(401, 'invalid username or password'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid username or password.')
  })

  it('returns to the requested address with its query string intact', async () => {
    // The query is not decoration: a share from the phone names the files
    // waiting in the cache with it, so dropping it loses the photos.
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(session())
    renderLogin(authValue({ login }), {
      from: { pathname: '/share-target', search: '?share=1700000000000-1' },
    })

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('share page')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=1700000000000-1')
  })

  it('sends an already-signed-in visitor onward, query string and all', async () => {
    renderLogin(authValue({ status: 'authenticated' }), {
      from: { pathname: '/share-target', search: '?share=abc' },
    })

    expect(await screen.findByText('share page')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=abc')
  })

  it('returns to a ?returnTo= path, ahead of the stashed address', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(session())
    renderLogin(
      authValue({ login }),
      { from: { pathname: '/share-target', search: '' } },
      '?returnTo=%2Finvite%2FAB3DEF7H',
    )

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('invite page')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/invite/AB3DEF7H')
  })

  it('ignores a ?returnTo= that names another host', async () => {
    renderLogin(authValue({ status: 'authenticated' }), undefined, '?returnTo=//evil.example/x')

    expect(await screen.findByText('home page')).toBeInTheDocument()
  })

  it('falls back to the library when the guard stashed nothing', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(session())
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('home page')).toBeInTheDocument()
  })

  it('says the account is waiting for approval on a 403', async () => {
    // The password was right — this is somebody who registered and has not been
    // let in yet. Blaming their credentials would send them off retyping a
    // password that works.
    const user = userEvent.setup()
    const login = vi
      .fn()
      .mockRejectedValue(new ApiError(403, 'auth: account is waiting for approval'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'newcomer')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/waiting for an administrator/i)
    expect(alert).toHaveTextContent(/will get an e-mail once it is approved/i)
  })

  it('promises no approval e-mail on an instance that sends none', async () => {
    // Same 403, same fact — but with mail.enabled off nobody is ever written to,
    // so the sentence says the account will simply be let in.
    settingsMock.mockResolvedValue({
      passkeys_enabled: false,
      mail_enabled: false,
    })
    const user = userEvent.setup()
    const login = vi
      .fn()
      .mockRejectedValue(new ApiError(403, 'auth: account is waiting for approval'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'newcomer')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/waiting for an administrator/i)
    expect(alert).toHaveTextContent(/no e-mail is coming/i)
  })

  it('renders the rate-limited message on a 429', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new ApiError(429, 'too many'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/too many login attempts/i)
  })

  it('blames the connection, not the password, when the request never left the device', async () => {
    // A phone with no signal: fetch rejects, nothing judged the credentials. The
    // generic "sign in failed, please try again" used to land here and send the
    // reader off to retype a password they had not forgotten.
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new NetworkError('Failed to fetch'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/could not reach the server/i)
    expect(alert).toHaveTextContent(/your password is probably fine/i)
    // Emphatically not the credentials message, and not the generic one.
    expect(alert).not.toHaveTextContent(/invalid username or password/i)
    expect(alert).not.toHaveTextContent(/sign in failed/i)
  })

  it('warns up front when the session probe already found the backend unreachable', async () => {
    renderLogin(authValue({ status: 'unreachable' }))

    expect(await screen.findByTestId('login-offline-notice')).toHaveTextContent(
      /cannot reach the server right now/i,
    )
  })

  it('keeps its hands off the keyboard while the backend is unreachable', () => {
    // autoFocus raises the phone's keyboard. Doing that for a form that cannot
    // succeed is an invitation to type a password nothing will check.
    const { unmount } = renderLogin(authValue({ status: 'unreachable' }))
    expect(screen.getByLabelText('Username')).not.toHaveFocus()
    unmount()

    renderLogin(authValue())
    expect(screen.getByLabelText('Username')).toHaveFocus()
  })

  it('replaces the standing warning with the message for the attempt just made', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new NetworkError('Failed to fetch'))
    renderLogin(authValue({ status: 'unreachable', login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not reach the server/i)
    expect(screen.queryByTestId('login-offline-notice')).not.toBeInTheDocument()
  })

  it('invites registration into the project named by ?project= when it is open', async () => {
    projectSettingsMock.mockResolvedValue({ project_name: 'Rodina', registration_enabled: true })
    renderLogin(authValue(), undefined, '?project=rodina')

    const link = await screen.findByRole('link', { name: 'Register' })
    expect(link).toHaveAttribute('href', '/p/rodina/register')
    expect(projectSettingsMock).toHaveBeenCalledWith('rodina', expect.any(AbortSignal))
  })

  it('invites registration into the project whose page the guard bounced the reader off', async () => {
    projectSettingsMock.mockResolvedValue({ project_name: 'Rodina', registration_enabled: true })
    renderLogin(authValue(), { from: { pathname: '/p/rodina/albums', search: '' } })

    const link = await screen.findByRole('link', { name: 'Register' })
    expect(link).toHaveAttribute('href', '/p/rodina/register')
  })

  it('offers no registration when the visit is about no project', async () => {
    // Registration always goes through a project; there is no instance-wide door.
    renderLogin(authValue())

    await waitFor(() => {
      expect(settingsMock).toHaveBeenCalled()
    })
    expect(projectSettingsMock).not.toHaveBeenCalled()
    expect(screen.queryByTestId('login-register-link')).not.toBeInTheDocument()
  })

  it("says nothing about registration when the project's is closed", async () => {
    // A link to a form that answers every submit with "not open" is worse than
    // no link: the reader fills the whole thing in before finding out.
    renderLogin(authValue(), undefined, '?project=rodina')

    await waitFor(() => {
      expect(projectSettingsMock).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('login-register-link')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Register' })).not.toBeInTheDocument()
  })

  it('says nothing about registration when the project could not be asked', async () => {
    projectSettingsMock.mockRejectedValue(new NetworkError('Failed to fetch'))
    renderLogin(authValue(), undefined, '?project=rodina')

    await waitFor(() => {
      expect(projectSettingsMock).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('login-register-link')).not.toBeInTheDocument()
  })

  it('offers the passkey button only on an instance that has passkeys', async () => {
    renderLogin(authValue())

    await waitFor(() => {
      expect(settingsMock).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('login-passkey-button')).not.toBeInTheDocument()
  })

  it('hides the passkey button in a browser without WebAuthn', async () => {
    // A button that answers every press with "this browser cannot" is worse than
    // no button: the password form below it works everywhere.
    settingsMock.mockResolvedValue({
      passkeys_enabled: true,
      mail_enabled: true,
    })
    supportedMock.mockReturnValue(false)
    renderLogin(authValue())

    await waitFor(() => {
      expect(settingsMock).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('login-passkey-button')).not.toBeInTheDocument()
  })

  it('signs in with a passkey and returns to the requested address', async () => {
    settingsMock.mockResolvedValue({
      passkeys_enabled: true,
      mail_enabled: true,
    })
    const user = userEvent.setup()
    const loginWithPasskey = vi.fn().mockResolvedValue(session())
    renderLogin(authValue({ loginWithPasskey }), {
      from: { pathname: '/share-target', search: '?share=abc' },
    })

    await user.click(await screen.findByRole('button', { name: 'Sign in with a passkey' }))

    expect(loginWithPasskey).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('share page')).toBeInTheDocument()
    // Nothing was typed: a discoverable sign-in names no account.
    expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  })

  it('says a cancelled prompt in plain words, never the exception', async () => {
    settingsMock.mockResolvedValue({
      passkeys_enabled: true,
      mail_enabled: true,
    })
    const user = userEvent.setup()
    const loginWithPasskey = vi
      .fn()
      .mockRejectedValue(
        new PasskeyError('cancelled', 'The operation either timed out or was not allowed.'),
      )
    renderLogin(authValue({ loginWithPasskey }))

    await user.click(await screen.findByRole('button', { name: 'Sign in with a passkey' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/was not completed/i)
    expect(alert).toHaveTextContent(/sign in with your password/i)
    expect(alert).not.toHaveTextContent(/timed out or was not allowed/i)
    // The password form is still there to fall back on.
    expect(screen.getByLabelText('Username')).toBeInTheDocument()
  })

  it("borrows the password form's own sentence for an account awaiting approval", async () => {
    // The signature was good — this is the same fact about the account, so it is
    // the same sentence, not an authenticator-flavoured retelling of it.
    settingsMock.mockResolvedValue({
      passkeys_enabled: true,
      mail_enabled: true,
    })
    const user = userEvent.setup()
    const loginWithPasskey = vi.fn().mockRejectedValue(new PasskeyError('pendingApproval'))
    renderLogin(authValue({ loginWithPasskey }))

    await user.click(await screen.findByRole('button', { name: 'Sign in with a passkey' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/waiting for an administrator/i)
  })
  describe('only returns the account to a page it may use', () => {
    /** Signs in through the password form as the account `signedIn` names. */
    async function signInAs(
      signedIn: AuthSession,
      state?: { from: { pathname: string; search: string; hash?: string } },
      search = '',
    ) {
      const user = userEvent.setup()
      const login = vi.fn().mockResolvedValue(signedIn)
      renderLogin(authValue({ login }), state, search)
      await user.type(screen.getByLabelText('Username'), 'alice')
      await user.type(screen.getByLabelText('Password'), 'secret')
      await user.click(screen.getByRole('button', { name: 'Sign in' }))
    }

    it("sends an ordinary account off a system administrator's page to its home", async () => {
      // Signed out as the system administrator on /system/projects, signed in as
      // a project admin: the stashed page would only say "no access".
      await signInAs(session([membership('v', { role: 'admin' })]), {
        from: { pathname: '/system/projects', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
      expect(screen.getByTestId('location')).toHaveTextContent(/^\/$/)
      expect(screen.queryByText('system page')).not.toBeInTheDocument()
    })

    it('returns a system administrator to the /system page it was on', async () => {
      await signInAs(session([], 'maintainer'), {
        from: { pathname: '/system/projects', search: '?q=rod' },
      })

      expect(await screen.findByText('system page')).toBeInTheDocument()
      expect(screen.getByTestId('location')).toHaveTextContent('/system/projects?q=rod')
    })

    it('sends an account away from a project it is not in, exactly as from one that does not exist', async () => {
      await signInAs(session([membership('v')]), {
        from: { pathname: '/p/rodina/albums', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
      expect(screen.queryByText('project page')).not.toBeInTheDocument()
    })

    it('returns the same person to their own deep link, query and hash included', async () => {
      await signInAs(session([membership('v', { role: 'viewer' })]), {
        from: { pathname: '/p/v/photos/ph123', search: '?panel=info', hash: '#c-7' },
      })

      expect(await screen.findByText('project page')).toBeInTheDocument()
      expect(screen.getByTestId('location')).toHaveTextContent('/p/v/photos/ph123?panel=info#c-7')
    })

    it('falls back when the project page needs a higher role than the account holds', async () => {
      await signInAs(session([membership('v', { role: 'viewer' })]), {
        from: { pathname: '/p/v/members', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
    })

    it('falls back from a maintainer-only project page for a project admin', async () => {
      await signInAs(session([membership('v', { role: 'admin' })]), {
        from: { pathname: '/p/v/import', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
    })

    it('falls back from a project whose membership still waits for approval', async () => {
      await signInAs(session([membership('v', { approved: false })]), {
        from: { pathname: '/p/v/albums', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
    })

    it('keeps an invitation ?returnTo= for an account that belongs to no project yet', async () => {
      await signInAs(
        session([]),
        { from: { pathname: '/system/projects', search: '' } },
        '?returnTo=%2Finvite%2FAB3DEF7H',
      )

      expect(await screen.findByText('invite page')).toBeInTheDocument()
      expect(screen.getByTestId('location')).toHaveTextContent('/invite/AB3DEF7H')
    })

    it('judges a passkey sign-in by the account it signed in', async () => {
      settingsMock.mockResolvedValue({ passkeys_enabled: true, mail_enabled: true })
      const user = userEvent.setup()
      const loginWithPasskey = vi.fn().mockResolvedValue(session([membership('v')]))
      renderLogin(authValue({ loginWithPasskey }), {
        from: { pathname: '/p/rodina', search: '' },
      })

      await user.click(await screen.findByRole('button', { name: 'Sign in with a passkey' }))

      expect(await screen.findByText('home page')).toBeInTheDocument()
    })

    it('sends an already-signed-in visitor home from a page it may not use', async () => {
      renderLogin(authValue({ status: 'authenticated', instanceRole: 'user', projects: [] }), {
        from: { pathname: '/system', search: '' },
      })

      expect(await screen.findByText('home page')).toBeInTheDocument()
    })
  })
})
