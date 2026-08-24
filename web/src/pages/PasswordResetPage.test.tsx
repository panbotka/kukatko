import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ApiError, NetworkError } from '../services/auth'

import { PasswordResetPage } from './PasswordResetPage'

vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, fetchPasswordResetStatus: vi.fn(), resetPassword: vi.fn() }
})

const { fetchPasswordResetStatus, resetPassword } = await import('../services/auth')
const statusMock = vi.mocked(fetchPasswordResetStatus)
const resetMock = vi.mocked(resetPassword)

/** The token in the address the page is opened at. */
const TOKEN = 'reset-token-abc'

/** Mounts the page at its route, with a sign-in route behind it for the link. */
function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[`/password-reset/${TOKEN}`]}>
        <Routes>
          <Route path="/password-reset/:token" element={<PasswordResetPage />} />
          <Route path="/login" element={<div>sign-in page</div>} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** Types both passwords; each value can be overridden per test. */
async function fillForm(
  user: ReturnType<typeof userEvent.setup>,
  overrides: Partial<Record<'password' | 'repeat', string>> = {},
) {
  // The form appears only once the link has been checked, so the first field is
  // awaited rather than assumed.
  await user.type(
    await screen.findByLabelText('New password'),
    overrides.password ?? 'hunter2hunter2',
  )
  await user.type(screen.getByLabelText('Password again'), overrides.repeat ?? 'hunter2hunter2')
}

/** The form group a field lives in, so a rejection can be located next to it. */
function groupOf(label: string): HTMLElement {
  const group = screen.getByLabelText(label).closest('.form-group, .mb-3')
  if (group === null) {
    throw new Error(`no form group around ${label}`)
  }
  return group as HTMLElement
}

describe('PasswordResetPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    statusMock.mockResolvedValue({ valid: true, display_name: 'Anna Nováková' })
  })

  it('checks the link before it shows anything to fill in', () => {
    // A promise that never settles: the page is caught mid-check, which is where
    // an empty form would be the wrong thing to show.
    statusMock.mockReturnValue(new Promise(() => undefined))
    renderPage()

    expect(screen.getByTestId('password-reset-checking')).toBeInTheDocument()
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument()
    expect(statusMock).toHaveBeenCalledWith(TOKEN, expect.anything())
  })

  it('greets the person by name and offers the two password fields', async () => {
    renderPage()

    expect(await screen.findByText(/Hello, Anna Nováková/)).toBeInTheDocument()
    expect(screen.getByLabelText('New password')).toBeInTheDocument()
    expect(screen.getByLabelText('Password again')).toBeInTheDocument()
    // The rule the account screen states is stated here too, so nobody learns
    // the minimum only by being refused.
    expect(screen.getByText(/at least 8 characters long/i)).toBeInTheDocument()
    expect(screen.queryByTestId('password-reset-invalid')).not.toBeInTheDocument()
  })

  it('sets the new password and says the other sessions are gone, signing nobody in', async () => {
    const user = userEvent.setup()
    resetMock.mockResolvedValue()
    renderPage()

    await fillForm(user)
    await user.click(screen.getByRole('button', { name: 'Set the new password' }))

    expect(resetMock).toHaveBeenCalledWith(TOKEN, 'hunter2hunter2')

    const done = await screen.findByTestId('password-reset-done')
    expect(done).toHaveTextContent(/Your password has been changed/i)
    expect(done).toHaveTextContent(/Every other sign-in to this account has been ended/i)
    expect(within(done).getByRole('link', { name: 'Go to sign-in' })).toBeInTheDocument()

    // The form is gone and nobody was signed in: the call ended every session of
    // the account, so the next step is to sign in, not to browse.
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument()
    expect(screen.queryByText('sign-in page')).not.toBeInTheDocument()
  })

  it('refuses to submit when the two passwords differ', async () => {
    const user = userEvent.setup()
    renderPage()

    await fillForm(user, { password: 'hunter2hunter2', repeat: 'hunter2hunter3' })
    await user.click(screen.getByRole('button', { name: 'Set the new password' }))

    expect(resetMock).not.toHaveBeenCalled()
    expect(within(groupOf('Password again')).getByText(/passwords do not match/i)).toBeVisible()
    expect(screen.getByLabelText('Password again')).toBeInvalid()
  })

  it('explains a dead link instead of showing a form, and reveals nothing about the account', async () => {
    // The backend answers unknown, spent, expired and blocked links alike, so
    // the page has exactly one thing it may say about all four.
    statusMock.mockResolvedValue({ valid: false })
    renderPage()

    const invalid = await screen.findByTestId('password-reset-invalid')
    expect(invalid).toHaveTextContent(/This link no longer works/i)
    expect(invalid).toHaveTextContent(/expired, or somebody has already used it/i)
    expect(within(invalid).queryByText(/account/i)).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go to sign-in' })).toBeInTheDocument()
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Set the new password' })).not.toBeInTheDocument()
  })

  it('falls back to the dead-link explanation when the link dies before the form is sent', async () => {
    const user = userEvent.setup()
    // Usable when the page opened, gone by the time it was submitted: the answer
    // is the link's, not the password's, so nothing is said about what was typed.
    resetMock.mockRejectedValue(new ApiError(404, 'auth: the password-reset link is not valid'))
    renderPage()

    await fillForm(user)
    await user.click(screen.getByRole('button', { name: 'Set the new password' }))

    const invalid = await screen.findByTestId('password-reset-invalid')
    expect(invalid).toHaveTextContent(/This link no longer works/i)
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument()
    expect(screen.queryByTestId('password-reset-done')).not.toBeInTheDocument()
  })

  it('puts a password the server refuses as too short under that field', async () => {
    const user = userEvent.setup()
    resetMock.mockRejectedValue(new ApiError(400, 'auth: password must be at least 8 characters'))
    renderPage()

    await fillForm(user)
    await user.click(screen.getByRole('button', { name: 'Set the new password' }))

    const group = groupOf('New password')
    expect(await within(group).findByText(/too short/i)).toBeVisible()
    expect(screen.queryByTestId('password-reset-invalid')).not.toBeInTheDocument()
  })

  it('says the server could not be reached rather than claiming the link is dead', async () => {
    // "We could not ask" is not "the link is gone": a dead-link message here
    // would send somebody off asking for a replacement they do not need.
    statusMock.mockRejectedValue(new NetworkError('Failed to fetch'))
    renderPage()

    const unreachable = await screen.findByTestId('password-reset-unreachable')
    expect(unreachable).toHaveTextContent(/could not reach the server/i)
    expect(screen.queryByTestId('password-reset-invalid')).not.toBeInTheDocument()

    // The retry asks again, and a link that is fine gets its form after all.
    const user = userEvent.setup()
    statusMock.mockResolvedValue({ valid: true, display_name: 'Anna Nováková' })
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByLabelText('New password')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByTestId('password-reset-unreachable')).not.toBeInTheDocument()
    })
  })
})
