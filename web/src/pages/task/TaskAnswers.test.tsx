import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { createComment } from '../../services/comments'
import { REVIEW_APPROVE, REVIEW_RETURN, type Task } from '../../services/tasks'

import { TaskAnswers } from './TaskAnswers'

vi.mock('../../services/comments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/comments')>()
  return { ...actual, createComment: vi.fn() }
})

const createCommentMock = vi.mocked(createComment)
const subject = { kind: 'task', uid: 'tk1' } as const

function renderAnswers(
  task: Pick<Task, 'uid' | 'state' | 'options'>,
  canHandOver: boolean,
  callbacks = { onAnswered: vi.fn(), onHandedOver: vi.fn() },
) {
  render(
    <I18nextProvider i18n={i18n}>
      <TaskAnswers
        task={task}
        chosen={null}
        onAnswered={callbacks.onAnswered}
        canHandOver={canHandOver}
        onHandedOver={callbacks.onHandedOver}
      />
    </I18nextProvider>,
  )
  return callbacks
}

const review = { uid: 'tk1', state: 'review', options: [] } as const

beforeEach(async () => {
  await i18n.changeLanguage('en')
  createCommentMock.mockReset()
  createCommentMock.mockResolvedValue({} as Awaited<ReturnType<typeof createComment>>)
})

describe('TaskAnswers', () => {
  it.each([
    ['Approve', REVIEW_APPROVE],
    ['Send back', REVIEW_RETURN],
  ])(
    "hands the task to the agent with a writer's %s, posting the fixed string",
    async (label, body) => {
      const user = userEvent.setup()
      const { onAnswered, onHandedOver } = renderAnswers({ ...review, options: [] }, true)

      expect(screen.getByText('Answering also hands the task to the agent.')).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: label }))

      await waitFor(() => {
        expect(onHandedOver).toHaveBeenCalledTimes(1)
      })
      // One request: the comment and the move land together.
      expect(createCommentMock).toHaveBeenCalledTimes(1)
      expect(createCommentMock).toHaveBeenCalledWith(subject, body, { state: 'working' })
      expect(onAnswered).toHaveBeenCalledTimes(1)
    },
  )

  it("keeps a viewer's built-in answer a plain comment", async () => {
    const user = userEvent.setup()
    const { onAnswered, onHandedOver } = renderAnswers({ ...review, options: [] }, false)

    expect(
      screen.queryByText('Answering also hands the task to the agent.'),
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Approve' }))

    await waitFor(() => {
      expect(onAnswered).toHaveBeenCalledTimes(1)
    })
    // No state at all: a viewer sending one would get a 403.
    expect(createCommentMock).toHaveBeenCalledWith(subject, REVIEW_APPROVE)
    expect(createCommentMock.mock.calls[0]).toHaveLength(2)
    expect(onHandedOver).not.toHaveBeenCalled()
  })

  it("never moves the task with the task's own option, even for a writer", async () => {
    const user = userEvent.setup()
    const { onAnswered, onHandedOver } = renderAnswers(
      { uid: 'tk1', state: 'review', options: ['1936', '1938'] },
      true,
    )

    expect(
      screen.queryByText('Answering also hands the task to the agent.'),
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '1938' }))

    await waitFor(() => {
      expect(onAnswered).toHaveBeenCalledTimes(1)
    })
    expect(createCommentMock).toHaveBeenCalledWith(subject, '1938')
    expect(createCommentMock.mock.calls[0]).toHaveLength(2)
    expect(onHandedOver).not.toHaveBeenCalled()
  })

  it('does not refresh the task when the answer failed', async () => {
    const user = userEvent.setup()
    createCommentMock.mockRejectedValue(new Error('boom'))
    const { onAnswered, onHandedOver } = renderAnswers({ ...review, options: [] }, true)

    await user.click(screen.getByRole('button', { name: 'Approve' }))

    expect(await screen.findByText(/The answer could not be posted/)).toBeInTheDocument()
    expect(onAnswered).not.toHaveBeenCalled()
    expect(onHandedOver).not.toHaveBeenCalled()
  })
})
