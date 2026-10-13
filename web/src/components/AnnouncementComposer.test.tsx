import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { AnnouncementComposer } from './AnnouncementComposer'

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

// renderComposer renders the compose control within the i18n provider.
function renderComposer() {
  return render(
    <I18nextProvider i18n={i18n}>
      <AnnouncementComposer />
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

describe('AnnouncementComposer', () => {
  it('publishes a message at the chosen level', async () => {
    const user = userEvent.setup()
    renderComposer()

    const textarea = await screen.findByLabelText('Message')
    await user.type(textarea, 'Downtime')
    await user.selectOptions(screen.getByLabelText('Level'), 'warning')
    await user.click(screen.getByRole('button', { name: 'Publish' }))

    await waitFor(() => {
      expect(setMock).toHaveBeenCalledWith('Downtime', 'warning')
    })
    expect(await screen.findByText('Announcement published.')).toBeInTheDocument()
  })

  it("prefills the project's current message", async () => {
    fetchMock.mockResolvedValue({ message: 'Odstávka', level: 'warning', updated_at: 't0' })
    renderComposer()

    expect(await screen.findByDisplayValue('Odstávka')).toBeInTheDocument()
    expect(screen.getByLabelText('Level')).toHaveValue('warning')
  })

  it('clears the announcement', async () => {
    const user = userEvent.setup()
    renderComposer()

    await screen.findByLabelText('Message')
    await user.click(screen.getByRole('button', { name: 'Clear announcement' }))

    await waitFor(() => {
      expect(clearMock).toHaveBeenCalledTimes(1)
    })
    expect(await screen.findByText('Announcement cleared.')).toBeInTheDocument()
  })
})
