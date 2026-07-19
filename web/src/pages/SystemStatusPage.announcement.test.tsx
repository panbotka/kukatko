import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'

import { SystemStatusPage } from './SystemStatusPage'

// The status snapshot is irrelevant to the announcement compose control (which
// renders regardless of the snapshot's state), so keep the page in its loading
// state with a never-resolving fetch.
vi.mock('../services/system', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/system')>()
  return {
    ...actual,
    fetchSystemStatus: vi.fn(() => new Promise(() => undefined)),
    requeueDeadLetterJobs: vi.fn(),
    triggerBackup: vi.fn(),
  }
})

// Mock the announcement service so the compose control's calls are observable.
vi.mock('../services/announcement', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/announcement')>()
  return {
    ...actual,
    fetchAnnouncement: vi.fn(),
    setAnnouncement: vi.fn(),
    clearAnnouncement: vi.fn(),
  }
})

const { fetchAnnouncement, setAnnouncement, clearAnnouncement } =
  await import('../services/announcement')
const fetchMock = vi.mocked(fetchAnnouncement)
const setMock = vi.mocked(setAnnouncement)
const clearMock = vi.mocked(clearAnnouncement)

// auth builds an AuthContext value; the compose control lives on the
// maintainer-gated system page.
function auth(isMaintainer: boolean): AuthContextValue {
  const role = isMaintainer ? 'maintainer' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    downloadToken: null,
    canWrite: isMaintainer,
    isAdmin: isMaintainer,
    isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

// renderPage renders the system page within auth + i18n + router providers.
function renderPage(isMaintainer = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isMaintainer)}>
        <MemoryRouter>
          <SystemStatusPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  setMock.mockReset()
  clearMock.mockReset()
  fetchMock.mockResolvedValue({ message: '' })
  setMock.mockResolvedValue({ message: 'Downtime', level: 'warning', updated_at: 't1' })
  clearMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SystemStatusPage announcement compose control', () => {
  it('publishes a message at the chosen level', async () => {
    const user = userEvent.setup()
    renderPage()

    const textarea = await screen.findByLabelText('Message')
    await user.type(textarea, 'Downtime')
    await user.selectOptions(screen.getByLabelText('Level'), 'warning')
    await user.click(screen.getByRole('button', { name: 'Publish' }))

    await waitFor(() => {
      expect(setMock).toHaveBeenCalledWith('Downtime', 'warning')
    })
    expect(await screen.findByText('Announcement published.')).toBeInTheDocument()
  })

  it('clears the announcement', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('Message')
    await user.click(screen.getByRole('button', { name: 'Clear announcement' }))

    await waitFor(() => {
      expect(clearMock).toHaveBeenCalledTimes(1)
    })
    expect(await screen.findByText('Announcement cleared.')).toBeInTheDocument()
  })

  it('is hidden for non-maintainers and never fetches the announcement', async () => {
    renderPage(false)
    expect(
      await screen.findByText('This page is available to system maintainers only.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Message')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
