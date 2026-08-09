import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type PhotoComment } from '../../services/comments'

import { CommentsPanel } from './CommentsPanel'

vi.mock('../../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/comments')>()
  return {
    ...actual,
    fetchComments: vi.fn(),
    createComment: vi.fn(),
    updateComment: vi.fn(),
    deleteComment: vi.fn(),
  }
})

const { fetchComments, createComment, updateComment, deleteComment } =
  await import('../../services/comments')
const fetchCommentsMock = vi.mocked(fetchComments)
const createCommentMock = vi.mocked(createComment)
const updateCommentMock = vi.mocked(updateComment)
const deleteCommentMock = vi.mocked(deleteComment)

function comment(overrides: Partial<PhotoComment> = {}): PhotoComment {
  return {
    uid: 'cm_1',
    photo_uid: 'ph_1',
    author_uid: 'usr_jarmila',
    author_name: 'Jarmila',
    body: 'This is the barn before it burned down.',
    created_at: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
    ...overrides,
  }
}

/**
 * Renders the thread. `currentUserUid` defaults to the author of the fixture
 * comment, so "my own comment" is the common case a test opts out of.
 */
function renderPanel(
  options: {
    currentUserUid?: string | null
    canModerate?: boolean
    onCountChange?: (count: number) => void
  } = {},
) {
  const { currentUserUid = 'usr_jarmila', canModerate = false, onCountChange } = options
  return render(
    <I18nextProvider i18n={i18n}>
      <CommentsPanel
        photoUid="ph_1"
        currentUserUid={currentUserUid}
        canModerate={canModerate}
        onCountChange={onCountChange}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchCommentsMock.mockResolvedValue([])
})

describe('CommentsPanel', () => {
  it('renders the thread with author, body and how long ago it was said', async () => {
    fetchCommentsMock.mockResolvedValue([
      comment(),
      comment({
        uid: 'cm_2',
        author_uid: 'usr_petr',
        author_name: 'Petr',
        body: 'That is uncle Josef on the left.',
        created_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
      }),
    ])
    renderPanel()

    const items = await screen.findAllByRole('listitem')
    expect(items).toHaveLength(2)
    // Oldest first — a conversation reads forwards.
    expect(within(items[0]).getByText('Jarmila')).toBeInTheDocument()
    expect(
      within(items[0]).getByText('This is the barn before it burned down.'),
    ).toBeInTheDocument()
    expect(within(items[0]).getByText('2h ago')).toBeInTheDocument()
    expect(within(items[1]).getByText('Petr')).toBeInTheDocument()
    expect(within(items[1]).getByText('5m ago')).toBeInTheDocument()
    // The heading counts the thread, pluralised.
    expect(screen.getByText('2 comments')).toBeInTheDocument()
  })

  it('marks an edited comment as edited', async () => {
    fetchCommentsMock.mockResolvedValue([comment({ edited_at: new Date().toISOString() })])
    renderPanel()

    expect(await screen.findByText('edited')).toBeInTheDocument()
  })

  it('invites the first comment when the thread is empty', async () => {
    renderPanel()

    expect(await screen.findByText('Say what you know about this photo…')).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
    // No count in the heading when there is nothing to count.
    expect(screen.getByText('Comments')).toBeInTheDocument()
  })

  it('posts a comment and appends it to the thread', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(
      comment({ uid: 'cm_new', body: 'Summer of 1968.', created_at: new Date().toISOString() }),
    )
    renderPanel()
    await screen.findByText('Say what you know about this photo…')

    await user.type(screen.getByLabelText('New comment'), 'Summer of 1968.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith('ph_1', 'Summer of 1968.')
    })
    expect(await screen.findByText('Summer of 1968.')).toBeInTheDocument()
    // The composer empties itself, ready for the next remark.
    expect(screen.getByLabelText('New comment')).toHaveValue('')
  })

  it('sends on Enter and breaks the line on Shift+Enter', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(comment({ uid: 'cm_new', body: 'one\ntwo' }))
    renderPanel()
    await screen.findByText('Say what you know about this photo…')

    const input = screen.getByLabelText('New comment')
    await user.click(input)
    await user.keyboard('one{Shift>}{Enter}{/Shift}two')
    expect(input).toHaveValue('one\ntwo')
    expect(createCommentMock).not.toHaveBeenCalled()

    await user.keyboard('{Enter}')
    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith('ph_1', 'one\ntwo')
    })
  })

  it('lets a read-only viewer post — the deliberate exception to the rule', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(comment({ uid: 'cm_new', body: 'I remember this day.' }))
    // A viewer: signed in, may not curate anything, still part of the conversation.
    renderPanel({ currentUserUid: 'usr_viewer', canModerate: false })
    await screen.findByText('Say what you know about this photo…')

    const input = screen.getByLabelText('New comment')
    expect(input).toBeEnabled()
    await user.type(input, 'I remember this day.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith('ph_1', 'I remember this day.')
    })
  })

  it('edits the reader’s own comment in place', async () => {
    const user = userEvent.setup()
    fetchCommentsMock.mockResolvedValue([comment()])
    updateCommentMock.mockResolvedValue(
      comment({ body: 'The barn, a year before the fire.', edited_at: new Date().toISOString() }),
    )
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    const editor = screen.getByLabelText('Edit comment')
    await user.clear(editor)
    await user.type(editor, 'The barn, a year before the fire.')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateCommentMock).toHaveBeenCalledWith(
        'ph_1',
        'cm_1',
        'The barn, a year before the fire.',
      )
    })
    expect(await screen.findByText('The barn, a year before the fire.')).toBeInTheDocument()
    expect(screen.getByText('edited')).toBeInTheDocument()
    expect(screen.queryByLabelText('Edit comment')).not.toBeInTheDocument()
  })

  it('offers no edit or delete on someone else’s comment', async () => {
    fetchCommentsMock.mockResolvedValue([comment()])
    renderPanel({ currentUserUid: 'usr_petr' })

    await screen.findByText('This is the barn before it burned down.')
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  })

  it('deletes only after the reader confirms', async () => {
    const user = userEvent.setup()
    fetchCommentsMock.mockResolvedValue([comment()])
    deleteCommentMock.mockResolvedValue(undefined)
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    // The dialog asks first; nothing has been deleted yet.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Delete this comment?')).toBeInTheDocument()
    expect(deleteCommentMock).not.toHaveBeenCalled()

    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => {
      expect(deleteCommentMock).toHaveBeenCalledWith('ph_1', 'cm_1')
    })
    await waitFor(() => {
      expect(screen.queryByText('This is the barn before it burned down.')).not.toBeInTheDocument()
    })
  })

  it('keeps the comment when the confirmation is dismissed', async () => {
    const user = userEvent.setup()
    fetchCommentsMock.mockResolvedValue([comment()])
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(deleteCommentMock).not.toHaveBeenCalled()
    expect(screen.getByText('This is the barn before it burned down.')).toBeInTheDocument()
  })

  it('lets an admin remove anyone’s comment but never rewrite it', async () => {
    fetchCommentsMock.mockResolvedValue([comment()])
    renderPanel({ currentUserUid: 'usr_admin', canModerate: true })

    await screen.findByText('This is the barn before it burned down.')
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
  })

  it('reports the thread length upwards, and again after a post', async () => {
    const user = userEvent.setup()
    const onCountChange = vi.fn()
    fetchCommentsMock.mockResolvedValue([comment()])
    createCommentMock.mockResolvedValue(comment({ uid: 'cm_2', body: 'And that is the well.' }))
    renderPanel({ onCountChange })

    await waitFor(() => {
      expect(onCountChange).toHaveBeenCalledWith(1)
    })

    await user.type(screen.getByLabelText('New comment'), 'And that is the well.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(onCountChange).toHaveBeenLastCalledWith(2)
    })
    expect(screen.getByText('2 comments')).toBeInTheDocument()
  })

  it('says so when the thread cannot be loaded', async () => {
    fetchCommentsMock.mockRejectedValue(new ApiError(500, 'boom'))
    renderPanel()

    expect(await screen.findByText('The comments could not be loaded.')).toBeInTheDocument()
  })

  it('tells the reader to slow down when the rate limit answers', async () => {
    const user = userEvent.setup()
    createCommentMock.mockRejectedValue(new ApiError(429, 'too many'))
    renderPanel()
    await screen.findByText('Say what you know about this photo…')

    await user.type(screen.getByLabelText('New comment'), 'Again!')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(
      await screen.findByText('Steady on — comments are coming too fast. Try again in a moment.'),
    ).toBeInTheDocument()
    // The text stays in the box, so nothing anyone typed is thrown away.
    expect(screen.getByLabelText('New comment')).toHaveValue('Again!')
  })

  it('speaks Czech, with the plural the count needs', async () => {
    await i18n.changeLanguage('cs')
    fetchCommentsMock.mockResolvedValue([
      comment(),
      comment({ uid: 'cm_2' }),
      comment({ uid: 'cm_3' }),
      comment({ uid: 'cm_4' }),
      comment({ uid: 'cm_5' }),
    ])
    renderPanel()

    expect(await screen.findByText('5 komentářů')).toBeInTheDocument()
    expect(screen.getByLabelText('Nový komentář')).toBeInTheDocument()
  })

  it('names the empty thread’s invitation in Czech', async () => {
    await i18n.changeLanguage('cs')
    renderPanel()

    expect(await screen.findByText('Napiš, co o téhle fotce víš…')).toBeInTheDocument()
  })
})
