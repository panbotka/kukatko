import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type RegenerateThumbnailResult } from '../../services/photos'

import { RegenerateThumbnailButton } from './RegenerateThumbnailButton'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, regenerateThumbnail: vi.fn() }
})

const { regenerateThumbnail } = await import('../../services/photos')
const regenerateThumbnailMock = vi.mocked(regenerateThumbnail)

function renderButton(onRegenerated = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <RegenerateThumbnailButton uid="p1" onRegenerated={onRegenerated} />
    </I18nextProvider>,
  )
  return onRegenerated
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

describe('RegenerateThumbnailButton', () => {
  it('calls the endpoint and refreshes the thumbnail on success', async () => {
    regenerateThumbnailMock.mockResolvedValue({ status: 'regenerated', sizes: ['tile_500'] })
    const user = userEvent.setup()
    const onRegenerated = renderButton()

    await user.click(screen.getByRole('button', { name: /regenerate thumbnail/i }))

    await waitFor(() => {
      expect(regenerateThumbnailMock).toHaveBeenCalledWith('p1')
    })
    expect(onRegenerated).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Thumbnail regenerated')).toBeInTheDocument()
  })

  it('shows a pending state while the request is in flight', async () => {
    let resolve: (value: RegenerateThumbnailResult) => void = () => undefined
    regenerateThumbnailMock.mockReturnValue(
      new Promise<RegenerateThumbnailResult>((r) => {
        resolve = r
      }),
    )
    const user = userEvent.setup()
    const onRegenerated = renderButton()

    await user.click(screen.getByRole('button', { name: /regenerate thumbnail/i }))
    // In flight: the button is disabled, shows the pending label and has not yet
    // asked the parent to refresh.
    expect(screen.getByRole('button', { name: /regenerating/i })).toBeDisabled()
    expect(onRegenerated).not.toHaveBeenCalled()

    resolve({ status: 'regenerated', sizes: [] })
    await waitFor(() => {
      expect(screen.getByText('Thumbnail regenerated')).toBeInTheDocument()
    })
  })

  it('surfaces a 422 (undecodable original) without refreshing', async () => {
    regenerateThumbnailMock.mockRejectedValue(new ApiError(422, 'undecodable'))
    const user = userEvent.setup()
    const onRegenerated = renderButton()

    await user.click(screen.getByRole('button', { name: /regenerate thumbnail/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent(
      'The original is missing or cannot be decoded',
    )
    expect(onRegenerated).not.toHaveBeenCalled()
  })

  it('shows a generic error for other failures', async () => {
    regenerateThumbnailMock.mockRejectedValue(new ApiError(500, 'boom'))
    const user = userEvent.setup()
    renderButton()

    await user.click(screen.getByRole('button', { name: /regenerate thumbnail/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Could not regenerate the thumbnail')
    })
  })
})
