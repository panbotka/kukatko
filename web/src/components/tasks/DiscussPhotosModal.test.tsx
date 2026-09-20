import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { PHOTO_UID_LENGTH } from '../../lib/photoRefs'
import { ApiError } from '../../services/auth'
import { type Comment } from '../../services/comments'
import { ToastProvider } from '../toast/ToastProvider'

import { DISCUSS_PREVIEW_MAX, DiscussPhotosModal } from './DiscussPhotosModal'

vi.mock('../../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/comments')>()
  return { ...actual, createComment: vi.fn() }
})

const { createComment } = await import('../../services/comments')
const createCommentMock = vi.mocked(createComment)

/** A uid of the catalogue's shape, numbered so a list of them is legible. */
function uid(n: number): string {
  return `ph${String(n).padStart(24, '0')}`
}

/** `count` distinct uids. */
function uids(count: number): string[] {
  return Array.from({ length: count }, (_, i) => uid(i + 1))
}

function posted(body: string): Comment {
  return {
    uid: 'cm1',
    task_uid: 'tk1',
    author_uid: 'u1',
    author_name: 'U',
    body,
    created_at: '2026-09-20T10:00:00Z',
  }
}

function renderModal(photoUids: string[], onPosted = vi.fn(), onClose = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <ToastProvider>
        <DiscussPhotosModal
          show
          taskUid="tk1"
          photoUids={photoUids}
          onClose={onClose}
          onPosted={onPosted}
        />
      </ToastProvider>
    </I18nextProvider>,
  )
  return { onPosted, onClose }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  createCommentMock.mockReset()
})

describe('DiscussPhotosModal', () => {
  it('previews the selection as small thumbnails and folds the rest into +N', () => {
    renderModal(uids(DISCUSS_PREVIEW_MAX + 5))

    const strip = screen.getByRole('list', { name: '29 photos' })
    const thumbs = within(strip).getAllByRole('presentation')
    expect(thumbs).toHaveLength(DISCUSS_PREVIEW_MAX)
    expect(thumbs[0]).toHaveAttribute('src', `/api/v1/photos/${uid(1)}/thumb/tile_100`)
    expect(within(strip).getByText('+5')).toBeInTheDocument()
  })

  it('posts one plain-text comment: the message, a blank line, one uid per line', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(posted('x'))
    const { onPosted } = renderModal([uid(1), uid(2)])

    await user.type(screen.getByLabelText('Message'), 'These two are wrong.')
    await user.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith(
        { kind: 'task', uid: 'tk1' },
        `These two are wrong.\n\n${uid(1)}\n${uid(2)}`,
      )
    })
    expect(onPosted).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('2 photos put into the discussion.')).toBeInTheDocument()
  })

  it('sends the uids alone when the message is left empty', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(posted('x'))
    renderModal([uid(7)])

    await user.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith({ kind: 'task', uid: 'tk1' }, uid(7))
    })
  })

  it('keeps the dialog and the message when the post fails, saying so inline', async () => {
    const user = userEvent.setup()
    createCommentMock.mockRejectedValue(new ApiError(500, 'boom'))
    const { onPosted, onClose } = renderModal([uid(1)])

    await user.type(screen.getByLabelText('Message'), 'Still here')
    await user.click(screen.getByRole('button', { name: 'Send' }))

    expect(
      await screen.findByText('The comment could not be posted. Please try again.'),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Message')).toHaveValue('Still here')
    expect(onPosted).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('refuses a selection that cannot fit in one comment, naming how many would', async () => {
    const user = userEvent.setup()
    // 2000 characters hold 74 uids; 80 do not.
    renderModal(uids(80))

    expect(
      screen.getByText(
        'Only 74 of the 80 selected photos fit in one comment. Pick fewer or shorten the message.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Send' }))
    expect(createCommentMock).not.toHaveBeenCalled()
  })

  it('charges the message against the same limit', async () => {
    const user = userEvent.setup()
    // 74 uids fit an empty message; 26 characters and the blank line push one out.
    renderModal(uids(74))
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled()

    await user.type(screen.getByLabelText('Message'), 'x'.repeat(PHOTO_UID_LENGTH))

    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
    expect(screen.getByText(/Only 73 of the 74 selected photos/)).toBeInTheDocument()
  })

  it('speaks Czech', async () => {
    await i18n.changeLanguage('cs')
    renderModal([uid(1)])

    expect(
      screen.getByRole('heading', { name: 'Vložit vybrané fotky do diskuse' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Odeslat' })).toBeInTheDocument()
  })
})
