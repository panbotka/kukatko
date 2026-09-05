import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import type { User } from '../../services/auth'
import type { SubjectCount } from '../../services/people'

import { WelcomeModal } from './WelcomeModal'

vi.mock('../../services/settings', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/settings')>()
  return { ...actual, fetchWelcomeMarkdown: vi.fn() }
})
vi.mock('../../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/auth')>()
  return { ...actual, markWelcomeSeen: vi.fn(), setMySubject: vi.fn() }
})
vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

const { fetchWelcomeMarkdown } = await import('../../services/settings')
const { markWelcomeSeen, setMySubject } = await import('../../services/auth')
const { fetchSubjects } = await import('../../services/people')

const welcomeMock = vi.mocked(fetchWelcomeMarkdown)
const seenMock = vi.mocked(markWelcomeSeen)
const linkMock = vi.mocked(setMySubject)
const subjectsMock = vi.mocked(fetchSubjects)

/** A signed-in account; `overrides` set the two fields the welcome reads. */
function account(overrides: Partial<User> = {}): User {
  return {
    uid: 'u1',
    username: 'anna',
    display_name: 'Anna',
    email: 'anna@example.test',
    role: 'editor',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    subject_uid: null,
    welcome_seen_at: null,
    ...overrides,
  }
}

/** A listed person with counts, as `GET /subjects` returns one. */
function counted(uid: string, name: string, photos: number): SubjectCount {
  return {
    uid,
    slug: name.toLowerCase().replace(/\s+/g, '-'),
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photos,
    photo_count: photos,
  }
}

/** Mounts the welcome under an authenticated context, as the shell does. */
function renderWelcome(user: User, refresh = vi.fn().mockResolvedValue(undefined)) {
  const auth = {
    status: 'authenticated',
    user,
    role: user.role,
    downloadToken: 'tok',
    canWrite: true,
    isAdmin: false,
    isMaintainer: false,
    canImport: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh,
  } as unknown as AuthContextValue

  const { unmount } = render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <WelcomeModal />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
  return { refresh, unmount }
}

/** The open welcome. It is a portal, so every assertion is scoped inside it. */
async function dialog() {
  return within(await screen.findByRole('dialog'))
}

describe('WelcomeModal', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    welcomeMock.mockResolvedValue('## Hello\n\nWelcome to the family archive.')
    seenMock.mockResolvedValue(account({ welcome_seen_at: '2026-08-24T10:00:00Z' }))
    linkMock.mockResolvedValue(account({ subject_uid: 'su_a' }))
    subjectsMock.mockResolvedValue([
      counted('su_a', 'Anna Nováková', 42),
      counted('su_b', 'Bob', 3),
    ])
  })

  it('greets an account that has never seen it with the administrator text', async () => {
    renderWelcome(account())

    const modal = await dialog()
    expect(modal.getByRole('heading', { name: 'Hello' })).toBeInTheDocument()
    expect(modal.getByText('Welcome to the family archive.')).toBeInTheDocument()
    // Step one is prose only; the question comes after Continue.
    expect(modal.queryByRole('searchbox')).not.toBeInTheDocument()
  })

  it('stays away — and asks the backend nothing — once the account has seen it', async () => {
    renderWelcome(account({ welcome_seen_at: '2026-08-01T09:00:00Z' }))

    await waitFor(() => {
      expect(welcomeMock).not.toHaveBeenCalled()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens straight on the question when the administrator wrote no text', async () => {
    welcomeMock.mockResolvedValue('   \n  ')
    renderWelcome(account())

    const modal = await dialog()
    expect(modal.getByRole('searchbox', { name: 'Find a person' })).toBeInTheDocument()
    expect(modal.queryByRole('button', { name: 'Continue' })).not.toBeInTheDocument()
  })

  it('drops the person picker when the library has named nobody yet', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([])
    renderWelcome(account())

    // The administrator's greeting is still the point of the dialog…
    const modal = await dialog()
    expect(modal.getByRole('heading', { name: 'Hello' })).toBeInTheDocument()
    expect(modal.getByText('Welcome to the family archive.')).toBeInTheDocument()
    // …but there is nobody to be asked about, so there is no second step.
    expect(modal.queryByRole('button', { name: 'Continue' })).not.toBeInTheDocument()
    expect(modal.queryByRole('searchbox')).not.toBeInTheDocument()

    await user.click(modal.getByRole('button', { name: 'Done' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(seenMock).toHaveBeenCalled()
    expect(linkMock).not.toHaveBeenCalled()
  })

  it('asks who the reader is as soon as one person in the library is named', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([counted('su_a', 'Anna Nováková', 42)])
    renderWelcome(account())

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Continue' }))

    expect(await modal.findByRole('searchbox', { name: 'Find a person' })).toBeInTheDocument()
    expect(await modal.findByRole('button', { name: /Anna Nováková/ })).toBeInTheDocument()
  })

  it('does not open on a library with neither a greeting nor a named person', async () => {
    welcomeMock.mockResolvedValue('   \n  ')
    subjectsMock.mockResolvedValue([])
    renderWelcome(account())

    await waitFor(() => {
      expect(subjectsMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    // Nothing was shown, so nothing is recorded: the next visit asks again.
    expect(seenMock).not.toHaveBeenCalled()
  })

  it('reads the people afresh each time, never off an earlier visit', async () => {
    subjectsMock.mockResolvedValue([])
    const { unmount } = renderWelcome(account())
    const first = await dialog()
    expect(first.queryByRole('button', { name: 'Continue' })).not.toBeInTheDocument()
    unmount()

    // The instance has since named somebody; the next newcomer is asked.
    subjectsMock.mockResolvedValue([counted('su_a', 'Anna Nováková', 42)])
    renderWelcome(account())
    const second = await dialog()
    expect(second.getByRole('button', { name: 'Continue' })).toBeInTheDocument()
  })

  it('does not open at all when the greeting cannot be fetched, and records nothing', async () => {
    welcomeMock.mockRejectedValue(new Error('boom'))
    renderWelcome(account())

    await waitFor(() => {
      expect(welcomeMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(seenMock).not.toHaveBeenCalled()
  })

  it('links the account to the person the reader searches for and confirms', async () => {
    const user = userEvent.setup()
    const { refresh } = renderWelcome(account())

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Continue' }))
    await user.type(await modal.findByRole('searchbox', { name: 'Find a person' }), 'novak')

    // The face rides along with the name: the row carries the person's counts,
    // which is what tells two namesakes apart.
    const row = await modal.findByRole('button', { name: /Anna Nováková/ })
    expect(modal.queryByRole('button', { name: /Bob/ })).not.toBeInTheDocument()

    await user.click(row)
    expect(row).toHaveAttribute('aria-pressed', 'true')
    // Marking a row is not an answer.
    expect(linkMock).not.toHaveBeenCalled()

    await user.click(modal.getByRole('button', { name: 'Confirm' }))

    await waitFor(() => {
      expect(linkMock).toHaveBeenCalledWith('su_a')
    })
    expect(refresh).toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(seenMock).toHaveBeenCalled()
  })

  it('records the visit and closes when the reader skips, linking nobody', async () => {
    const user = userEvent.setup()
    renderWelcome(account())

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Skip' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(seenMock).toHaveBeenCalled()
    expect(linkMock).not.toHaveBeenCalled()
  })

  it('closes anyway when recording the visit fails', async () => {
    seenMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderWelcome(account())

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Skip' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('tells an already linked account who it is instead of asking again', async () => {
    const user = userEvent.setup()
    renderWelcome(account({ subject_uid: 'su_a' }))

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Continue' }))

    expect(await modal.findByText('Your account is linked to Anna Nováková.')).toBeInTheDocument()
    expect(modal.queryByRole('searchbox')).not.toBeInTheDocument()

    // …and still offers to change it, which is the picker one click later.
    await user.click(modal.getByRole('button', { name: 'Pick someone else' }))
    expect(await modal.findByRole('searchbox', { name: 'Find a person' })).toBeInTheDocument()
  })
})
