import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ApiError, NetworkError, type RegisteredAccount } from '../services/auth'

import { RegisterPage } from './RegisterPage'

vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, register: vi.fn() }
})

vi.mock('../services/settings', () => ({ fetchPublicSettings: vi.fn() }))

const { register } = await import('../services/auth')
const { fetchPublicSettings } = await import('../services/settings')
const registerMock = vi.mocked(register)
const settingsMock = vi.mocked(fetchPublicSettings)

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

/** Mounts the page with a sign-in route behind it, so its link resolves. */
function renderRegister() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/register']}>
        <Routes>
          <Route path="/register" element={<RegisterPage />} />
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
  // The form appears only once the instance has answered whether registration
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
    settingsMock.mockResolvedValue({ registration_enabled: true, passkeys_enabled: false })
  })

  it('creates the account and confirms it is waiting, without signing anybody in', async () => {
    const user = userEvent.setup()
    registerMock.mockResolvedValue(created())
    renderRegister()

    await fillForm(user)
    await user.click(await screen.findByRole('button', { name: 'Register' }))

    expect(registerMock).toHaveBeenCalledWith({
      // Everything typed is trimmed, the secret included: a word copied out of a
      // message arrives with a space more often than not.
      username: 'newcomer',
      display_name: 'New Comer',
      email: 'newcomer@example.com',
      password: 'hunter2hunter2',
      secret: 'veselice',
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

  it('shows no form at all when the instance says registration is closed', async () => {
    settingsMock.mockResolvedValue({ registration_enabled: false, passkeys_enabled: false })
    renderRegister()

    const closed = await screen.findByTestId('register-closed')
    expect(closed).toHaveTextContent(/Registration is closed/i)
    expect(within(closed).getByRole('link', { name: 'Back to sign-in' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Register' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Registration word')).not.toBeInTheDocument()
  })

  it('still shows the form when the instance could not be asked', async () => {
    // "We could not ask" is not "the door is shut": the server has the final
    // word, and it words a refusal better than a failed probe could.
    settingsMock.mockRejectedValue(new NetworkError('Failed to fetch'))
    renderRegister()

    expect(await screen.findByRole('button', { name: 'Register' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByTestId('register-closed')).not.toBeInTheDocument()
    })
  })
})
