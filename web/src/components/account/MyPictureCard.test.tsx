import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type PictureState } from '../../services/userpic'

import { MyPictureCard } from './MyPictureCard'

vi.mock('../../services/userpic', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/userpic')>()
  return {
    ...actual,
    fetchMyPicture: vi.fn(),
    uploadMyPicture: vi.fn(),
    pickMyPicture: vi.fn(),
    clearMyPicture: vi.fn(),
  }
})
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, searchPhotos: vi.fn() }
})

const { fetchMyPicture, uploadMyPicture, pickMyPicture, clearMyPicture } =
  await import('../../services/userpic')
const { searchPhotos } = await import('../../services/photos')
const fetchMyPictureMock = vi.mocked(fetchMyPicture)
const uploadMyPictureMock = vi.mocked(uploadMyPicture)
const pickMyPictureMock = vi.mocked(pickMyPicture)
const clearMyPictureMock = vi.mocked(clearMyPicture)
const searchPhotosMock = vi.mocked(searchPhotos)

/** A signed-in session, linked to a person of the library or not. */
function auth(subjectUid: string | null): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', subject_uid: subjectUid },
    role: 'viewer',
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderCard(subjectUid: string | null = null) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(subjectUid)}>
        <MyPictureCard />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The picture drawn in the card's preview, if the account has one. */
function preview(): HTMLElement | null {
  return screen.queryByRole('presentation', { hidden: true })
}

beforeEach(async () => {
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  fetchMyPictureMock.mockResolvedValue({ origin: 'none' })
  searchPhotosMock.mockResolvedValue({
    photos: [],
    total: 0,
    limit: 24,
    offset: 0,
    next_offset: null,
  })
})

describe('MyPictureCard', () => {
  it('says the account falls through to the coloured initial', async () => {
    renderCard()

    expect(await screen.findByText('Showing the first letter of your name.')).toBeInTheDocument()
    // Nothing to remove when nothing was set: the linked person's face and the
    // initial are not the user's to clear.
    expect(screen.queryByRole('button', { name: /Remove/ })).not.toBeInTheDocument()
    expect(preview()).toBeNull()
    expect(screen.getByText('U')).toBeInTheDocument()
  })

  it('shows the inherited face without anything being configured', async () => {
    fetchMyPictureMock.mockResolvedValue({ origin: 'subject', photo_uid: 'ph_1' })
    renderCard('sub_1')

    expect(
      await screen.findByText('Showing the face of the person you are linked to.'),
    ).toBeInTheDocument()
    expect(preview()?.getAttribute('src')).toBe('/api/v1/users/u1/avatar?v=0')
    expect(screen.queryByRole('button', { name: /Remove/ })).not.toBeInTheDocument()
  })

  it('uploads a picture and re-reads which source answers', async () => {
    const user = userEvent.setup()
    fetchMyPictureMock.mockResolvedValueOnce({ origin: 'none' })
    renderCard()
    await screen.findByText('Showing the first letter of your name.')

    const state: PictureState = { origin: 'upload' }
    fetchMyPictureMock.mockResolvedValue(state)
    uploadMyPictureMock.mockResolvedValue(undefined)
    const file = new File(['bytes'], 'me.jpg', { type: 'image/jpeg' })
    await user.upload(screen.getByLabelText('Upload a picture'), file)

    await waitFor(() => {
      expect(uploadMyPictureMock).toHaveBeenCalledWith(file)
    })
    expect(await screen.findByText('Showing the picture you uploaded.')).toBeInTheDocument()
    // The preview must not keep showing the old picture out of the ten-minute
    // cache, so its URL changes with every saved change.
    expect(preview()?.getAttribute('src')).toBe('/api/v1/users/u1/avatar?v=1')
  })

  it('offers to fall back to the linked face when clearing, and does it', async () => {
    const user = userEvent.setup()
    fetchMyPictureMock.mockResolvedValue({ origin: 'upload' })
    renderCard('sub_1')

    const clear = await screen.findByRole('button', {
      name: 'Remove it and go back to my face',
    })
    fetchMyPictureMock.mockResolvedValue({ origin: 'subject', photo_uid: 'ph_1' })
    clearMyPictureMock.mockResolvedValue(undefined)
    await user.click(clear)

    await waitFor(() => {
      expect(clearMyPictureMock).toHaveBeenCalled()
    })
    expect(
      await screen.findByText('Showing the face of the person you are linked to.'),
    ).toBeInTheDocument()
  })

  it('explains why a private photo was refused', async () => {
    const user = userEvent.setup()
    searchPhotosMock.mockResolvedValue({
      photos: [
        {
          uid: 'ph_private',
          title: 'A private one',
          file_name: 'p.jpg',
        } as unknown as Awaited<ReturnType<typeof searchPhotos>>['photos'][number],
      ],
      total: 1,
      limit: 24,
      offset: 0,
      next_offset: null,
    })
    pickMyPictureMock.mockRejectedValue(
      new ApiError(400, 'userpic: a private or hidden photo cannot be a profile picture'),
    )
    renderCard()
    await screen.findByText('Showing the first letter of your name.')

    await user.click(screen.getByRole('button', { name: /Pick from the library/ }))
    await user.click(await screen.findByRole('button', { name: 'A private one' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A private or hidden photo cannot be a profile picture',
    )
  })

  it('picks a photo from the library', async () => {
    const user = userEvent.setup()
    searchPhotosMock.mockResolvedValue({
      photos: [
        {
          uid: 'ph_1',
          title: 'The barn',
          file_name: 'barn.jpg',
        } as unknown as Awaited<ReturnType<typeof searchPhotos>>['photos'][number],
      ],
      total: 1,
      limit: 24,
      offset: 0,
      next_offset: null,
    })
    pickMyPictureMock.mockResolvedValue(undefined)
    renderCard()
    await screen.findByText('Showing the first letter of your name.')

    await user.click(screen.getByRole('button', { name: /Pick from the library/ }))
    fetchMyPictureMock.mockResolvedValue({ origin: 'photo', photo_uid: 'ph_1' })
    await user.click(await screen.findByRole('button', { name: 'The barn' }))

    await waitFor(() => {
      expect(pickMyPictureMock).toHaveBeenCalledWith('ph_1')
    })
    expect(
      await screen.findByText('Showing the photo you picked from the library.'),
    ).toBeInTheDocument()
  })
})
