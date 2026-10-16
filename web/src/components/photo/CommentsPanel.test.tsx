import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type Comment } from '../../services/comments'

import { CommentsPanel } from './CommentsPanel'

/** The thread under test hangs off one photograph; the panel is subject-agnostic. */
const PHOTO_SUBJECT = { kind: 'photo', uid: 'ph_1' } as const

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

function comment(overrides: Partial<Comment> = {}): Comment {
  return {
    uid: 'cm_1',
    photo_uid: 'ph_1',
    author_uid: 'usr_jarmila',
    author_name: 'Jarmila',
    body: 'This is the barn before it burned down.',
    mentions: [],
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
  // A router, because a photo uid in a body renders as a `Link`.
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <CommentsPanel
          subject={PHOTO_SUBJECT}
          currentUserUid={currentUserUid}
          canModerate={canModerate}
          onCountChange={onCountChange}
        />
      </MemoryRouter>
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

  it('shows the author’s own picture, and the initial for an authorless comment', async () => {
    fetchCommentsMock.mockResolvedValue([
      // The thread asks one endpoint for the author's account; whether that
      // resolves to an upload, a picked photo or the linked person's face is the
      // server's business and not visible here.
      comment(),
      // A comment whose account was deleted keeps its place but names nobody, so
      // it goes straight to the coloured initial without firing a request.
      comment({ uid: 'cm_2', author_uid: '', author_name: 'Petr' }),
    ])
    renderPanel()

    const items = await screen.findAllByRole('listitem')
    const face = within(items[0]).getByRole('presentation', { hidden: true })
    expect(face.getAttribute('src')).toBe('/api/v1/p/v/users/usr_jarmila/avatar')
    expect(within(items[1]).queryByRole('presentation', { hidden: true })).not.toBeInTheDocument()
    expect(within(items[1]).getByText('P')).toBeInTheDocument()
  })

  it('shows an author and a mention who left the project as former members', async () => {
    fetchCommentsMock.mockResolvedValue([
      comment({
        author_uid: 'usr_gone',
        author_name: '',
        author_former: true,
        body: 'Ask @[Petr](user:usr_petr) about it.',
        mentions: [{ user_uid: 'usr_petr', name: '', former: true, subject_uid: null }],
      }),
    ])
    renderPanel({ currentUserUid: 'usr_other' })

    const [item] = await screen.findAllByRole('listitem')
    // The author: the label instead of a name, and no picture asked for.
    expect(within(item).getAllByText('Former project member')).toHaveLength(2)
    expect(within(item).queryByRole('presentation', { hidden: true })).not.toBeInTheDocument()
    // The mention: the label, muted, linking nowhere — not the token's name.
    const chip = item.querySelector('[data-mention-uid="usr_petr"]')
    expect(chip).toHaveTextContent('Former project member')
    expect(chip).toHaveClass('kk-mention--gone')
    expect(chip?.tagName).toBe('SPAN')
    expect(within(item).queryByText('Petr')).not.toBeInTheDocument()
  })

  it('marks an edited comment as edited', async () => {
    fetchCommentsMock.mockResolvedValue([comment({ edited_at: new Date().toISOString() })])
    renderPanel()

    expect(await screen.findByText('edited')).toBeInTheDocument()
  })

  it('invites the first comment when the thread is empty', async () => {
    renderPanel()

    expect(await screen.findByText('Say what you know about this one…')).toBeInTheDocument()
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
    await screen.findByText('Say what you know about this one…')

    await user.type(screen.getByLabelText('New comment'), 'Summer of 1968.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith(PHOTO_SUBJECT, 'Summer of 1968.')
    })
    expect(await screen.findByText('Summer of 1968.')).toBeInTheDocument()
    // The composer empties itself, ready for the next remark.
    expect(screen.getByLabelText('New comment')).toHaveValue('')
  })

  it('sends on Enter and breaks the line on Shift+Enter', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(comment({ uid: 'cm_new', body: 'one\ntwo' }))
    renderPanel()
    await screen.findByText('Say what you know about this one…')

    const input = screen.getByLabelText('New comment')
    await user.click(input)
    await user.keyboard('one{Shift>}{Enter}{/Shift}two')
    expect(input).toHaveValue('one\ntwo')
    expect(createCommentMock).not.toHaveBeenCalled()

    await user.keyboard('{Enter}')
    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith(PHOTO_SUBJECT, 'one\ntwo')
    })
  })

  it('lets a read-only viewer post — the deliberate exception to the rule', async () => {
    const user = userEvent.setup()
    createCommentMock.mockResolvedValue(comment({ uid: 'cm_new', body: 'I remember this day.' }))
    // A viewer: signed in, may not curate anything, still part of the conversation.
    renderPanel({ currentUserUid: 'usr_viewer', canModerate: false })
    await screen.findByText('Say what you know about this one…')

    const input = screen.getByLabelText('New comment')
    expect(input).toBeEnabled()
    await user.type(input, 'I remember this day.')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    await waitFor(() => {
      expect(createCommentMock).toHaveBeenCalledWith(PHOTO_SUBJECT, 'I remember this day.')
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
        PHOTO_SUBJECT,
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
      expect(deleteCommentMock).toHaveBeenCalledWith(PHOTO_SUBJECT, 'cm_1')
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

  it('draws the row’s two actions as a matching pair of quiet icon buttons', async () => {
    fetchCommentsMock.mockResolvedValue([comment()])
    renderPanel()

    // A pencil and a bin, named for assistive tech and titled for the mouse —
    // no caption word and no outlined red box under every comment.
    const edit = await screen.findByRole('button', { name: 'Edit' })
    const remove = screen.getByRole('button', { name: 'Delete' })
    expect(edit).toHaveAttribute('title', 'Edit')
    expect(remove).toHaveAttribute('title', 'Delete')
    expect(edit).toHaveTextContent('')
    expect(remove).toHaveTextContent('')
    expect(edit.querySelector('.bi-pencil')).not.toBeNull()
    expect(remove.querySelector('.bi-trash')).not.toBeNull()

    // Same size and style: both the muted link variant, delete told apart only
    // by the danger modifier the stylesheet colours on hover/focus.
    for (const button of [edit, remove]) {
      expect(button).toHaveClass('btn-link', 'btn-sm', 'kk-comment__action')
      expect(button).not.toHaveClass('btn-outline-danger')
      // Both square off at 44px on touch.
      expect(button).toHaveClass('kukatko-tap-target-touch')
    }
    expect(remove).toHaveClass('kk-comment__action--danger')
    expect(edit).not.toHaveClass('kk-comment__action--danger')
  })

  it('hides the row’s actions while the comment is being edited', async () => {
    const user = userEvent.setup()
    fetchCommentsMock.mockResolvedValue([comment()])
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
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
    await screen.findByText('Say what you know about this one…')

    await user.type(screen.getByLabelText('New comment'), 'Again!')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))

    expect(
      await screen.findByText('Steady on — comments are coming too fast. Try again in a moment.'),
    ).toBeInTheDocument()
    // The text stays in the box, so nothing anyone typed is thrown away.
    expect(screen.getByLabelText('New comment')).toHaveValue('Again!')
  })

  it('renders a photo uid in a task thread as a link that stays in the task', async () => {
    const ref = 'ph000000000000000000000001'
    fetchCommentsMock.mockResolvedValue([
      comment({ photo_uid: undefined, task_uid: 'tk1', body: `Wrong one: ${ref}` }),
    ])
    renderPanel()

    const link = await screen.findByRole('link', { name: ref })
    expect(link).toHaveAttribute('href', `/p/v/photos/${ref}?task=tk1`)
    // And the thumbnail under it leads to the same place.
    expect(screen.getByRole('link', { name: `Photo ${ref}` })).toHaveAttribute(
      'href',
      `/p/v/photos/${ref}?task=tk1`,
    )
  })

  it.each([
    {
      // A phone's info sheet: the heading sits right above the linked comment,
      // and centring the comment alone (1600 - (300 - 80) / 2 = 1490) would
      // leave the heading cut off at the sheet's top edge. Heading to comment
      // (1500..1680) fits, so that stretch is centred: 1500 - (300 - 180) / 2.
      name: 'with its heading when both fit',
      clientHeight: 300,
      headingTop: 1500,
      want: 1440,
      headingVisible: true,
    },
    {
      // The heading is far above (a long thread): the comment alone is centred
      // in the 400px body — 1600 - (400 - 80) / 2.
      name: 'alone when the heading is too far above',
      clientHeight: 400,
      headingTop: 100,
      want: 1440,
      headingVisible: false,
    },
  ])(
    'reveals a linked comment $name, scrolling only the drawer body, never the viewer',
    async ({ clientHeight, headingTop, want, headingVisible }) => {
      // The photo viewer in miniature: a fixed, clipping root, and the drawer body
      // inside it as the one scroller the thread lives in.
      const root = document.createElement('div')
      root.style.position = 'fixed'
      root.style.overflow = 'hidden'
      const body = document.createElement('div')
      body.style.overflowY = 'auto'
      root.append(body)
      document.body.append(root)
      Object.defineProperty(body, 'clientHeight', { configurable: true, value: clientHeight })
      Object.defineProperty(body, 'scrollHeight', { configurable: true, value: 3000 })
      // jsdom has no layout: the linked row sits far down the thread, and the
      // thread's heading somewhere above it. Content positions are measured
      // against an unscrolled body, as getBoundingClientRect would be at rest.
      const rect = vi
        .spyOn(HTMLElement.prototype, 'getBoundingClientRect')
        .mockImplementation(function (this: HTMLElement) {
          if (this.id === 'comment-cm_2') {
            return new DOMRect(0, 1600, 300, 80)
          }
          if (this.classList.contains('kk-text-eyebrow')) {
            return new DOMRect(0, headingTop, 300, 20)
          }
          return new DOMRect()
        })
      const scrollIntoView = vi.spyOn(Element.prototype, 'scrollIntoView')
      window.history.replaceState(null, '', '#comment-cm_2')
      try {
        fetchCommentsMock.mockResolvedValue([comment(), comment({ uid: 'cm_2' })])
        render(
          <I18nextProvider i18n={i18n}>
            <MemoryRouter>
              <CommentsPanel
                subject={PHOTO_SUBJECT}
                currentUserUid="usr_jarmila"
                canModerate={false}
              />
            </MemoryRouter>
          </I18nextProvider>,
          { container: body },
        )

        await waitFor(() => {
          expect(body.scrollTop).toBe(want)
        })
        const shown = (top: number, height: number): boolean =>
          top >= body.scrollTop && top + height <= body.scrollTop + clientHeight
        expect(shown(1600, 80)).toBe(true)
        expect(shown(headingTop, 20)).toBe(headingVisible)
        expect(root.scrollTop).toBe(0)
        expect(document.documentElement.scrollTop).toBe(0)
        // `scrollIntoView` scrolls every ancestor, the viewer root included.
        expect(scrollIntoView).not.toHaveBeenCalled()
      } finally {
        window.history.replaceState(null, '', window.location.pathname)
        rect.mockRestore()
        scrollIntoView.mockRestore()
        root.remove()
      }
    },
  )

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

    expect(await screen.findByText('Napište, co o tomhle víte…')).toBeInTheDocument()
  })
})
