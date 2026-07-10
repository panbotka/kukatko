import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type AdminUser } from '../services/users'

import { UsersPage } from './UsersPage'

vi.mock('../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/users')>()
  return {
    ...actual,
    fetchUsers: vi.fn(),
    createUser: vi.fn(),
    updateUser: vi.fn(),
    setUserDisabled: vi.fn(),
    resetUserPassword: vi.fn(),
  }
})

const { fetchUsers, createUser, setUserDisabled } = await import('../services/users')
const fetchUsersMock = vi.mocked(fetchUsers)
const createUserMock = vi.mocked(createUser)
const setUserDisabledMock = vi.mocked(setUserDisabled)

/** The signed-in administrator; their own row must not offer self-disabling. */
const ME = 'u-admin'

/** Builds an admin user row, defaulting to an enabled viewer. */
function user(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    uid: 'u1',
    username: 'ada',
    display_name: 'Ada Lovelace',
    email: '',
    role: 'viewer',
    disabled: false,
    note: '',
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    ...overrides,
  }
}

function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: ME, username: 'root', display_name: 'Root', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(isAdmin = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <MemoryRouter>
          <UsersPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchUsersMock.mockReset()
  fetchUsersMock.mockResolvedValue([])
  createUserMock.mockReset()
  setUserDisabledMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('UsersPage', () => {
  it('denies access to non-admins and never fetches the roster', () => {
    renderPage(false)

    expect(screen.getByText('This page is available to administrators only.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Users' })).not.toBeInTheDocument()
    expect(fetchUsersMock).not.toHaveBeenCalled()
  })

  it('renders the table from the fetched users', async () => {
    fetchUsersMock.mockResolvedValue([
      user({
        uid: 'u1',
        username: 'ada',
        display_name: 'Ada Lovelace',
        role: 'editor',
        note: 'On loan from the analytical engine',
        last_login_at: '2026-06-30T08:15:00Z',
      }),
      user({ uid: 'u2', username: 'bob', display_name: '', role: 'viewer', disabled: true }),
    ])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('Editor')).toBeInTheDocument()
    expect(screen.getByText('On loan from the analytical engine')).toBeInTheDocument()

    // The disabled account is flagged as such, and a user who never signed in
    // renders "Never" rather than an empty cell.
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(screen.getByText('Disabled')).toBeInTheDocument()
    expect(screen.getByText('Never')).toBeInTheDocument()
  })

  it('shows a retry button when the fetch fails, and reloads on click', async () => {
    fetchUsersMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    fetchUsersMock.mockResolvedValueOnce([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Failed to load the users.')).toBeInTheDocument()

    await actor.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('ada')).toBeInTheDocument()
  })

  it('renders an empty state rather than crashing on an empty roster', async () => {
    fetchUsersMock.mockResolvedValue([])
    renderPage()

    expect(await screen.findByText('No users')).toBeInTheDocument()
  })

  it('shows an API validation error inline next to the offending field', async () => {
    createUserMock.mockRejectedValue(new ApiError(409, 'username already taken'))
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    await actor.type(within(dialog).getByLabelText('Username'), 'ada')
    await actor.type(within(dialog).getByLabelText('Password'), 'correct-horse')
    await actor.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(createUserMock).toHaveBeenCalled()
    })

    // The message sits on the username input, not in a form-level alert.
    const username = within(dialog).getByLabelText('Username')
    expect(username).toHaveClass('is-invalid')
    expect(within(dialog).getByText('That username is already taken.')).toBeInTheDocument()
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('disables the disable control on the signed-in admin’s own row', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: ME, username: 'root', display_name: 'Root', role: 'admin' }),
      user({ uid: 'u1', username: 'ada' }),
    ])
    renderPage()

    // Wait for the real table: the loading skeleton is made of rows too.
    expect(await screen.findByText('root')).toBeInTheDocument()
    const rows = screen.getAllByRole('row')
    // rows[0] is the header; the roster is ordered as stubbed.
    const own = within(rows[1]).getByRole('button', { name: 'Disable' })
    const other = within(rows[2]).getByRole('button', { name: 'Disable' })

    expect(own).toBeDisabled()
    expect(within(rows[1]).getByText('You cannot disable your own account.')).toBeInTheDocument()
    expect(other).toBeEnabled()
  })

  it('disables another user only after the confirmation step', async () => {
    const ada = user({ uid: 'u1', username: 'ada' })
    fetchUsersMock.mockResolvedValue([ada])
    setUserDisabledMock.mockResolvedValue({ ...ada, disabled: true })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))

    // The click alone changes nothing: the dialog asks first.
    expect(setUserDisabledMock).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/signed out of every device/)).toBeInTheDocument()

    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))
    await waitFor(() => {
      expect(setUserDisabledMock).toHaveBeenCalledWith(ada, true)
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('renders the username read-only when editing an existing user', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Username')).toHaveAttribute('readonly')
    // Editing offers no password field; that is a separate dialog.
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
  })

  it('does not offer deleting a user', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
  })
})
