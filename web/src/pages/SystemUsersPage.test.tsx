import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { InstanceAccount } from '../services/projects'

import { SystemUsersPage } from './SystemUsersPage'

vi.mock('../services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/projects')>()
  return { ...actual, fetchInstanceAccounts: vi.fn(), setInstanceRole: vi.fn() }
})

const { fetchInstanceAccounts, setInstanceRole, ProjectApiError } =
  await import('../services/projects')
const fetchMock = vi.mocked(fetchInstanceAccounts)
const roleMock = vi.mocked(setInstanceRole)

/** An account row, overridable. */
function account(overrides: Partial<InstanceAccount> = {}): InstanceAccount {
  return {
    uid: 'u2',
    username: 'jana',
    display_name: 'Jana',
    email: 'jana@example.com',
    instance_role: 'user',
    disabled: false,
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
    user: { uid: 'u1', username: 'admin', display_name: 'Admin', role: 'maintainer' },
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
        },
        {
          project_uid: 'pjr',
          project_slug: 'rodina',
          project_name: 'Rodina',
          active: true,
          role: 'viewer',
          approved: false,
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
    expect(jana.getByText('v (Kurátor)')).toHaveAttribute('title', 'Veselice')
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
})
