import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { FavoriteButton } from './FavoriteButton'

// Only the network call is faked; the hook's optimistic logic runs for real.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, favoritePhoto: vi.fn() }
})

const { favoritePhoto } = await import('../../services/photos')
const favoriteMock = vi.mocked(favoritePhoto)

function renderButton(favorite: boolean) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FavoriteButton uid="ph1" favorite={favorite} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  favoriteMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('FavoriteButton', () => {
  it('favorites optimistically and calls the API', async () => {
    favoriteMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderButton(false)

    const button = screen.getByRole('button', { name: 'Add to favorites' })
    expect(button).toHaveAttribute('aria-pressed', 'false')

    await user.click(button)

    // Optimistic flip is immediate (before the request resolves).
    expect(screen.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(favoriteMock).toHaveBeenCalledWith('ph1', true)
  })

  it('unfavorites and calls the API with DELETE semantics', async () => {
    favoriteMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderButton(true)

    await user.click(screen.getByRole('button', { name: 'Remove from favorites' }))

    expect(favoriteMock).toHaveBeenCalledWith('ph1', false)
  })

  it('rolls back the optimistic flip when the request fails', async () => {
    favoriteMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderButton(false)

    await user.click(screen.getByRole('button', { name: 'Add to favorites' }))

    // After the failed request settles, the state reverts to not-favorited.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Add to favorites' })).toHaveAttribute(
        'aria-pressed',
        'false',
      )
    })
    expect(favoriteMock).toHaveBeenCalledTimes(1)
  })
})
