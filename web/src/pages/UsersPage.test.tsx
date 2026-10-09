import { getDefaultNormalizer, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { ToastProvider } from '../components/toast/ToastProvider'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type AdminUser } from '../services/users'
import { installRule } from '../test/css'
import { expectLive, expectOff } from '../test/reasoned'

import { UserFormModal, UsersPage } from './UsersPage'

vi.mock('../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/users')>()
  return {
    ...actual,
    fetchUsers: vi.fn(),
    createUser: vi.fn(),
    updateUser: vi.fn(),
    setUserDisabled: vi.fn(),
    resetUserPassword: vi.fn(),
    approveUser: vi.fn(),
    issuePasswordReset: vi.fn(),
    renameUser: vi.fn(),
  }
})

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

// The roster asks the instance whether an approval is followed by an e-mail at
// all, so the dialogs can say what actually happens.
vi.mock('../services/settings', () => ({ fetchPublicSettings: vi.fn() }))

const {
  approveUser,
  createUser,
  fetchUsers,
  issuePasswordReset,
  renameUser,
  setUserDisabled,
  updateUser,
} = await import('../services/users')
const { fetchSubjects } = await import('../services/people')
const { fetchPublicSettings } = await import('../services/settings')
const fetchPublicSettingsMock = vi.mocked(fetchPublicSettings)
const fetchSubjectsMock = vi.mocked(fetchSubjects)
const fetchUsersMock = vi.mocked(fetchUsers)
const createUserMock = vi.mocked(createUser)
const setUserDisabledMock = vi.mocked(setUserDisabled)
const updateUserMock = vi.mocked(updateUser)
const approveUserMock = vi.mocked(approveUser)
const issuePasswordResetMock = vi.mocked(issuePasswordReset)
const renameUserMock = vi.mocked(renameUser)

/**
 * What the backend answers when a change would take away the instance's last
 * enabled maintainer (`auth.ErrLastMaintainer` → 409); the page tells it apart
 * from the other 409, a duplicate username, by the message.
 */
const LAST_MAINTAINER_ERROR = new ApiError(409, 'auth: cannot remove the last maintainer')

/** The opening words of the last-maintainer explanation, for a partial match. */
const LAST_MAINTAINER_TEXT = /This is the last enabled maintainer/

/** The signed-in administrator; their own row must not offer self-disabling. */
const ME = 'u-admin'

/** Builds an admin user row, defaulting to an enabled viewer. */
function user(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    uid: 'u1',
    username: 'ada',
    display_name: 'Ada Lovelace',
    email: 'ada@example.com',
    role: 'viewer',
    disabled: false,
    note: '',
    // Let in long ago unless a test says otherwise: an account waiting for an
    // administrator is the exception, not the default row.
    approved_at: '2026-01-02T10:05:00Z',
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    ...overrides,
  }
}

function auth(opts: { isAdmin?: boolean; isMaintainer?: boolean } = {}): AuthContextValue {
  const { isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role = isMaintainer ? 'maintainer' : isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: ME, username: 'root', display_name: 'Root', role },
    role,
    downloadToken: null,
    canCurate: isAdmin,
    canWrite: isAdmin,
    isAdmin,
    isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, so
 * `useIsNarrowViewport` — and through it the roster's table/card choice — takes
 * the branch under test.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** Prints the router's current path + query, so a test can read the URL state. */
function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

/** The browser's Back button, as far as the router is concerned. */
function BackProbe() {
  const navigate = useNavigate()
  return (
    <button
      type="button"
      onClick={() => {
        void navigate(-1)
      }}
    >
      History back
    </button>
  )
}

/** The URL the probe currently reports. */
function currentUrl(): string {
  return screen.getByTestId('location').textContent
}

/**
 * Renders the page at `path`. The default is `?all=1` — the whole roster —
 * because most tests are about accounts that were let in long ago; the tests of
 * the waiting-only default pass the bare `/users`.
 */
function renderPage(value: AuthContextValue = auth({ isAdmin: true }), path = '/users?all=1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <ToastProvider>
          <MemoryRouter initialEntries={[path]}>
            <UsersPage />
            <LocationProbe />
            <BackProbe />
          </MemoryRouter>
        </ToastProvider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** A promise whose settling the test controls, to observe the in-flight state. */
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined
  let reject: (reason: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchUsersMock.mockReset()
  fetchUsersMock.mockResolvedValue([])
  fetchSubjectsMock.mockReset()
  fetchSubjectsMock.mockResolvedValue([])
  createUserMock.mockReset()
  setUserDisabledMock.mockReset()
  updateUserMock.mockReset()
  approveUserMock.mockReset()
  issuePasswordResetMock.mockReset()
  renameUserMock.mockReset()
  fetchPublicSettingsMock.mockReset()
  fetchPublicSettingsMock.mockResolvedValue({
    registration_enabled: false,
    passkeys_enabled: false,
    mail_enabled: true,
  })
})

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('UsersPage', () => {
  it('denies access to non-admins and never fetches the roster', () => {
    renderPage(auth())

    expect(screen.getByText('This page is available to administrators only.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Users' })).not.toBeInTheDocument()
    expect(fetchUsersMock).not.toHaveBeenCalled()
  })

  it('offers the maintainer role only to a maintainer', async () => {
    fetchUsersMock.mockResolvedValue([])
    const actor = userEvent.setup()

    // A plain admin cannot grant maintainer, so the role select omits it.
    const adminView = renderPage(auth({ isAdmin: true }))
    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const adminDialog = await screen.findByRole('dialog')
    const adminSelect = within(adminDialog).getByLabelText('Role')
    expect(within(adminSelect).getByRole('option', { name: 'Administrator' })).toBeInTheDocument()
    expect(within(adminSelect).queryByRole('option', { name: 'Maintainer' })).toBeNull()
    adminView.unmount()

    // A maintainer sees the maintainer option and can assign it.
    renderPage(auth({ isMaintainer: true }))
    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const maintainerDialog = await screen.findByRole('dialog')
    const maintainerSelect = within(maintainerDialog).getByLabelText('Role')
    expect(within(maintainerSelect).getByRole('option', { name: 'Maintainer' })).toBeInTheDocument()
  })

  it('offers the curator role to a plain admin, in ladder order', async () => {
    fetchUsersMock.mockResolvedValue([])
    const actor = userEvent.setup()

    // Only maintainer is restricted; any admin may grant curator.
    renderPage(auth({ isAdmin: true }))
    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')
    const select = within(dialog).getByLabelText('Role')
    const names = within(select)
      .getAllByRole('option')
      .map((option) => option.textContent)
    expect(names).toEqual(['Viewer', 'Curator', 'Editor', 'Administrator'])
  })

  it('locks a maintainer account against a non-maintainer admin', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u9', username: 'ops', role: 'maintainer' })])
    renderPage(auth({ isAdmin: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const row = screen.getByText('ops').closest('tr') as HTMLElement
    // A non-maintainer cannot edit, reset the password of, or disable a maintainer.
    const locked = 'Only a system maintainer can manage this account.'
    for (const name of ['Edit', 'Change password', 'Disable']) {
      const button = within(row).getByRole('button', { name })
      expectOff(button)
      // Reachable both ways: the tooltip for a mouse, the printed line the
      // button describes itself by for a keyboard, a screen reader and a phone.
      expect(button).toHaveAttribute('title', locked)
      expect(button).toHaveAttribute('aria-describedby', within(row).getByText(locked).id)
    }
  })

  it('lets a maintainer manage another maintainer account', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u9', username: 'ops', role: 'maintainer' })])
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const row = screen.getByText('ops').closest('tr') as HTMLElement
    expectLive(within(row).getByRole('button', { name: 'Edit' }))
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

  it('keeps a note written on two lines on two lines', async () => {
    // The note is typed into a `<textarea>`, so it comes back with the breaks the
    // administrator put there; HTML's whitespace collapsing would run them into
    // one line. jsdom loads no stylesheet, so the real rule comes from `app.css`.
    const uninstall = installRule('src/styles/app.css', '.kk-multiline')
    try {
      fetchUsersMock.mockResolvedValue([user({ note: 'On loan\nuntil June' })])
      renderPage()

      expect(await screen.findByText('ada')).toBeInTheDocument()
      const note = screen.getByText('On loan\nuntil June', {
        normalizer: getDefaultNormalizer({ collapseWhitespace: false, trim: false }),
      })
      expect(getComputedStyle(note).whiteSpace).toBe('pre-wrap')
    } finally {
      uninstall()
    }
  })

  it('keeps those line breaks on the phone card as well', async () => {
    // The card is the same column read out in a different layout; a note that
    // reads as two lines on the desktop table may not silently become one here.
    mockViewport(true)
    const uninstall = installRule('src/styles/app.css', '.kk-multiline')
    try {
      fetchUsersMock.mockResolvedValue([user({ note: 'On loan\nuntil June' })])
      renderPage()

      expect(await screen.findByText('ada')).toBeInTheDocument()
      const note = within(screen.getByRole('listitem')).getByText('On loan\nuntil June', {
        normalizer: getDefaultNormalizer({ collapseWhitespace: false, trim: false }),
      })
      expect(getComputedStyle(note).whiteSpace).toBe('pre-wrap')
    } finally {
      uninstall()
    }
  })

  it('keeps the full ten-column table on a wide viewport', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('columnheader')).toHaveLength(10)
    // No card stack alongside it: only one of the two layouts is ever in the DOM.
    expect(screen.queryByRole('listitem')).toBeNull()
  })

  it('shows an approved account on a phone as a compact card with its actions folded', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({
        uid: 'u1',
        username: 'ada',
        display_name: 'Ada Lovelace',
        role: 'editor',
        note: 'On loan from the analytical engine',
        last_login_at: '2026-06-30T08:15:00Z',
      }),
    ])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    // The wide table is gone entirely — nothing left to drag sideways.
    expect(screen.queryByRole('table')).toBeNull()

    // One record, one card: who it is, then a short list of what matters.
    const card = screen.getByRole('listitem')
    expect(within(card).getByText('Ada Lovelace')).toBeInTheDocument()
    expect(within(card).getByText('Editor')).toBeInTheDocument()
    expect(within(card).getByText('Enabled')).toBeInTheDocument()
    expect(within(card).getByText('ada@example.com')).toBeInTheDocument()
    expect(within(card).getByText('Last login')).toBeInTheDocument()
    expect(within(card).getByText('On loan from the analytical engine')).toBeInTheDocument()
    // No Approve on an account that was already let in.
    expect(within(card).queryByRole('button', { name: 'Approve' })).toBeNull()

    // The secondary actions are folded, not a stack of buttons under every person.
    const more = within(card).getByRole('button', { name: 'More actions' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    expect(within(card).queryByRole('button', { name: 'Edit' })).toBeNull()

    await actor.click(more)
    expect(more).toHaveAttribute('aria-expanded', 'true')
    const foldId = more.getAttribute('aria-controls')
    const fold = screen.getAllByRole('button', { name: 'Edit' })[0].closest(`[id="${foldId}"]`)
    if (!(fold instanceof HTMLElement)) {
      throw new Error('the fold does not hold the secondary actions')
    }
    expect(card).toContainElement(fold)
    for (const name of ['Edit', 'Change password', 'Reset link', 'Disable']) {
      expectLive(within(fold).getByRole('button', { name }))
    }
    // The action column header is not repeated as a field label.
    expect(within(card).queryByText('Actions')).toBeNull()
  })

  it('keeps the maintainer boundary and the self-disable guard inside the fold', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({ uid: ME, username: 'root', display_name: 'Root', role: 'admin' }),
      user({ uid: 'u9', username: 'ops', role: 'maintainer' }),
    ])
    const actor = userEvent.setup()
    renderPage(auth({ isAdmin: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const [own, maintainer] = screen.getAllByRole('listitem')
    await actor.click(within(own).getByRole('button', { name: 'More actions' }))
    await actor.click(within(maintainer).getByRole('button', { name: 'More actions' }))

    // Own account: disabling is refused, with the reason spelled out on the card.
    const ownDisable = within(own).getByRole('button', { name: 'Disable' })
    expectOff(ownDisable)
    // The sentence is on the card itself, and the button points at it — one
    // copy, reachable by hover, by focus and by eye on a phone alike.
    const ownHint = within(own).getByText('You cannot disable your own account.')
    expect(ownDisable).toHaveAttribute('aria-describedby', ownHint.id)
    expectLive(within(own).getByRole('button', { name: 'Edit' }))
    // A maintainer's account is untouchable for a plain admin, same as on the table.
    const locked = within(maintainer).getByText('Only a system maintainer can manage this account.')
    for (const name of ['Edit', 'Change password', 'Reset link', 'Disable']) {
      const button = within(maintainer).getByRole('button', { name })
      expectOff(button)
      expect(button).toHaveAttribute('aria-describedby', locked.id)
    }
  })

  it('opens the confirmation dialog from a phone card’s folded actions', async () => {
    mockViewport(true)
    const ada = user({ uid: 'u1', username: 'ada' })
    fetchUsersMock.mockResolvedValue([ada])
    setUserDisabledMock.mockResolvedValue({ ...ada, disabled: true })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const card = screen.getByRole('listitem')
    await actor.click(within(card).getByRole('button', { name: 'More actions' }))
    await actor.click(within(card).getByRole('button', { name: 'Disable' }))

    const dialog = await screen.findByRole('dialog')
    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))
    await waitFor(() => {
      expect(setUserDisabledMock).toHaveBeenCalledWith(ada, true)
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('shows a waiting account on a phone as a compact card with one big Approve', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({
        uid: 'u1',
        username: 'jana',
        display_name: 'Jana Nováková',
        email: 'jana@example.com',
        approved_at: null,
        note: 'Admin-only remark',
        created_at: '2026-10-08T18:30:00Z',
      }),
    ])
    renderPage(auth({ isAdmin: true }), '/users')

    const card = await screen.findByRole('listitem')
    expect(within(card).getByText('Jana Nováková')).toBeInTheDocument()
    expect(within(card).getByText('jana')).toBeInTheDocument()
    expect(within(card).getByText('jana@example.com')).toBeInTheDocument()
    expect(within(card).getByText('Registered')).toBeInTheDocument()
    expect(within(card).getByText('Waiting for approval')).toBeInTheDocument()
    // Compact: the roster's other columns are not on a waiting card.
    expect(within(card).queryByText('Admin-only remark')).toBeNull()
    expect(within(card).queryByText('Last login')).toBeNull()

    const approve = within(card).getByRole('button', { name: 'Approve' })
    expectLive(approve)
    expect(approve).toHaveClass('btn-success', 'btn-lg', 'w-100')
    // Only Approve and the fold toggle are on the folded card.
    expect(
      within(card)
        .getAllByRole('button')
        .map((button) => button.textContent),
    ).toEqual(['Approve', 'More actions'])
  })

  it('approves from a phone card in one tap and takes the card off the waiting list', async () => {
    mockViewport(true)
    const jana = user({ uid: 'u1', username: 'jana', display_name: 'Jana', approved_at: null })
    const petr = user({ uid: 'u2', username: 'petr', display_name: 'Petr', approved_at: null })
    fetchUsersMock.mockResolvedValue([jana, petr])
    const request = deferred<AdminUser>()
    approveUserMock.mockReturnValue(request.promise)
    const actor = userEvent.setup()
    renderPage(auth({ isAdmin: true }), '/users')

    expect(await screen.findByText('Jana')).toBeInTheDocument()
    const card = screen.getAllByRole('listitem')[0]
    await actor.click(within(card).getByRole('button', { name: 'Approve' }))

    // No confirmation step: the request is already out.
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(approveUserMock).toHaveBeenCalledWith('u1')
    // While it runs the button is disabled, so a second tap cannot fire twice.
    const busy = within(card).getByRole('button', { name: 'Approve' })
    expect(busy).toBeDisabled()
    await actor.click(busy)
    expect(approveUserMock).toHaveBeenCalledTimes(1)
    // The other waiting person's card is unaffected.
    expectLive(within(screen.getAllByRole('listitem')[1]).getByRole('button', { name: 'Approve' }))

    request.resolve({ ...jana, approved_at: '2026-10-09T09:00:00Z' })

    // The card leaves the waiting list straight away; the next person stays.
    await waitFor(() => {
      expect(screen.queryByText('Jana')).toBeNull()
    })
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('Petr')).toBeInTheDocument()
    expect(screen.getByText('Waiting for approval: 1')).toBeInTheDocument()
    expect(
      await screen.findByText('Jana was approved and will get an e-mail about it.'),
    ).toBeVisible()
    expect(fetchUsersMock).toHaveBeenCalledTimes(1)
  })

  it('keeps a refused approval on the card it belongs to', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'jana', approved_at: null }),
      user({ uid: 'u2', username: 'petr', approved_at: null }),
    ])
    approveUserMock.mockRejectedValue(new ApiError(409, 'auth: user is disabled'))
    const actor = userEvent.setup()
    renderPage(auth({ isAdmin: true }), '/users')

    expect(await screen.findByText('jana')).toBeInTheDocument()
    const [jana, petr] = screen.getAllByRole('listitem')
    await actor.click(within(jana).getByRole('button', { name: 'Approve' }))

    expect(await within(jana).findByRole('alert')).toHaveTextContent('The account is blocked.')
    expect(within(petr).queryByRole('alert')).toBeNull()
    // Nothing moved: the account is still waiting, and Approve is live again.
    expect(within(jana).getByText('Waiting for approval')).toBeInTheDocument()
    expectLive(within(jana).getByRole('button', { name: 'Approve' }))
  })

  it('says why Approve is off on a phone card, outside the fold', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'jana', approved_at: null, disabled: true }),
    ])
    renderPage(auth({ isAdmin: true }), '/users')

    const card = await screen.findByRole('listitem')
    expectOff(
      within(card).getByRole('button', { name: 'Approve' }),
      'A blocked account cannot be approved. Enable it first.',
    )
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
    await actor.type(within(dialog).getByLabelText('E-mail'), 'ada@example.com')
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

    expectOff(own)
    const ownHint = within(rows[1]).getByText('You cannot disable your own account.')
    expect(own).toHaveAttribute('aria-describedby', ownHint.id)
    expectLive(other)
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

  it('explains why the last maintainer cannot be disabled', async () => {
    const solo = user({ uid: 'u1', username: 'solo', role: 'maintainer' })
    fetchUsersMock.mockResolvedValue([solo])
    setUserDisabledMock.mockRejectedValue(LAST_MAINTAINER_ERROR)
    const actor = userEvent.setup()
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('solo')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))
    const dialog = await screen.findByRole('dialog')
    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))

    // The refusal names the rule and what to do about it, not "action failed".
    expect(await screen.findByRole('alert')).toHaveTextContent(LAST_MAINTAINER_TEXT)
    expect(screen.queryByText('The action could not be completed.')).not.toBeInTheDocument()
    // The row survives: nothing was disabled.
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows the last-maintainer refusal as a form-level alert, not a username error', async () => {
    const solo = user({ uid: 'u1', username: 'solo', role: 'maintainer' })
    fetchUsersMock.mockResolvedValue([solo])
    updateUserMock.mockRejectedValue(LAST_MAINTAINER_ERROR)
    const actor = userEvent.setup()
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('solo')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    await actor.selectOptions(within(dialog).getByLabelText('Role'), 'admin')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalled()
    })

    // The other 409 (a duplicate username) flags the username input; this one
    // belongs to no field, so it must not be mistaken for it.
    expect(within(dialog).getByRole('alert')).toHaveTextContent(LAST_MAINTAINER_TEXT)
    expect(within(dialog).getByLabelText('Username')).not.toHaveClass('is-invalid')
  })

  it('will not create an account without an e-mail address', async () => {
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    await actor.type(within(dialog).getByLabelText('Username'), 'ada')
    await actor.type(within(dialog).getByLabelText('Password'), 'correct-horse')
    await actor.click(within(dialog).getByRole('button', { name: 'Create' }))

    // The request is never made: the backend would refuse it anyway, so the
    // dialog says so where the reader is already looking.
    expect(createUserMock).not.toHaveBeenCalled()
    expect(within(dialog).getByLabelText('E-mail')).toHaveClass('is-invalid')
    expect(within(dialog).getByText('Enter a valid e-mail address.')).toBeInTheDocument()
  })

  it('edits an account without losing its address, and refuses to clear it', async () => {
    const ada = user({ uid: 'u1', username: 'ada', email: 'ada@example.com' })
    fetchUsersMock.mockResolvedValue([ada])
    updateUserMock.mockResolvedValue({ ...ada, display_name: 'Ada L' })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')

    // The stored address is offered for editing, not silently echoed back.
    const email = within(dialog).getByLabelText('E-mail')
    expect(email).toHaveValue('ada@example.com')

    await actor.clear(email)
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(updateUserMock).not.toHaveBeenCalled()

    await actor.type(email, 'ada.lovelace@example.com')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalledWith(
        'u1',
        expect.objectContaining({ email: 'ada.lovelace@example.com' }),
      )
    })
  })

  it('offers the username for editing on an account the admin may manage', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog')
    const username = within(dialog).getByLabelText('Username')
    expect(username).not.toHaveAttribute('readonly')
    expect(username).toHaveValue('ada')
    expect(within(dialog).getByText(/signs in with the new name/)).toBeInTheDocument()
    // Editing offers no password field; that is a separate dialog.
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
  })

  it('renames before saving the profile and updates the roster row in order', async () => {
    const ada = user({ uid: 'u1', username: 'ada' })
    const bob = user({ uid: 'u2', username: 'bob', display_name: 'Bob', email: 'bob@example.com' })
    fetchUsersMock.mockResolvedValue([ada, bob])
    renameUserMock.mockResolvedValue({ ...ada, username: 'zoe' })
    updateUserMock.mockResolvedValue({ ...ada, username: 'zoe' })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const adaRow = screen.getByText('ada').closest('tr') as HTMLElement
    await actor.click(within(adaRow).getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const username = within(dialog).getByLabelText('Username')
    await actor.clear(username)
    await actor.type(username, 'Zoe')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(renameUserMock).toHaveBeenCalledWith('u1', 'Zoe')
    expect(renameUserMock.mock.invocationCallOrder[0]).toBeLessThan(
      updateUserMock.mock.invocationCallOrder[0] ?? 0,
    )
    expect(screen.queryByText('ada')).not.toBeInTheDocument()
    // The renamed row moves to where its new name sorts: after bob.
    const names = screen
      .getAllByRole('row')
      .slice(1)
      .map((row) => within(row).getAllByRole('cell')[0]?.textContent)
    expect(names).toEqual(['bob', 'zoe'])
  })

  it('does not send a rename when only the letter case or spacing differs', async () => {
    const ada = user({ uid: 'u1', username: 'ada' })
    fetchUsersMock.mockResolvedValue([ada])
    updateUserMock.mockResolvedValue(ada)
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const username = within(dialog).getByLabelText('Username')
    await actor.clear(username)
    await actor.type(username, ' ADA ')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalled()
    })
    expect(renameUserMock).not.toHaveBeenCalled()
  })

  it('leaves an untouched legacy mixed-case username alone on save', async () => {
    const petr = user({ uid: 'u1', username: 'Petr' })
    fetchUsersMock.mockResolvedValue([petr])
    updateUserMock.mockResolvedValue(petr)
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Petr')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalled()
    })
    expect(renameUserMock).not.toHaveBeenCalled()
  })

  it('shows a taken username inline on the field and saves nothing else', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    renameUserMock.mockRejectedValue(new ApiError(409, 'username already taken'))
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const username = within(dialog).getByLabelText('Username')
    await actor.clear(username)
    await actor.type(username, 'bob')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(username).toHaveClass('is-invalid')
    })
    expect(within(dialog).getByText('That username is already taken.')).toBeInTheDocument()
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    expect(updateUserMock).not.toHaveBeenCalled()
  })

  it('shows an over-long username refusal inline with the limit', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    renameUserMock.mockRejectedValue(
      new ApiError(400, 'auth: username must be at most 64 characters'),
    )
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const username = within(dialog).getByLabelText('Username')
    await actor.clear(username)
    await actor.type(username, 'someone')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(
      await within(dialog).findByText('A username can be at most 64 characters long.'),
    ).toBeInTheDocument()
    expect(username).toHaveClass('is-invalid')
  })

  it('keeps the username read-only on an account the actor may not manage', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <UserFormModal
          user={user({ uid: 'u9', username: 'ops', role: 'maintainer' })}
          isMaintainer={false}
          canManage={false}
          onHide={vi.fn()}
          onSaved={vi.fn()}
          onRenamed={vi.fn()}
        />
      </I18nextProvider>,
    )

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Username')).toHaveAttribute('readonly')
    expect(
      within(dialog).getByText(
        "Only another system maintainer can change a system maintainer's username.",
      ),
    ).toBeInTheDocument()
  })

  it('gives the create/edit dialog the whole phone screen with its actions pinned', async () => {
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    // react-bootstrap maps `fullscreen="sm-down"` + `scrollable` onto these two
    // dialog classes: below `sm` the form gets the whole screen instead of a
    // cramped centred card, and the body is the only part that scrolls — so the
    // footer stays pinned above the on-screen keyboard rather than under it.
    expect(dialog.querySelector('.modal-dialog')).toHaveClass(
      'modal-fullscreen-sm-down',
      'modal-dialog-scrollable',
    )

    // The <form> wraps header, body and footer, so it has to hand Bootstrap's
    // height cap through to the body instead of sizing to its own content.
    expect(dialog.querySelector('form')).toHaveClass('d-flex', 'flex-column', 'overflow-hidden')

    // Both footer actions render — neither is dropped by the reflow.
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Create' })).toBeInTheDocument()
  })

  it('gives the password dialog the same full-screen sheet and pinned actions', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Change password' }))
    const dialog = await screen.findByRole('dialog')

    expect(dialog.querySelector('.modal-dialog')).toHaveClass(
      'modal-fullscreen-sm-down',
      'modal-dialog-scrollable',
    )
    expect(dialog.querySelector('form')).toHaveClass('d-flex', 'flex-column', 'overflow-hidden')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Change password' })).toBeInTheDocument()
  })

  it('keeps the enable/disable question a centred card, only scrollable', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))
    const dialog = await screen.findByRole('dialog')

    // A question with no inputs summons no keyboard, so it keeps its centred
    // card on every screen; `scrollable` still pins the two buttons.
    const dialogEl = dialog.querySelector('.modal-dialog')
    expect(dialogEl).toHaveClass('modal-dialog-scrollable', 'modal-dialog-centered')
    expect(dialogEl).not.toHaveClass('modal-fullscreen-sm-down')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Disable' })).toBeInTheDocument()
  })

  it('names the person an account is linked to, and an em dash when it is not', async () => {
    fetchSubjectsMock.mockResolvedValue([
      { uid: 'sub1', slug: 'jarmila', name: 'Jarmila', photo_count: 12 },
    ] as never)
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', subject_uid: 'sub1' }),
      user({ uid: 'u2', username: 'bob' }),
    ])
    renderPage()

    // The people arrive on their own request, so the name replaces the UID a
    // beat after the roster renders.
    expect(await screen.findByText('Jarmila')).toBeInTheDocument()
    const rows = screen.getAllByRole('row')
    // Row 0 is the header; the roster is ordered as the backend returned it, and
    // the person is the sixth column (after username, name, e-mail, role and state).
    expect(within(rows[1]).getAllByRole('cell')[5]).toHaveTextContent('Jarmila')
    expect(within(rows[2]).getAllByRole('cell')[5]).toHaveTextContent('—')
  })

  it('lets an administrator link an account to a person, and says what that does', async () => {
    const actor = userEvent.setup()
    fetchSubjectsMock.mockResolvedValue([
      { uid: 'sub1', slug: 'jarmila', name: 'Jarmila', photo_count: 12 },
    ] as never)
    fetchUsersMock.mockResolvedValue([user()])
    updateUserMock.mockResolvedValue(user({ subject_uid: 'sub1' }))
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')

    // The consequence is on the form, next to the field that causes it.
    expect(within(dialog).getByText(/cover photo is then shown/i)).toBeInTheDocument()

    // The role select is a combobox too, so name the field.
    await actor.type(within(dialog).getByRole('combobox', { name: 'Find a person' }), 'Jar')
    await actor.click(await within(dialog).findByRole('option', { name: /Jarmila/ }))
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalledWith(
        'u1',
        expect.objectContaining({ subject_uid: 'sub1' }),
      )
    })
  })

  it('shows each account’s address, and a placeholder as no address at all', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', email: 'ada@example.com' }),
      user({ uid: 'u2', username: 'root', email: 'root@kukatko.invalid' }),
    ])
    renderPage()

    expect(await screen.findByText('ada@example.com')).toBeInTheDocument()
    // The account that only has a stand-in address is never shown as reachable:
    // every mail the app sends goes to this field.
    expect(screen.queryByText('root@kukatko.invalid')).not.toBeInTheDocument()
    expect(screen.getByText('No address')).toBeInTheDocument()
    expect(screen.getByText(/Fill in a real one/)).toBeInTheDocument()
  })

  it('badges a waiting account apart from a blocked one, and offers Approve only there', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', approved_at: null }),
      user({ uid: 'u2', username: 'bob', disabled: true }),
    ])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const waiting = screen.getByText('ada').closest('tr') as HTMLElement
    const blocked = screen.getByText('bob').closest('tr') as HTMLElement

    // Two states, two badges, two colours — never the same paint.
    expect(within(waiting).getByText('Waiting for approval')).toHaveClass('bg-warning')
    expect(within(blocked).getByText('Disabled')).toHaveClass('bg-danger')
    expect(within(waiting).queryByText('Enabled')).toBeNull()

    // Approve is the answer to a question only the waiting row is asking.
    expectLive(within(waiting).getByRole('button', { name: 'Approve' }))
    expect(within(blocked).queryByRole('button', { name: 'Approve' })).toBeNull()
  })

  it('will not offer to approve a blocked account, and says why', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', approved_at: null, disabled: true }),
    ])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const row = screen.getByText('ada').closest('tr') as HTMLElement
    // The backend refuses it (409) and it would be half a decision anyway: the
    // person still could not sign in. The reason is printed on the row.
    expectOff(
      within(row).getByRole('button', { name: 'Approve' }),
      'A blocked account cannot be approved. Enable it first.',
    )
  })

  it('approves a waiting account on the table in one click, and updates the row in place', async () => {
    const ada = user({ uid: 'u1', username: 'ada', approved_at: null })
    fetchUsersMock.mockResolvedValue([ada])
    approveUserMock.mockResolvedValue({ ...ada, approved_at: '2026-08-24T09:00:00Z' })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const row = screen.getByText('ada').closest('tr') as HTMLElement
    await actor.click(within(row).getByRole('button', { name: 'Approve' }))

    // No dialog on the desktop either: one rule for both layouts.
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(approveUserMock).toHaveBeenCalledWith('u1')

    // The row is the approved one — no second fetch of the whole roster.
    expect(await screen.findByText('Enabled')).toBeInTheDocument()
    expect(screen.queryByText('Waiting for approval')).toBeNull()
    expect(
      screen.getByText('Ada Lovelace was approved and will get an e-mail about it.'),
    ).toBeVisible()
    expect(fetchUsersMock).toHaveBeenCalledTimes(1)
  })

  it('does not promise an approval e-mail on an instance that sends none', async () => {
    // Mail off means the no-op sender: the account hears nothing, so the
    // confirmation tells the administrator to say it themselves.
    fetchPublicSettingsMock.mockResolvedValue({
      registration_enabled: false,
      passkeys_enabled: false,
      mail_enabled: false,
    })
    const ada = user({ uid: 'u1', username: 'ada', approved_at: null })
    fetchUsersMock.mockResolvedValue([ada])
    approveUserMock.mockResolvedValue({ ...ada, approved_at: '2026-08-24T09:00:00Z' })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    // The mail setting arrives on its own request; wait for it before acting.
    await waitFor(() => {
      expect(fetchPublicSettingsMock).toHaveBeenCalled()
    })
    await actor.click(screen.getByRole('button', { name: 'Approve' }))

    expect(await screen.findByText(/Mail is switched off, so tell them yourself/)).toBeVisible()
    expect(screen.queryByText(/will get an e-mail about it/)).toBeNull()
  })

  it('leaves the row waiting when the approval is refused, and explains on the row', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada', approved_at: null })])
    approveUserMock.mockRejectedValue(new ApiError(409, 'auth: user is disabled'))
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Approve' }))

    // A 409 on a row action is the blocked account, never a taken username.
    const row = screen.getByText('ada').closest('tr') as HTMLElement
    expect(await within(row).findByRole('alert')).toHaveTextContent('The account is blocked.')
    expect(screen.queryByText('That username is already taken.')).toBeNull()
    // Nothing moved: the account is still waiting to be let in.
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument()
  })

  it('shows only the waiting accounts when the URL says nothing', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', approved_at: null }),
      user({ uid: 'u2', username: 'bob' }),
    ])
    renderPage(auth({ isAdmin: true }), '/users')

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.queryByText('bob')).toBeNull()
    expect(screen.getByRole('checkbox', { name: 'Only waiting for approval' })).toBeChecked()
    expect(screen.getByText('Waiting for approval: 1')).toBeInTheDocument()
  })

  it('shows everybody under ?all=1', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', approved_at: null }),
      user({ uid: 'u2', username: 'bob' }),
    ])
    renderPage(auth({ isAdmin: true }), '/users?all=1')

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Only waiting for approval' })).not.toBeChecked()
  })

  it('writes the filter into the URL, and Back restores the previous view', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: 'u1', username: 'ada', approved_at: null }),
      user({ uid: 'u2', username: 'bob' }),
    ])
    const actor = userEvent.setup()
    renderPage(auth({ isAdmin: true }), '/users')

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(currentUrl()).toBe('/users')
    // The count is on the page whatever the filter says, so an administrator
    // who came for something else still notices the errand.
    expect(screen.getByText('Waiting for approval: 1')).toBeInTheDocument()

    await actor.click(screen.getByRole('checkbox', { name: 'Only waiting for approval' }))
    expect(currentUrl()).toBe('/users?all=1')
    expect(screen.getByText('bob')).toBeInTheDocument()
    // Still the count of the whole roster, and no second request for it.
    expect(screen.getByText('Waiting for approval: 1')).toBeInTheDocument()

    await actor.click(screen.getByRole('checkbox', { name: 'Only waiting for approval' }))
    expect(currentUrl()).toBe('/users')
    expect(screen.queryByText('bob')).toBeNull()

    // Each switch was a history entry: Back walks them in reverse.
    await actor.click(screen.getByRole('button', { name: 'History back' }))
    expect(currentUrl()).toBe('/users?all=1')
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(fetchUsersMock).toHaveBeenCalledTimes(1)
  })

  it('reads as "all done" when nobody is waiting, with the way to everybody', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    const actor = userEvent.setup()
    renderPage(auth({ isAdmin: true }), '/users')

    expect(await screen.findByText('Nobody is waiting for approval')).toBeInTheDocument()
    expect(screen.queryByText('ada')).toBeNull()
    expect(screen.getByText('Waiting for approval: 0')).toBeInTheDocument()

    await actor.click(screen.getByRole('button', { name: 'Show all users' }))
    expect(screen.getByText('ada')).toBeInTheDocument()
    expect(currentUrl()).toBe('/users?all=1')
    expect(screen.queryByText('Nobody is waiting for approval')).toBeNull()
  })

  it('issues a reset link from the row and forgets it when the dialog closes', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    issuePasswordResetMock.mockResolvedValue({
      reset_url: 'https://kukatko.example/password-reset/tok-123',
      expires_at: '2026-09-01T10:00:00Z',
      email: 'ada@example.com',
    })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Reset link' }))

    // It asks first: issuing kills the account's earlier unused link.
    const dialog = await screen.findByRole('dialog')
    expect(issuePasswordResetMock).not.toHaveBeenCalled()
    await actor.click(within(dialog).getByRole('button', { name: 'Issue the link' }))
    await waitFor(() => {
      expect(issuePasswordResetMock).toHaveBeenCalledWith('u1')
    })

    const field = await screen.findByLabelText('Password reset link')
    expect(field).toHaveValue('https://kukatko.example/password-reset/tok-123')
    await actor.click(screen.getByRole('button', { name: 'Copy' }))
    await expect(navigator.clipboard.readText()).resolves.toBe(
      'https://kukatko.example/password-reset/tok-123',
    )

    // Closing takes the link off the screen for good — it is a bearer
    // credential for somebody's password, not a row of the roster.
    await actor.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => {
      expect(screen.queryByLabelText('Password reset link')).toBeNull()
    })
    // The password of the account was not touched either way.
    expect(screen.getByText('ada')).toBeInTheDocument()
  })

  it('shows a rejected address next to the e-mail field, not in a banner', async () => {
    createUserMock.mockRejectedValue(new ApiError(400, 'auth: invalid email address'))
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    await actor.type(within(dialog).getByLabelText('Username'), 'ada')
    await actor.type(within(dialog).getByLabelText('Password'), 'correct-horse')
    await actor.type(within(dialog).getByLabelText('E-mail'), 'ada@localhost')
    await actor.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(createUserMock).toHaveBeenCalled()
    })
    expect(within(dialog).getByLabelText('E-mail')).toHaveClass('is-invalid')
    expect(within(dialog).getByText('Enter a valid e-mail address.')).toBeInTheDocument()
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('does not offer deleting a user', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
  })
})
