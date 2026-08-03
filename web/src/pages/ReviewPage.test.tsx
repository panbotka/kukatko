import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Label } from '../services/organize'
import { type Subject } from '../services/people'
import { type Photo } from '../services/photos'
import {
  REASON_NO_CANDIDATES,
  REASON_NO_LABELS,
  REASON_NO_PEOPLE,
  REASON_NO_SOURCES,
  type ReviewQuestion,
  type ReviewQueue,
  type ReviewSource,
} from '../services/review'

import { ReviewPage } from './ReviewPage'

vi.mock('../services/review', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/review')>()
  return { ...actual, fetchReviewQueue: vi.fn(), answerReview: vi.fn() }
})

vi.mock('../services/feedback', () => ({
  rejectFace: vi.fn(),
  unrejectFace: vi.fn(),
  rejectLabel: vi.fn(),
  unrejectLabel: vi.fn(),
}))

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, assignFace: vi.fn(), fetchFaces: vi.fn() }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, attachLabel: vi.fn(), detachLabel: vi.fn() }
})

const { fetchReviewQueue, answerReview } = await import('../services/review')
const { unrejectFace, unrejectLabel } = await import('../services/feedback')
const { assignFace } = await import('../services/people')
const queueMock = vi.mocked(fetchReviewQueue)
const answerMock = vi.mocked(answerReview)
const unrejectFaceMock = vi.mocked(unrejectFace)
const unrejectLabelMock = vi.mocked(unrejectLabel)
const assignFaceMock = vi.mocked(assignFace)

/** A photo with the display dimensions the stage needs to size its frame. */
function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    file_width: 1200,
    file_height: 800,
    file_orientation: 1,
    title: '',
  } as unknown as Photo
}

/** A subject the face questions are asked about. */
function subject(uid: string, name: string): Subject {
  return {
    uid,
    slug: uid,
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/** A label the label questions are asked about. */
function label(uid: string, name: string): Label {
  return {
    uid,
    slug: uid,
    name,
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/**
 * A face question. The tight bbox is deliberately off-centre and small so the
 * padding assertions have something to move.
 */
function faceQuestion(id: string, name = 'Tomáš Kozák'): ReviewQuestion {
  return {
    id,
    kind: 'face',
    confidence: 0.72,
    photo: photo(`p-${id}`),
    subject: subject(`s-${id}`, name),
    face_index: 0,
    bbox: { relative: [0.4, 0.3, 0.2, 0.2], pixel: [480, 240, 240, 160] },
    action: 'assign_person',
    marker_uid: `m-${id}`,
  }
}

/** A label question. */
function labelQuestion(id: string, name = 'Ostatky'): ReviewQuestion {
  return {
    id,
    kind: 'label',
    confidence: 0.65,
    photo: photo(`p-${id}`),
    label: label(`l-${id}`, name),
  }
}

/** Wraps questions in a queue response; the backend echoes the applied source. */
function makeQueue(questions: ReviewQuestion[], overrides: Partial<ReviewQueue> = {}): ReviewQueue {
  return {
    questions,
    source: 'both',
    answered: 0,
    remaining: questions.length,
    ...overrides,
  }
}

/** Reflects the current `source` query param so a test can assert the URL. */
function SourceProbe() {
  const [params] = useSearchParams()
  return <span data-testid="source-probe">{params.get('source') ?? ''}</span>
}

function renderPage(entry = '/review') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <SourceProbe />
        <ReviewPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  queueMock.mockReset()
  answerMock.mockReset().mockResolvedValue({ result: 'assigned', answered: 1, remaining: 0 })
  unrejectFaceMock.mockReset().mockResolvedValue(undefined)
  unrejectLabelMock.mockReset().mockResolvedValue(undefined)
  assignFaceMock.mockReset()
})

describe('ReviewPage', () => {
  it('asks a face question with the person name and a padded face box', async () => {
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1')]))
    renderPage()

    const question = await screen.findByTestId('review-question')
    expect(question).toHaveTextContent('Tomáš Kozák')

    // The drawn rectangle is the tight bbox grown by 30 % of its own size on
    // every side: a tight crop of a face is unjudgeable.
    const box = screen.getByTestId('review-bbox')
    expect(parseFloat(box.style.left)).toBeCloseTo(34)
    expect(parseFloat(box.style.top)).toBeCloseTo(24)
    expect(parseFloat(box.style.width)).toBeCloseTo(32)
    expect(parseFloat(box.style.height)).toBeCloseTo(32)
  })

  it('asks a label question with the label name and draws no face box', async () => {
    queueMock.mockResolvedValue(makeQueue([labelQuestion('q1')]))
    renderPage()

    const question = await screen.findByTestId('review-question')
    expect(question).toHaveTextContent('Ostatky')
    expect(screen.queryByTestId('review-bbox')).toBeNull()
  })

  it('sends yes / no / skip for → ← space and advances each time', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([
        faceQuestion('q1', 'Alice'),
        faceQuestion('q2', 'Bob'),
        faceQuestion('q3', 'Cyril'),
        faceQuestion('q4', 'Dana'),
      ]),
    )
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('{ArrowRight}')
    expect(answerMock).toHaveBeenLastCalledWith('q1', 'yes')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Bob')
    })

    await user.keyboard('{ArrowLeft}')
    expect(answerMock).toHaveBeenLastCalledWith('q2', 'no')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Cyril')
    })

    await user.keyboard(' ')
    expect(answerMock).toHaveBeenLastCalledWith('q3', 'skip')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Dana')
    })
  })

  it('keeps the next question in memory: answering within a batch fetches nothing', async () => {
    const user = userEvent.setup()
    // Six questions: two answers leave four, above the refill watermark, so any
    // fetch here would be one the player had to wait for.
    queueMock.mockResolvedValue(
      makeQueue([1, 2, 3, 4, 5, 6].map((n) => faceQuestion(`q${String(n)}`, `P${String(n)}`))),
    )
    renderPage()
    await screen.findByTestId('review-question')
    expect(queueMock).toHaveBeenCalledTimes(1)

    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('P2')
    })
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('P3')
    })

    // No spinner between cards, and no second batch request.
    expect(queueMock).toHaveBeenCalledTimes(1)
  })

  it('undoes a rejected face through the un-reject endpoint and restores the card', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice'), faceQuestion('q2', 'Bob')]))
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Bob')
    })

    await user.keyboard('z')
    await waitFor(() => {
      expect(unrejectFaceMock).toHaveBeenCalledWith({
        photo_uid: 'p-q1',
        face_index: 0,
        subject_uid: 's-q1',
      })
    })
    // The undone question is back on screen, and the counter went back with it.
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')
    })
    expect(screen.getByTestId('review-progress')).toHaveTextContent('0 answered')
  })

  it('undoes a confirmed face by unassigning the marker it assigned', async () => {
    const user = userEvent.setup()
    assignFaceMock.mockResolvedValue(undefined)
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice'), faceQuestion('q2', 'Bob')]))
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Bob')
    })

    await user.keyboard('z')
    await waitFor(() => {
      expect(assignFaceMock).toHaveBeenCalledWith('p-q1', {
        action: 'unassign_person',
        marker_uid: 'm-q1',
      })
    })
  })

  it('undoes a rejected label through the un-reject endpoint', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([labelQuestion('q1', 'Cats'), labelQuestion('q2', 'Dogs')]),
    )
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Dogs')
    })

    await user.keyboard('z')
    await waitFor(() => {
      expect(unrejectLabelMock).toHaveBeenCalledWith({ photo_uid: 'p-q1', label_uid: 'l-q1' })
    })
  })

  it('surfaces a failed answer without losing the player’s place', async () => {
    const user = userEvent.setup()
    answerMock.mockRejectedValue(new Error('offline'))
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice'), faceQuestion('q2', 'Bob')]))
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('{ArrowRight}')

    // The verdict is held for a retry...
    expect(await screen.findByTestId('review-answer-errors')).toBeInTheDocument()
    // ...and the flow moved on regardless: the failure never blocks the rhythm.
    expect(screen.getByTestId('review-question')).toHaveTextContent('Bob')
  })

  it('distinguishes an empty queue from a library with nothing to ask about', async () => {
    queueMock.mockResolvedValue(makeQueue([], { remaining: 0, reason: REASON_NO_SOURCES }))
    const { unmount } = renderPage()
    expect(await screen.findByTestId('review-empty-library')).toBeInTheDocument()
    expect(screen.queryByTestId('review-empty-queue')).toBeNull()
    unmount()

    queueMock.mockResolvedValue(makeQueue([], { remaining: 0, reason: REASON_NO_CANDIDATES }))
    renderPage()
    expect(await screen.findByTestId('review-empty-queue')).toBeInTheDocument()
    expect(screen.queryByTestId('review-empty-library')).toBeNull()
  })

  it('asks the backend for the source the URL names', async () => {
    queueMock.mockResolvedValue(makeQueue([labelQuestion('q1')], { source: 'labels' }))
    renderPage('/review?source=labels')

    await screen.findByTestId('review-question')
    expect(queueMock).toHaveBeenCalledWith('labels')
    // The toggle reflects what is being asked, so the state is never invisible.
    expect(screen.getByTestId('review-source-labels')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('review-source-both')).toHaveAttribute('aria-pressed', 'false')
  })

  it('rebuilds the game from the chosen source and puts the choice in the URL', async () => {
    const user = userEvent.setup()
    // The backend echoes the source it built for; the questions differ per
    // source, which is how the swap is visible on screen.
    queueMock.mockImplementation((source: ReviewSource = 'both') =>
      Promise.resolve(
        source === 'people'
          ? makeQueue([faceQuestion('f1', 'Alice')], { source })
          : makeQueue([labelQuestion('l1', 'Cats')], { source }),
      ),
    )
    renderPage()
    expect(await screen.findByTestId('review-question')).toHaveTextContent('Cats')

    await user.click(screen.getByTestId('review-source-people'))

    // The URL owns the choice ("back always works"), the queue was rebuilt from
    // it, and the label card the player asked to stop seeing is gone.
    await waitFor(() => {
      expect(screen.getByTestId('source-probe')).toHaveTextContent('people')
    })
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')
    })
    expect(queueMock).toHaveBeenLastCalledWith('people')
  })

  it('drops a batch that arrives after the source changed', async () => {
    const user = userEvent.setup()
    let resolveStale: ((queue: ReviewQueue) => void) | undefined
    queueMock
      .mockImplementationOnce(
        () =>
          new Promise<ReviewQueue>((resolve) => {
            resolveStale = resolve
          }),
      )
      .mockImplementation((source: ReviewSource = 'both') =>
        Promise.resolve(makeQueue([faceQuestion('f1', 'Alice')], { source })),
      )
    renderPage()

    await user.click(screen.getByTestId('review-source-people'))
    // The first fetch answers late, for the source nobody is playing any more.
    resolveStale?.(makeQueue([labelQuestion('l1', 'Cats')], { source: 'both' }))

    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')
    })
    expect(screen.getByTestId('review-question')).not.toHaveTextContent('Cats')
  })

  it('says which chosen source is empty and offers the other one', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([], { source: 'people', remaining: 0, reason: REASON_NO_PEOPLE }),
    )
    renderPage('/review?source=people')

    const empty = await screen.findByTestId('review-empty-source')
    expect(empty).toHaveTextContent('No people yet')
    expect(screen.queryByTestId('review-empty-library')).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Ask about labels' }))
    await waitFor(() => {
      expect(screen.getByTestId('source-probe')).toHaveTextContent('labels')
    })
  })

  it('offers both sources when the chosen one has run dry', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([], { source: 'labels', remaining: 0, reason: REASON_NO_CANDIDATES }),
    )
    renderPage('/review?source=labels')

    const empty = await screen.findByTestId('review-empty-queue')
    expect(empty).toHaveTextContent('Nothing left to sort in the chosen source.')

    await user.click(screen.getByRole('button', { name: 'Ask about both' }))
    await waitFor(() => {
      expect(screen.getByTestId('source-probe')).toHaveTextContent('both')
    })
  })

  it('keeps the plain empty-queue wording when both sources are in play', async () => {
    queueMock.mockResolvedValue(makeQueue([], { remaining: 0, reason: REASON_NO_CANDIDATES }))
    renderPage()

    // Nothing to switch to, so no scoped hint and no switch button either.
    const empty = await screen.findByTestId('review-empty-queue')
    expect(empty).toHaveTextContent('No questions right now.')
    expect(screen.queryByRole('button', { name: 'Ask about both' })).toBeNull()
  })

  it('shows the labels-are-empty state with a way to the other source', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([], { source: 'labels', remaining: 0, reason: REASON_NO_LABELS }),
    )
    renderPage('/review?source=labels')

    expect(await screen.findByTestId('review-empty-source')).toHaveTextContent('No labels yet')
    await user.click(screen.getByRole('button', { name: 'Ask about people' }))
    await waitFor(() => {
      expect(screen.getByTestId('source-probe')).toHaveTextContent('people')
    })
  })

  it('reports a failed queue fetch and retries on demand', async () => {
    const user = userEvent.setup()
    queueMock.mockRejectedValueOnce(new Error('offline'))
    renderPage()

    expect(await screen.findByTestId('review-load-error')).toBeInTheDocument()

    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice')]))
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')
    })
  })
})
