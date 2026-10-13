import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ApiError, NetworkError, type RegisteredAccount } from '../services/auth'

import { ProjectRegisterPage, RegisterPage } from './RegisterPage'

vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, register: vi.fn() }
})

vi.mock('../services/settings', () => ({
  fetchPublicSettings: vi.fn(),
  fetchProjectPublicSettings: vi.fn(),
}))

vi.mock('../services/invites', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/invites')>()
  return { ...actual, fetchPublicInvite: vi.fn() }
})

const { register } = await import('../services/auth')
const { fetchProjectPublicSettings, fetchPublicSettings } = await import('../services/settings')
const registerMock = vi.mocked(register)
const settingsMock = vi.mocked(fetchPublicSettings)
const projectMock = vi.mocked(fetchProjectPublicSettings)
const { fetchPublicInvite, InviteApiError } = await import('../services/invites')
const inviteMock = vi.mocked(fetchPublicInvite)

/** The account the backend echoes back after a successful registration. */
function created(overrides: Partial<RegisteredAccount> = {}): RegisteredAccount {
  return {
    username: 'newcomer',
    display_name: 'New Comer',
    email: 'newcomer@example.com',
    pending_approval: true,
    ...overrides,
  }
}

/** The registration page of the project every secret-path case registers into. */
const PROJECT_URL = '/p/rodina/register'

/**
 * Mounts both registration pages — the instance's `/register` and a project's
 * `/p/:slug/register` — with a sign-in route behind them, so their links resolve.
 */
function renderRegister(url = PROJECT_URL) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/p/:slug/register" element={<ProjectRegisterPage />} />
          <Route path="/login" element={<div>sign-in page</div>} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** Fills every field of the form; each value can be overridden per test. */
async function fillForm(
  user: ReturnType<typeof userEvent.setup>,
  overrides: Partial<Record<'password' | 'repeat' | 'secret' | 'username', string>> = {},
) {
  // The form appears only once the project has answered whether registration
  // is open, so the first field is awaited rather than assumed.
  await user.type(await screen.findByLabelText('Username'), overrides.username ?? '  newcomer  ')
  await user.type(screen.getByLabelText('Display name'), 'New Comer')
  await user.type(screen.getByLabelText('E-mail'), 'newcomer@example.com')
  await user.type(screen.getByLabelText('Password'), overrides.password ?? 'hunter2hunter2')
  await user.type(screen.getByLabelText('Password again'), overrides.repeat ?? 'hunter2hunter2')
  await user.type(screen.getByLabelText('Registration word'), overrides.secret ?? '  veselice  ')
}

/** The form group a field lives in, so a rejection can be located next to it. */
function groupOf(label: string): HTMLElement {
  const group = screen.getByLabelText(label).closest('.form-group, .mb-3')
  if (group === null) {
    throw new Error(`no form group around ${label}`)
  }
  return group as HTMLElement
}

describe('RegisterPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    settingsMock.mockResolvedValue({ passkeys_enabled: false, mail_enabled: true })
    projectMock.mockReset().mockResolvedValue({
      project_name: 'Rodina Nováků',
      registration_enabled: true,
    })
  })

  it('opens in the same frame as sign-in, wordmark over the card', () => {
    renderRegister()

    expect(screen.getByTestId('auth-wordmark')).toHaveTextContent('Kukátko')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Registration')
  })

  it("registers into the page's project and confirms it is waiting, signing nobody in", async () => {
    const user = userEvent.setup()
    registerMock.mockResolvedValue(created())
    renderRegister()

    await fillForm(user)
    expect(projectMock).toHaveBeenCalledWith('rodina', expect.any(AbortSignal))
    expect(screen.getByTestId('register-intro')).toHaveTextContent(
      'registering to the project Rodina Nováků',
    )
    await user.click(await screen.findByRole('button', { name: 'Register' }))

    expect(registerMock).toHaveBeenCalledWith({
      // Everything typed is trimmed, the secret included: a word copied out of a
      // message arrives with a space more often than not. The project the page
      // belongs to travels with it: its word is the one checked.
      username: 'newcomer',
      display_name: 'New Comer',
      email: 'newcomer@example.com',
      password: 'hunter2hunter2',
      secret: 'veselice',
      project: 'rodina',
    })

    const done = await screen.findByTestId('register-done')
    expect(done).toHaveTextContent(/The account newcomer has been created/i)
    expect(done).toHaveTextContent(/waiting for an administrator/i)
    expect(done).toHaveTextContent(/sent a confirmation to your e-mail/i)
    expect(within(done).getByRole('link', { name: 'Back to sign-in' })).toBeInTheDocument()

    // The form is gone and nobody is signed in: the account exists but is not
    // usable yet, so there is no session to hand out.
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
  })

  it('takes an upload link in place of the registration word', async () => {
    const user = userEvent.setup()
    registerMock.mockResolvedValue(created())
    renderRegister('/register?link=Ab3dEf7h')

    await user.type(await screen.findByLabelText('Username'), 'newcomer')
    await user.type(screen.getByLabelText('Display name'), 'New Comer')
    await user.type(screen.getByLabelText('E-mail'), 'newcomer@example.com')
    await user.type(screen.getByLabelText('Password'), 'hunter2hunter2')
    await user.type(screen.getByLabelText('Password again'), 'hunter2hunter2')
    // The link is the key: the word is not asked for at all.
    expect(screen.queryByLabelText('Registration word')).not.toBeInTheDocument()
    expect(screen.getByText(/registering through an upload link/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Register' }))

    expect(registerMock).toHaveBeenCalledWith({
      username: 'newcomer',
      display_name: 'New Comer',
      email: 'newcomer@example.com',
      password: 'hunter2hunter2',
      secret: '',
      upload_link: 'Ab3dEf7h',
    })
    expect(await screen.findByTestId('register-done')).toBeInTheDocument()
  })

  it('registers through an invitation: no word, the project named, the code sent', async () => {
    inviteMock.mockResolvedValue({
      project_name: 'Veselice',
      role: 'editor',
      registration_role: 'editor',
      signed_in: false,
    })
    registerMock.mockResolvedValue(created({ pending_approval: false }))
    const user = userEvent.setup()
    renderRegister('/register?invite=AB3DEF7H')

    await user.type(await screen.findByLabelText('Username'), 'newcomer')
    expect(inviteMock).toHaveBeenCalledWith('AB3DEF7H', expect.any(AbortSignal))
    expect(screen.getByTestId('register-intro')).toHaveTextContent(
      'invitation to the project Veselice',
    )
    expect(screen.queryByLabelText('Registration word')).not.toBeInTheDocument()
    expect(screen.queryByTestId('register-closed')).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Display name'), 'New Comer')
    await user.type(screen.getByLabelText('E-mail'), 'newcomer@example.com')
    await user.type(screen.getByLabelText('Password'), 'hunter2hunter2')
    await user.type(screen.getByLabelText('Password again'), 'hunter2hunter2')
    await user.click(screen.getByRole('button', { name: 'Register' }))

    expect(registerMock).toHaveBeenCalledWith({
      username: 'newcomer',
      display_name: 'New Comer',
      email: 'newcomer@example.com',
      password: 'hunter2hunter2',
      secret: '',
      invite_code: 'AB3DEF7H',
    })
    // The account is approved at once: no "waiting for an administrator".
    const done = await screen.findByTestId('register-done')
    expect(done).toHaveTextContent('approved already')
    expect(done).not.toHaveTextContent('waiting')
  })

  it('shows no form for an invitation that is not valid', async () => {
    inviteMock.mockRejectedValue(new InviteApiError(404, 'not_found'))
    renderRegister('/register?invite=NOPE2345')

    expect(await screen.findByTestId('register-invite-invalid')).toHaveTextContent(
      'invitation is not valid',
    )
    expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  })

  it('says a dead upload link cannot be registered through', async () => {
    const user = userEvent.setup()
    registerMock.mockRejectedValue(new ApiError(403, 'auth: the upload link is not valid'))
    renderRegister('/register?link=Ab3dEf7h')

    await user.type(await screen.findByLabelText('Username'), 'newcomer')
    await user.type(screen.getByLabelText('Display name'), 'New Comer')
    await user.type(screen.getByLabelText('E-mail'), 'newcomer@example.com')
    await user.type(screen.getByLabelText('Password'), 'hunter2hunter2')
    await user.type(screen.getByLabelText('Password again'), 'hunter2hunter2')
    await user.click(screen.getByRole('button', { name: 'Register' }))

    expect(await screen.findByText(/upload link is no longer valid/i)).toBeInTheDocument()
  })

  it('refuses to submit when the two passwords differ', async () => {
    const user = userEvent.setup()
    renderRegister()

    await fillForm(user, { password: 'hunter2hunter2', repeat: 'hunter2hunter3' })
    await user.click(await screen.findByRole('button', { name: 'Register' }))

    expect(registerMock).not.toHaveBeenCalled()
    expect(within(groupOf('Password again')).getByText(/passwords do not match/i)).toBeVisible()
    expect(screen.getByLabelText('Password again')).toBeInvalid()
  })

  it('says the registration word is wrong, not that the server failed', async () => {
    const user = userEvent.setup()
    registerMock.mockRejectedValue(new ApiError(403, 'auth: wrong registration secret'))
    renderRegister()

    await fillForm(user, { secret: 'nonsense' })
    await user.click(await screen.findByRole('button', { name: 'Register' }))

    const group = groupOf('Registration word')
    expect(await within(group).findByText(/not the right registration word/i)).toBeVisible()
    expect(screen.getByLabelText('Registration word')).toBeInvalid()
    // A wrong word is not an outage, and nothing generic may stand in for it.
    expect(screen.queryByText(/Registration failed/i)).not.toBeInTheDocument()
  })

  it('puts a taken username next to the username field', async () => {
    const user = userEvent.setup()
    registerMock.mockRejectedValue(new ApiError(409, 'username already taken'))
    renderRegister()

    await fillForm(user)
    await user.click(await screen.findByRole('button', { name: 'Register' }))

    const group = groupOf('Username')
    expect(await within(group).findByText(/already has that username/i)).toBeVisible()
    expect(screen.getByLabelText('Username')).toBeInvalid()
  })

  it('promises no e-mail on an instance that sends none', async () => {
    // With mail.enabled off the mailer is the no-op sender: the confirmation the
    // page used to promise would never arrive, and the hint under the address
    // said the same thing a second time.
    settingsMock.mockResolvedValue({ passkeys_enabled: false, mail_enabled: false })
    const user = userEvent.setup()
    registerMock.mockResolvedValue(created())
    renderRegister()

    expect(await screen.findByText(/sends no e-mail, so nothing will arrive/i)).toBeVisible()

    await fillForm(user)
    await user.click(screen.getByRole('button', { name: 'Register' }))

    const done = await screen.findByTestId('register-done')
    expect(done).toHaveTextContent(/waiting for an administrator/i)
    expect(done).toHaveTextContent(/No confirmation is coming/i)
    expect(done).not.toHaveTextContent(/sent a confirmation/i)
  })

  it('shows no form at all when the project says registration is closed', async () => {
    projectMock.mockResolvedValue({ project_name: 'Rodina Nováků', registration_enabled: false })
    renderRegister()

    const closed = await screen.findByTestId('register-closed')
    expect(closed).toHaveTextContent(/Registration to the project Rodina Nováků is closed/i)
    // Back to sign-in remembers the project, so the sign-in screen can offer
    // its registration again once it opens.
    expect(within(closed).getByRole('link', { name: 'Back to sign-in' })).toHaveAttribute(
      'href',
      '/login?project=rodina',
    )
    expect(screen.queryByRole('button', { name: 'Register' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Registration word')).not.toBeInTheDocument()
  })

  it('says so for a project that does not exist or is deactivated', async () => {
    projectMock.mockRejectedValue(new ApiError(404, 'project not found'))
    renderRegister('/p/nic/register')

    expect(await screen.findByTestId('register-project-missing')).toHaveTextContent(
      'does not exist or is no longer active',
    )
    expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  })

  it('still shows the form when the project could not be asked', async () => {
    // "We could not ask" is not "the door is shut": the server has the final
    // word, and it words a refusal better than a failed probe could.
    projectMock.mockRejectedValue(new NetworkError('Failed to fetch'))
    renderRegister()

    expect(await screen.findByRole('button', { name: 'Register' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByTestId('register-closed')).not.toBeInTheDocument()
    })
  })

  it('asks each project about its own registration', async () => {
    projectMock.mockImplementation((slug) =>
      Promise.resolve({
        project_name: slug === 'a' ? 'Projekt A' : 'Projekt B',
        registration_enabled: slug === 'a',
      }),
    )
    const first = renderRegister('/p/a/register')
    expect(await screen.findByTestId('register-intro')).toHaveTextContent('Projekt A')
    first.unmount()

    renderRegister('/p/b/register')
    expect(await screen.findByTestId('register-closed')).toHaveTextContent('Projekt B')
    expect(projectMock.mock.calls.map(([slug]) => slug)).toEqual(['a', 'b'])
  })

  it("forwards the instance's /register?project= to that project's page", async () => {
    renderRegister('/register?project=rodina')

    expect(await screen.findByTestId('register-intro')).toHaveTextContent('Rodina Nováků')
    expect(projectMock).toHaveBeenCalledWith('rodina', expect.any(AbortSignal))
  })

  it('explains that registration goes through a project when /register names none', () => {
    renderRegister('/register')

    const notice = screen.getByTestId('register-via-project')
    expect(notice).toHaveTextContent(/through an invitation or a project's link/i)
    expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
    expect(projectMock).not.toHaveBeenCalled()
  })
})
