import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'

import { DownloadZipButton, type DownloadZipButtonProps } from './DownloadZipButton'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, downloadPhotosZip: vi.fn() }
})

const { downloadPhotosZip } = await import('../../services/photos')
const zipMock = vi.mocked(downloadPhotosZip)

function renderButton(props: Partial<DownloadZipButtonProps> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <DownloadZipButton {...props} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  zipMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DownloadZipButton', () => {
  it('downloads the current selection with its UIDs', async () => {
    zipMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderButton({ photoUids: ['ph1', 'ph2'] })

    await user.click(screen.getByRole('button', { name: /Download ZIP/ }))

    await waitFor(() => {
      expect(zipMock).toHaveBeenCalledTimes(1)
    })
    expect(zipMock).toHaveBeenCalledWith({
      photoUids: ['ph1', 'ph2'],
      albumUid: undefined,
      name: undefined,
    })
  })

  it('downloads a whole album by UID and name', async () => {
    zipMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderButton({ albumUid: 'alb1', name: 'Trip' })

    await user.click(screen.getByRole('button', { name: /Download ZIP/ }))

    await waitFor(() => {
      expect(zipMock).toHaveBeenCalledWith({
        photoUids: undefined,
        albumUid: 'alb1',
        name: 'Trip',
      })
    })
  })

  it('is disabled when there is nothing to download', () => {
    renderButton({ photoUids: [] })
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('shows a pending state while the archive streams', async () => {
    let release!: () => void
    zipMock.mockReturnValue(
      new Promise<void>((resolve) => {
        release = resolve
      }),
    )
    const user = userEvent.setup()
    renderButton({ photoUids: ['ph1'] })

    await user.click(screen.getByRole('button'))

    expect(screen.getByText(/Preparing ZIP/)).toBeInTheDocument()
    expect(screen.getByRole('button')).toBeDisabled()

    release()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Download ZIP/ })).toBeEnabled()
    })
  })

  it('surfaces the over-cap error on a 413', async () => {
    zipMock.mockRejectedValue(new ApiError(413, 'too many photos'))
    const user = userEvent.setup()
    renderButton({ photoUids: ['ph1'] })

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/Too many photos/)
    })
  })

  it('surfaces a generic error otherwise', async () => {
    zipMock.mockRejectedValue(new ApiError(500, 'boom'))
    const user = userEvent.setup()
    renderButton({ photoUids: ['ph1'] })

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/could not be prepared/)
    })
  })
})
