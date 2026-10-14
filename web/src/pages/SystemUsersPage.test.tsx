import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import type { InstanceAccount } from '../services/projects'

import { SystemUsersPage } from './SystemUsersPage'

vi.mock('../services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/projects')>()
  return { ...actual, fetchInstanceAccounts: vi.fn(), setInstanceRole: vi.fn() }
})

vi.mock('../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/users')>()
  return {
    ...actual,
    createUser: vi.fn(),
    updateUser: vi.fn(),
    renameUser: vi.fn(),
    setUserDisabled: vi.fn(),
    resetUserPassword: vi.fn(),
    issuePasswordReset: vi.fn(),
  }
})

vi.mock('../services/settings', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/settings')>()
  return { ...actual, fetchPublicSettings: vi.fn() }
})

const { fetchInstanceAccounts, setInstanceRole, ProjectApiError } =
  await import('../services/projects')
const { createUser, updateUser, renameUser, setUserDisabled, resetUserPassword } =
  await import('../services/users')
const { fetchPublicSettings } = await import('../services/settings')
const fetchMock = vi.mocked(fetchInstanceAccounts)
const roleMock = vi.mocked(setInstanceRole)
const createMock = vi.mocked(createUser)
const updateMock = vi.mocked(updateUser)
const renameMock = vi.mocked(renameUser)
const disabledMock = vi.mocked(setUserDisabled)
const passwordMock = vi.mocked(resetUserPassword)

/** An account row, overridable. */
function account(overrides: Partial<InstanceAccount> = {}): InstanceAccount {
  return {
    uid: 'u2',
    username: 'jana',
    display_name: 'Jana',
    email: 'jana@example.com',
    instance_role: 'user',
    disabled: false,
    note: '',
    approved_at: '2026-01-01T00:00:00Z',
    last_login_at: null,
    memberships: [],
    ...overrides,
  }
}

const ME = account({
  uid: 'u1',
  username: 'admin',
  display_name: 'Admin',
  email: 'admin@example.com',
  instance_role: 'maintainer',
})

function renderPage() {
  const value = {
    status: 'authenticated',
    user: { uid: 'u1', username: 'admin', display_name: 'Admin' },
    isMaintainer: true,
  } as unknown as AuthContextValue
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/system/users']}>
          <SystemUsersPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The table row of the account with `username`. */
function rowOf(username: string): HTMLElement {
  const row = screen.getByText(username).closest('tr')
  if (row === null) {
    throw new Error(`no row for ${username}`)
  }
  return row
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  fetchMock.mockReset()
  roleMock.mockReset()
  vi.mocked(fetchPublicSettings).mockResolvedValue({ passkeys_enabled: false, mail_enabled: true })
  fetchMock.mockResolvedValue([
    ME,
    account({
      disabled: true,
      memberships: [
        {
          project_uid: 'pjv',
          project_slug: 'v',
          project_name: 'Veselice',
          active: true,
          role: 'curator',
          approved: true,
          subject_uid: 'sub_jana',
          subject_name: 'Jana Nováková',
        },
        {
          project_uid: 'pjr',
          project_slug: 'rodina',
          project_name: 'Rodina',
          active: true,
          role: 'viewer',
          approved: false,
          subject_uid: null,
          subject_name: '',
        },
      ],
    }),
  ])
})

describe('SystemUsersPage', () => {
  it('lists every account with its role, block and membership chips', async () => {
    renderPage()

    await screen.findByText('jana')
    const jana = within(rowOf('jana'))
    expect(jana.getByText('jana@example.com')).toBeInTheDocument()
    expect(jana.getByText('Uživatel')).toBeInTheDocument()
    expect(jana.getByText('Zablokovaný')).toBeInTheDocument()
    // The person is the membership's, shown per project and never edited here.
    expect(jana.getByText('v (Kurátor) · Jana Nováková')).toHaveAttribute(
      'title',
      expect.stringMatching(/V projektu Veselice je tento účet osoba Jana Nováková/),
    )
    expect(jana.getByText('rodina (Prohlížeč)')).toHaveAttribute('title', 'Čeká na schválení')

    const me = within(rowOf('admin'))
    expect(me.getByText('Správce systému')).toBeInTheDocument()
    expect(me.getByText('žádné')).toBeInTheDocument()
  })

  it('keeps one’s own row from changing its role, saying why', async () => {
    renderPage()
    await screen.findByText('jana')

    const own = within(rowOf('admin')).getByRole('button', { name: 'Odebrat správce' })
    expect(own).toHaveAttribute('aria-disabled', 'true')
    expect(own).toHaveAttribute('title', expect.stringMatching(/Vlastní systémovou roli/))
    await userEvent.click(own)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('promotes after the confirmation and redraws the row', async () => {
    const user = userEvent.setup()
    roleMock.mockResolvedValue(account({ instance_role: 'maintainer', disabled: true }))
    renderPage()
    await screen.findByText('jana')

    await user.click(
      within(rowOf('jana')).getByRole('button', { name: 'Povýšit na správce systému' }),
    )
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Povýšit Jana na správce systému?')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Povýšit na správce systému' }))

    await waitFor(() => {
      expect(roleMock).toHaveBeenCalledWith('u2', 'maintainer')
    })
    await waitFor(() => {
      expect(within(rowOf('jana')).getByText('Správce systému')).toBeInTheDocument()
    })
    expect(within(rowOf('jana')).getByRole('button', { name: 'Odebrat správce' })).toBeEnabled()
  })

  it('demotes after the confirmation and explains the last-maintainer refusal', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue([ME, account({ instance_role: 'maintainer' })])
    roleMock.mockRejectedValueOnce(new ProjectApiError(409, 'last_maintainer', ''))
    roleMock.mockResolvedValueOnce(account())
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odebrat správce' }))
    let dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Odebrat správce' }))
    expect(
      await screen.findByText('Instance musí mít aspoň jednoho správce systému.'),
    ).toBeInTheDocument()

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odebrat správce' }))
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Odebrat správce' }))

    await waitFor(() => {
      expect(roleMock).toHaveBeenLastCalledWith('u2', 'user')
    })
    await waitFor(() => {
      expect(within(rowOf('jana')).getByText('Uživatel')).toBeInTheDocument()
    })
  })

  it('shows a placeholder address as none and a waiting account as waiting', async () => {
    fetchMock.mockResolvedValue([ME, account({ email: 'jana@kukatko.invalid', approved_at: null })])
    renderPage()
    await screen.findByText('jana')

    const jana = within(rowOf('jana'))
    expect(jana.queryByText('jana@kukatko.invalid')).not.toBeInTheDocument()
    expect(jana.getByText('Bez adresy')).toBeInTheDocument()
    expect(jana.getByText('Čeká na schválení')).toBeInTheDocument()
  })

  it('creates an account with no membership and says where it joins a project', async () => {
    const user = userEvent.setup()
    createMock.mockResolvedValue({
      uid: 'u3',
      username: 'petr',
      display_name: 'Petr',
      email: 'petr@example.com',
      instance_role: 'user',
      disabled: false,
      note: '',
      created_at: '2026-10-14T00:00:00Z',
      updated_at: '2026-10-14T00:00:00Z',
    })
    renderPage()
    await screen.findByText('jana')

    await user.click(screen.getByRole('button', { name: 'Nový účet' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/nepatří do žádného projektu/)).toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Uživatelské jméno'), 'petr')
    await user.type(within(dialog).getByLabelText('Heslo'), 'correct horse')
    await user.type(within(dialog).getByLabelText('E-mail'), 'petr@example.com')
    await user.type(within(dialog).getByLabelText('Jméno'), 'Petr')
    await user.click(within(dialog).getByRole('button', { name: 'Vytvořit' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith({
        username: 'petr',
        password: 'correct horse',
        display_name: 'Petr',
        email: 'petr@example.com',
        instance_role: 'user',
        note: '',
      })
    })
    expect(await screen.findByText(/Účet Petr je vytvořený/)).toBeInTheDocument()
    // The list is read again so the new account appears with its (no) memberships.
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('edits the account record, renaming first, and touches no membership', async () => {
    const user = userEvent.setup()
    const saved = {
      uid: 'u2',
      username: 'janka',
      display_name: 'Jana',
      email: 'jana@new.example',
      instance_role: 'user' as const,
      disabled: true,
      note: '',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-10-14T00:00:00Z',
    }
    renameMock.mockResolvedValue(saved)
    updateMock.mockResolvedValue(saved)
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Upravit' }))
    const dialog = await screen.findByRole('dialog')
    // Editing offers no instance role (that is the confirmed promote/demote)
    // and no person (that is the membership's).
    expect(within(dialog).queryByLabelText('Systémová role')).not.toBeInTheDocument()
    const username = within(dialog).getByLabelText('Uživatelské jméno')
    await user.clear(username)
    await user.type(username, 'janka')
    const email = within(dialog).getByLabelText('E-mail')
    await user.clear(email)
    await user.type(email, 'jana@new.example')
    await user.click(within(dialog).getByRole('button', { name: 'Uložit' }))

    await waitFor(() => {
      // Only what the dialog changed: never the row's blocked flag or role.
      expect(updateMock).toHaveBeenCalledWith('u2', { email: 'jana@new.example' })
    })
    expect(renameMock).toHaveBeenCalledWith('u2', 'janka')
    expect(renameMock.mock.invocationCallOrder[0]).toBeLessThan(
      updateMock.mock.invocationCallOrder[0],
    )
    expect(await screen.findByText('Účet Jana je uložený.')).toBeInTheDocument()
  })

  it('sends a name-only edit of a blocked account as the name alone', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue({
      uid: 'u2',
      username: 'jana',
      display_name: 'Jana Nová',
      email: 'jana@example.com',
      instance_role: 'user' as const,
      disabled: true,
      note: '',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-10-14T00:00:00Z',
    })
    fetchMock.mockResolvedValue([ME, account({ disabled: true })])
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Upravit' }))
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByLabelText('Jméno')
    await user.clear(name)
    await user.type(name, 'Jana Nová')
    await user.click(within(dialog).getByRole('button', { name: 'Uložit' }))

    // A dialog opened before another administrator unblocked or promoted the
    // account must not send the row's stale flag or role back.
    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('u2', { display_name: 'Jana Nová' })
    })
    expect(renameMock).not.toHaveBeenCalled()
  })

  it('blocks and unblocks behind a confirmation, never one’s own account', async () => {
    const user = userEvent.setup()
    disabledMock.mockResolvedValue({} as Awaited<ReturnType<typeof setUserDisabled>>)
    fetchMock.mockResolvedValue([
      ME,
      account(),
      account({ uid: 'u3', username: 'petr', display_name: 'Petr', disabled: true }),
    ])
    renderPage()
    await screen.findByText('jana')

    const own = within(rowOf('admin')).getByRole('button', { name: 'Zablokovat' })
    expect(own).toHaveAttribute('aria-disabled', 'true')
    expect(own).toHaveAttribute('title', 'Vlastní účet zablokovat nemůžete.')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Zablokovat' }))
    let dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Zablokovat účet Jana?')).toBeInTheDocument()
    expect(disabledMock).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Zablokovat' }))
    await waitFor(() => {
      expect(disabledMock).toHaveBeenCalledWith(expect.objectContaining({ uid: 'u2' }), true)
    })
    expect(await screen.findByText('Účet Jana je zablokovaný.')).toBeInTheDocument()

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    await user.click(within(rowOf('petr')).getByRole('button', { name: 'Odblokovat' }))
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Odblokovat' }))
    await waitFor(() => {
      expect(disabledMock).toHaveBeenLastCalledWith(expect.objectContaining({ uid: 'u3' }), false)
    })
  })

  it('explains a block refused for the last maintainer', async () => {
    const user = userEvent.setup()
    disabledMock.mockRejectedValue(new ApiError(409, 'auth: cannot remove the last maintainer'))
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odblokovat' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Odblokovat' }))

    expect(await screen.findByText(/poslední aktivní správce systému/)).toBeInTheDocument()
  })

  it('sets another account’s password', async () => {
    const user = userEvent.setup()
    passwordMock.mockResolvedValue(undefined)
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Změnit heslo' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Nové heslo'), 'correct horse')
    await user.type(within(dialog).getByLabelText('Nové heslo znovu'), 'correct horse')
    await user.click(within(dialog).getByRole('button', { name: 'Změnit heslo' }))

    await waitFor(() => {
      expect(passwordMock).toHaveBeenCalledWith('u2', 'correct horse')
    })
    expect(await screen.findByText(/Heslo účtu Jana je změněné/)).toBeInTheDocument()
  })

  it('offers the password-reset link, worded for an instance without mail', async () => {
    const user = userEvent.setup()
    vi.mocked(fetchPublicSettings).mockResolvedValue({
      passkeys_enabled: false,
      mail_enabled: false,
    })
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odkaz na heslo' }))
    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText('Poslat odkaz na nastavení hesla uživateli jana?'),
    ).toBeInTheDocument()
    expect(within(dialog).getByText(/Mail není zapnutý/)).toBeInTheDocument()
  })
})
