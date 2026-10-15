import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ToastContext, type ToastApi } from '../components/toast/ToastContext'
import i18n from '../i18n'
import type { ProjectMemberRow } from '../services/members'

import { MembersPage } from './MembersPage'

vi.mock('../services/members', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/members')>()
  return {
    ...actual,
    fetchMembers: vi.fn(),
    setMemberRole: vi.fn(),
    approveMember: vi.fn(),
    removeMember: vi.fn(),
    addMember: vi.fn(),
    withdrawMember: vi.fn(),
  }
})

// The invitations section below the list fetches on its own; its behaviour is
// covered by InvitesSection.test.tsx.
vi.mock('../services/invites', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/invites')>()
  return {
    ...actual,
    fetchInvites: vi.fn().mockResolvedValue({ invites: [], default_days: 7, max_days: 90 }),
  }
})

const {
  fetchMembers,
  setMemberRole,
  approveMember,
  removeMember,
  addMember,
  withdrawMember,
  MemberApiError,
} = await import('../services/members')
const fetchMock = vi.mocked(fetchMembers)
const roleMock = vi.mocked(setMemberRole)
const approveMock = vi.mocked(approveMember)
const removeMock = vi.mocked(removeMember)
const addMock = vi.mocked(addMember)
const withdrawMock = vi.mocked(withdrawMember)

/** A member row, overridable. */
function member(overrides: Partial<ProjectMemberRow> = {}): ProjectMemberRow {
  return {
    user_uid: 'u2',
    username: 'jana',
    display_name: 'Jana Nováková',
    role: 'curator',
    approved_at: '2026-10-01T10:00:00Z',
    last_seen_at: '2026-10-12T08:30:00Z',
    has_picture: false,
    maintainer: false,
    ...overrides,
  }
}

const ME = member({ user_uid: 'u1', username: 'spravce', display_name: 'Správce', role: 'admin' })
const WAITING = member({
  user_uid: 'u3',
  username: 'novak',
  display_name: '',
  role: 'viewer',
  approved_at: null,
  last_seen_at: null,
})

const toastShow = vi.fn<ToastApi['show']>()

function renderPage() {
  const value = {
    status: 'authenticated',
    user: { uid: 'u1', username: 'spravce', display_name: 'Správce', role: 'admin' },
    isAdmin: true,
  } as unknown as AuthContextValue
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <ToastContext.Provider value={{ show: toastShow }}>
          <MemoryRouter initialEntries={['/p/v/members']}>
            <MembersPage />
          </MemoryRouter>
        </ToastContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/**
 * The table row of the member with `username` — found by the username cell, since
 * a member without a display name shows the username as the name too.
 */
function rowOf(username: string): HTMLElement {
  const cell = screen
    .getAllByText(username)
    .find((element) => element.classList.contains('font-monospace'))
  const row = cell?.closest('tr') ?? null
  if (row === null) {
    throw new Error(`no row for ${username}`)
  }
  return row
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  for (const mock of [fetchMock, roleMock, approveMock, removeMock, addMock]) {
    mock.mockReset()
  }
  toastShow.mockReset()
  fetchMock.mockResolvedValue([ME, member(), WAITING])
})

describe('MembersPage', () => {
  it('lists the members with their roles and approval state', async () => {
    renderPage()

    await screen.findByText('jana')
    const jana = within(rowOf('jana'))
    expect(jana.getByText('Jana Nováková')).toBeInTheDocument()
    expect(jana.getByRole('combobox', { name: 'Role uživatele Jana Nováková' })).toHaveValue(
      'curator',
    )
    expect(jana.getByText('Schválený')).toBeInTheDocument()
    expect(jana.queryByRole('button', { name: 'Schválit' })).not.toBeInTheDocument()
    // The last visit to this project, never the account's last sign-in.
    expect(screen.getByRole('columnheader', { name: 'Naposledy v projektu' })).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: 'Poslední přihlášení' })).toBeNull()

    // A waiting membership: the badge and the button, named by the username
    // when the account has no display name, never here.
    const novak = within(rowOf('novak'))
    expect(novak.getByRole('combobox', { name: 'Role uživatele novak' })).toHaveValue('viewer')
    expect(novak.getByText('Čeká na schválení')).toBeInTheDocument()
    expect(novak.getByRole('button', { name: 'Schválit' })).toBeInTheDocument()
    expect(novak.getByText('nikdy')).toBeInTheDocument()
  })

  it('names a maintainer for what it is and offers no role change on its row', async () => {
    fetchMock.mockResolvedValue([
      ME,
      member({
        user_uid: 'u9',
        username: 'ops',
        display_name: 'Ops',
        role: 'viewer',
        maintainer: true,
      }),
    ])
    renderPage()

    await screen.findByText('ops')
    const ops = within(rowOf('ops'))
    expect(ops.queryByRole('combobox')).not.toBeInTheDocument()
    expect(ops.getByText('Správce systému')).toBeInTheDocument()
    expect(ops.queryByText('Prohlížeč')).not.toBeInTheDocument()
  })

  it('words the server refusing a maintainer role change', async () => {
    roleMock.mockRejectedValue(new MemberApiError(409, 'maintainer'))
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('jana')
    await user.selectOptions(
      within(rowOf('jana')).getByRole('combobox', { name: 'Role uživatele Jana Nováková' }),
      'viewer',
    )
    await waitFor(() => {
      expect(toastShow).toHaveBeenCalledWith({
        message: i18n.t('members.maintainerRoleReason'),
        variant: 'danger',
      })
    })
  })

  it('keeps one’s own role and removal off, saying why', async () => {
    renderPage()
    await screen.findByText('jana')

    const me = within(rowOf('spravce'))
    const select = me.getByRole('combobox', { name: 'Role uživatele Správce' })
    expect(select).toBeDisabled()
    expect(select).toHaveAccessibleDescription(
      'Vlastní roli změnit nemůžete — musí to udělat jiný správce projektu.',
    )
    expect(select.parentElement).toHaveAttribute(
      'title',
      'Vlastní roli změnit nemůžete — musí to udělat jiný správce projektu.',
    )
    expect(me.getByRole('button', { name: 'Odebrat' })).toHaveAttribute('aria-disabled', 'true')

    // Everybody else's select is live.
    expect(
      within(rowOf('jana')).getByRole('combobox', { name: 'Role uživatele Jana Nováková' }),
    ).toBeEnabled()
  })

  it('changes a role at once and confirms it', async () => {
    const user = userEvent.setup()
    roleMock.mockResolvedValue(member({ role: 'editor' }))
    renderPage()
    await screen.findByText('jana')

    await user.selectOptions(
      within(rowOf('jana')).getByRole('combobox', { name: 'Role uživatele Jana Nováková' }),
      'editor',
    )

    expect(roleMock).toHaveBeenCalledWith('u2', 'editor')
    await waitFor(() => {
      expect(
        within(rowOf('jana')).getByRole('combobox', { name: 'Role uživatele Jana Nováková' }),
      ).toHaveValue('editor')
    })
    expect(toastShow).toHaveBeenCalledWith({
      message: 'Jana Nováková má teď roli Editor.',
      variant: 'success',
    })
  })

  it('says the project needs an admin when the last one would be demoted', async () => {
    const user = userEvent.setup()
    roleMock.mockRejectedValue(new MemberApiError(409, 'last_admin'))
    fetchMock.mockResolvedValue([ME, member({ role: 'admin' })])
    renderPage()
    await screen.findByText('jana')

    const select = within(rowOf('jana')).getByRole('combobox', {
      name: 'Role uživatele Jana Nováková',
    })
    await user.selectOptions(select, 'viewer')

    await waitFor(() => {
      expect(toastShow).toHaveBeenCalledWith({
        message: 'Projekt musí mít alespoň jednoho administrátora.',
        variant: 'danger',
      })
    })
    // The refused change is not shown as done.
    expect(select).toHaveValue('admin')
  })

  it('reports a `self` refusal as a toast', async () => {
    const user = userEvent.setup()
    removeMock.mockRejectedValue(new MemberApiError(409, 'self'))
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odebrat' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Odebrat' }))

    await waitFor(() => {
      expect(toastShow).toHaveBeenCalledWith({
        message: 'Tohle nemůžete udělat sami sobě — požádejte jiného správce projektu.',
        variant: 'danger',
      })
    })
    expect(screen.getByText('jana')).toBeInTheDocument()
  })

  it('approves a waiting membership', async () => {
    const user = userEvent.setup()
    approveMock.mockResolvedValue({ ...WAITING, approved_at: '2026-10-13T09:00:00Z' })
    renderPage()
    await screen.findAllByText('novak')

    await user.click(within(rowOf('novak')).getByRole('button', { name: 'Schválit' }))

    expect(approveMock).toHaveBeenCalledWith('u3')
    await waitFor(() => {
      expect(within(rowOf('novak')).queryByText('Čeká na schválení')).not.toBeInTheDocument()
    })
    expect(toastShow).toHaveBeenCalledWith({
      message: 'novak teď má do projektu přístup.',
      variant: 'success',
    })
  })

  it('removes a member behind a confirmation that keeps the account', async () => {
    const user = userEvent.setup()
    removeMock.mockResolvedValue()
    renderPage()
    await screen.findByText('jana')

    await user.click(within(rowOf('jana')).getByRole('button', { name: 'Odebrat' }))
    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText('Uživatel ztratí přístup k projektu; jeho účet zůstane.'),
    ).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Odebrat' }))

    expect(removeMock).toHaveBeenCalledWith('u2')
    await waitFor(() => {
      expect(screen.queryByText('jana')).not.toBeInTheDocument()
    })
  })

  // The service resolves alike for every answer (see services/members.test.ts),
  // so the page has exactly one thing to say after a submit.
  it('adds a member and answers with the neutral sentence', async () => {
    const user = userEvent.setup()
    addMock.mockResolvedValue()
    renderPage()
    await screen.findByText('jana')
    fetchMock.mockClear()

    await user.click(screen.getByRole('button', { name: 'Přidat člena' }))
    const dialog = await screen.findByRole('dialog', { name: 'Přidat člena' })
    await user.type(
      within(dialog).getByLabelText('Uživatelské jméno nebo e-mail (přesně)'),
      ' petr@example.cz ',
    )
    await user.selectOptions(within(dialog).getByLabelText('Role'), 'editor')
    await user.click(within(dialog).getByRole('button', { name: 'Přidat' }))

    expect(addMock).toHaveBeenCalledWith('petr@example.cz', 'editor')
    expect(
      await within(dialog).findByText(
        'Pokud takový účet existuje, byl přidán. V seznamu se objeví, až projekt poprvé otevře.',
      ),
    ).toBeInTheDocument()
    expect(within(dialog).queryByText(/nenalezen|neexistuje/i)).not.toBeInTheDocument()
    // And the list is refetched, in case somebody was added.
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })
  })

  // An added account is not listed until it opens the project, so the way back
  // is as blind as the add: the same identifier, one neutral sentence.
  it('withdraws an add by its identifier, without a role, and answers neutrally', async () => {
    const user = userEvent.setup()
    withdrawMock.mockResolvedValue()
    renderPage()
    await screen.findByText('jana')
    fetchMock.mockClear()

    await user.click(screen.getByRole('button', { name: 'Zrušit přidání' }))
    const dialog = await screen.findByRole('dialog', { name: 'Zrušit přidání člena' })
    expect(within(dialog).queryByLabelText('Role')).not.toBeInTheDocument()
    await user.type(
      within(dialog).getByLabelText('Uživatelské jméno nebo e-mail (přesně)'),
      'petr@example.cz',
    )
    await user.click(within(dialog).getByRole('button', { name: 'Zrušit přidání' }))

    expect(withdrawMock).toHaveBeenCalledWith('petr@example.cz')
    expect(addMock).not.toHaveBeenCalled()
    expect(
      await within(dialog).findByText(
        'Pokud takový účet existuje a projekt ještě neotevřel, už do něj přístup nemá.',
      ),
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })
  })

  it('asks for an identifier instead of sending an empty one', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('jana')

    await user.click(screen.getByRole('button', { name: 'Přidat člena' }))
    const dialog = await screen.findByRole('dialog', { name: 'Přidat člena' })
    await user.click(within(dialog).getByRole('button', { name: 'Přidat' }))

    expect(addMock).not.toHaveBeenCalled()
    expect(within(dialog).getByText('Vyplňte uživatelské jméno nebo e-mail.')).toBeInTheDocument()
  })

  it('offers a retry when the list cannot be loaded', async () => {
    fetchMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Členy se nepodařilo načíst.')).toBeInTheDocument()
  })
})
