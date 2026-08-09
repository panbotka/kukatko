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
import { declarations, readCss, ruleBody } from '../test/css'
import { frameRatio, loadImageAs } from '../test/imageFrame'

import { ReviewPage } from './ReviewPage'

vi.mock('../services/review', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/review')>()
  return { ...actual, fetchReviewQueue: vi.fn(), answerReview: vi.fn() }
})

vi.mock('../services/feedback', () => ({
  rejectFace: vi.fn(),
  unrejectFace: vi.fn(),
  confirmFace: vi.fn(),
  unconfirmFace: vi.fn(),
  rejectLabel: vi.fn(),
  unrejectLabel: vi.fn(),
  confirmDuplicate: vi.fn(),
  unconfirmDuplicate: vi.fn(),
  dismissDuplicate: vi.fn(),
  undismissDuplicate: vi.fn(),
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
const { unrejectFace, unrejectLabel, unconfirmFace, unconfirmDuplicate } =
  await import('../services/feedback')
const { assignFace } = await import('../services/people')
const queueMock = vi.mocked(fetchReviewQueue)
const answerMock = vi.mocked(answerReview)
const unrejectFaceMock = vi.mocked(unrejectFace)
const unrejectLabelMock = vi.mocked(unrejectLabel)
const unconfirmFaceMock = vi.mocked(unconfirmFace)
const unconfirmDuplicateMock = vi.mocked(unconfirmDuplicate)
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
    birth_year: null,
    death_year: null,
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

/** A place question over a location the geo-estimator guessed. */
function placeQuestion(id: string, name = 'Brno'): ReviewQuestion {
  return {
    id,
    kind: 'place',
    // The estimator either found coherent neighbours or refused, so there is no
    // confidence behind the guess — the page must not show one.
    confidence: 0,
    photo: photo(`p-${id}`),
    place: { name, country: 'cz', city: name, lat: 49.2, lng: 16.6 },
  }
}

/** A duplicate question over a near-identical pair. */
function duplicateQuestion(id: string): ReviewQuestion {
  return {
    id,
    kind: 'duplicate',
    confidence: 0.97,
    photo: photo(`p-${id}-a`),
    other: photo(`p-${id}-b`),
    group_id: `g-${id}`,
  }
}

/** An outlier question over a face assigned to somebody it may not be. */
function outlierQuestion(id: string, name = 'Tomáš Kozák'): ReviewQuestion {
  return {
    id,
    kind: 'outlier',
    confidence: 0.12,
    distance: 0.88,
    photo: photo(`p-${id}`),
    subject: subject(`s-${id}`, name),
    face_index: 0,
    bbox: { relative: [0.4, 0.3, 0.2, 0.2], pixel: [480, 240, 240, 160] },
    marker_uid: `m-${id}`,
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
  unconfirmFaceMock.mockReset().mockResolvedValue(undefined)
  unconfirmDuplicateMock.mockReset().mockResolvedValue(undefined)
  assignFaceMock.mockReset()
})

describe('ReviewPage', () => {
  it('asks a face question with the person name and a padded face box', async () => {
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1')]))
    renderPage()

    const question = await screen.findByTestId('review-question')
    expect(question).toHaveTextContent('Tomáš Kozák')

    // The rectangle is placed in percentages of the stage, so it waits until the
    // preview has reported the frame it actually rendered at.
    expect(screen.queryByTestId('review-bbox')).toBeNull()
    loadImageAs(screen.getByAltText('Photo under review'), 1200, 800)

    // The drawn rectangle is the tight bbox grown by 30 % of its own size on
    // every side: a tight crop of a face is unjudgeable.
    const box = screen.getByTestId('review-bbox')
    expect(parseFloat(box.style.left)).toBeCloseTo(34)
    expect(parseFloat(box.style.top)).toBeCloseTo(24)
    expect(parseFloat(box.style.width)).toBeCloseTo(32)
    expect(parseFloat(box.style.height)).toBeCloseTo(32)
  })

  it('sizes the stage from the loaded preview rather than from a transposed row', async () => {
    // A row whose dimensions were stored already-oriented and then rotated a
    // second time: 3000x4000 + orientation 6 reads as a landscape 4000x3000, while
    // the file (and its preview) is portrait. The stage must follow the preview.
    const question = faceQuestion('q1')
    queueMock.mockResolvedValue(
      makeQueue([
        {
          ...question,
          photo: { ...question.photo, file_width: 3000, file_height: 4000, file_orientation: 6 },
        },
      ]),
    )
    const { container } = renderPage()
    await screen.findByTestId('review-question')

    const stage = container.querySelector<HTMLElement>('.review-photo')
    // Until the preview loads the row is the estimate — it keeps the stage from
    // resizing under the question — and no rectangle is drawn against it.
    expect(frameRatio(stage)).toBeCloseTo(4000 / 3000)
    expect(screen.queryByTestId('review-bbox')).toBeNull()

    loadImageAs(screen.getByAltText('Photo under review'), 1440, 1920)
    // The frame the preview reports, which is the one a correct row (4000x3000 +
    // orientation 6 → a 3000x4000 display frame) would have given.
    expect(frameRatio(stage)).toBeCloseTo(3000 / 4000)
    expect(screen.getByTestId('review-bbox')).toBeInTheDocument()
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

  it('links the photo under question to its own page, on a face and on a label question', async () => {
    // The point is the *URL*: a real anchor can be right-click-copied and
    // Ctrl/Cmd+clicked, which is what the player asked for. So the assertion is
    // on `href`, not on a navigation happening.
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1')]))
    const { unmount } = renderPage()

    await screen.findByTestId('review-question')
    const faceLink = screen.getByRole('link', { name: 'Open the photo in a new tab' })
    expect(faceLink).toHaveAttribute('href', '/photos/p-q1')
    unmount()

    queueMock.mockResolvedValue(makeQueue([labelQuestion('q2')]))
    renderPage()
    await screen.findByTestId('review-question')
    expect(screen.getByRole('link', { name: 'Open the photo in a new tab' })).toHaveAttribute(
      'href',
      '/photos/p-q2',
    )
  })

  it('opens the photo in a new tab without handing it the opener', async () => {
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1')]))
    renderPage()

    await screen.findByTestId('review-question')
    const link = screen.getByRole('link', { name: 'Open the photo in a new tab' })
    expect(link).toHaveAttribute('target', '_blank')
    // A `_blank` target without `noopener` would let the photo page reach back
    // into the game's window through `window.opener`.
    expect(link.getAttribute('rel')).toContain('noopener')
  })

  it('does not answer or advance the queue when the photo link is clicked', async () => {
    const user = userEvent.setup()
    // jsdom cannot open a tab; stub it so the click is quiet and the assertion
    // is about the game, not about the navigation.
    vi.spyOn(window, 'open').mockReturnValue(null)
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice'), faceQuestion('q2', 'Bob')]))
    renderPage()
    await screen.findByTestId('review-question')

    await user.click(screen.getByRole('link', { name: 'Open the photo in a new tab' }))

    expect(answerMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')
    expect(screen.getByTestId('review-progress')).toHaveTextContent('0 answered')
  })

  it('opens the photo on `o` without answering, and still answers on y / n', async () => {
    const user = userEvent.setup()
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    queueMock.mockResolvedValue(makeQueue([faceQuestion('q1', 'Alice'), faceQuestion('q2', 'Bob')]))
    renderPage()
    await screen.findByTestId('review-question')

    await user.keyboard('o')
    expect(open).toHaveBeenCalledWith('/photos/p-q1', '_blank', 'noopener,noreferrer')
    // The shortcut is a detour, not an answer: nothing was sent and the same
    // card is still on screen.
    expect(answerMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('review-question')).toHaveTextContent('Alice')

    // ...and it did not eat the answer keys either (the collision regression).
    await user.keyboard('y')
    expect(answerMock).toHaveBeenLastCalledWith('q1', 'yes')
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Bob')
    })
    await user.keyboard('n')
    expect(answerMock).toHaveBeenLastCalledWith('q2', 'no')
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

  it('asks a place question named after the guessed place, with no confidence', async () => {
    queueMock.mockResolvedValue(makeQueue([placeQuestion('q1', 'Veselice')]))
    renderPage()

    const question = await screen.findByTestId('review-question')
    expect(question).toHaveTextContent('Veselice')
    // The whole frame, no face rectangle: the photo is the question.
    expect(screen.getByAltText('Photo under review')).toBeInTheDocument()
    expect(screen.queryByTestId('review-bbox')).toBeNull()
    // A percentage here would be invented — the estimator reports no score.
    expect(screen.queryByText(/Confidence/)).toBeNull()
  })

  it('shows a duplicate question as two photos side by side with their sizes', async () => {
    queueMock.mockResolvedValue(makeQueue([duplicateQuestion('q1')]))
    renderPage()

    await screen.findByTestId('review-duplicate')
    const halves = screen.getAllByTestId('review-duplicate-half')
    expect(halves).toHaveLength(2)
    // Both copies are shown, and they are different photos — a stage that
    // rendered one twice would look identical and answer itself "yes".
    const first = screen.getByAltText('First photo of the pair')
    const second = screen.getByAltText('Second photo of the pair')
    expect(first.getAttribute('src')).not.toBe(second.getAttribute('src'))
    // The whole frame, never a centre-cropped tile: a crop hides exactly the
    // edges where two exports of one shot differ.
    expect(first.getAttribute('src')).toContain('fit_')
    expect(second.getAttribute('src')).toContain('fit_')
    // The numbers under each copy are part of the question: often the only thing
    // that separates two exports of one shot.
    expect(screen.getAllByText('1200 × 800 px')).toHaveLength(2)
    // The single-photo stage must not also be mounted.
    expect(screen.queryByTestId('review-duplicate')).toBeInTheDocument()
    expect(screen.queryByAltText('Photo under review')).toBeNull()
  })

  it('shows an outlier question as a face crop cut from a full-frame preview', async () => {
    queueMock.mockResolvedValue(makeQueue([outlierQuestion('q1', 'Alice')]))
    renderPage()

    const question = await screen.findByTestId('review-question')
    expect(question).toHaveTextContent('Alice')
    await screen.findByTestId('review-outlier')

    const face = screen.getByTestId('review-outlier-face')
    // The crop MUST come from a `fit_*` size. A `tile_*` is a centre-cropped
    // square — a different frame from the one the bbox was normalised against —
    // so cropping one lands beside the face on anything but a square photo.
    expect(face.getAttribute('data-thumb-size')).toMatch(/^fit_/)
    expect(face.getAttribute('src')).toContain('fit_')
    expect(face.getAttribute('src')).not.toContain('tile_')
    // The face is cropped, not merely outlined: the background is scaled and
    // offset so only the padded box shows.
    expect(parseFloat(face.style.width)).toBeGreaterThan(100)
    // The box is drawn inside the crop, and the whole photo is beside it for
    // context — a face out of its scene is how a curator gets it wrong.
    expect(screen.getByTestId('review-outlier-bbox')).toBeInTheDocument()
    expect(screen.getByAltText('The whole photo the face is on')).toBeInTheDocument()
  })

  it('answers the new kinds with the same one-key controls, and skips them', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(
      makeQueue([duplicateQuestion('q1'), outlierQuestion('q2'), placeQuestion('q3')]),
    )
    answerMock.mockResolvedValue({ result: 'confirmed', answered: 1, remaining: 2 })
    renderPage()

    await screen.findByTestId('review-duplicate')
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q1', 'yes')
    })

    await screen.findByTestId('review-outlier')
    await user.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q2', 'no')
    })

    // Skipping keeps working on the new kinds too — "I do not know" is still an
    // answer, and it still writes nothing.
    await waitFor(() => {
      expect(screen.getByTestId('review-question')).toHaveTextContent('Brno')
    })
    await user.keyboard(' ')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q3', 'skip')
    })
  })

  it('undoes a duplicate confirmation through the un-confirm endpoint', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(makeQueue([duplicateQuestion('q1'), faceQuestion('q2')]))
    answerMock.mockResolvedValue({ result: 'confirmed', answered: 1, remaining: 1 })
    renderPage()

    await screen.findByTestId('review-duplicate')
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q1', 'yes')
    })

    await user.click(screen.getByTestId('review-undo'))
    await waitFor(() => {
      expect(unconfirmDuplicateMock).toHaveBeenCalledWith({
        photo_uid: 'p-q1-a',
        other_uid: 'p-q1-b',
      })
    })
    // The pair comes back as the card on screen; nothing was merged either way.
    await waitFor(() => {
      expect(screen.getByTestId('review-duplicate')).toBeInTheDocument()
    })
  })

  it('undoes an outlier confirmation through the un-confirm endpoint', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(makeQueue([outlierQuestion('q1'), faceQuestion('q2')]))
    answerMock.mockResolvedValue({ result: 'confirmed', answered: 1, remaining: 1 })
    renderPage()

    await screen.findByTestId('review-outlier')
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q1', 'yes')
    })

    await user.click(screen.getByTestId('review-undo'))
    await waitFor(() => {
      expect(unconfirmFaceMock).toHaveBeenCalledWith({
        photo_uid: 'p-q1',
        face_index: 0,
        subject_uid: 's-q1',
      })
    })
  })

  it('offers no undo after a place verdict, because none exists', async () => {
    const user = userEvent.setup()
    queueMock.mockResolvedValue(makeQueue([placeQuestion('q1'), faceQuestion('q2')]))
    answerMock.mockResolvedValue({ result: 'confirmed', answered: 1, remaining: 1 })
    renderPage()

    await screen.findByTestId('review-question')
    // The undo button starts disabled and must stay so: nothing can mark a
    // location an estimate again, and pretending otherwise would write a
    // decision the user never made.
    expect(screen.getByTestId('review-undo')).toBeDisabled()
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(answerMock).toHaveBeenCalledWith('q1', 'yes')
    })
    expect(screen.getByTestId('review-undo')).toBeDisabled()
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

/**
 * The anchor out to the photo is an overlay, and jsdom evaluates no media query
 * and loads no stylesheet — so what keeps it off the answer buttons and above the
 * face rectangle's dimming veil is asserted against the shipped `review.css`
 * itself. The live geometry was measured in a browser harness; these guards are
 * what stops a later edit from quietly undoing it.
 */
describe('review.css photo-link overlay', () => {
  const css = readCss('src/components/review/review.css')
  /** The finger-friendly minimum a touch target has to clear (2.75rem at a 16px root). */
  const TOUCH_FLOOR_PX = 44
  const REM_PX = 16

  /** The declarations of a rule, or a loud failure when the class was renamed. */
  function rule(source: string, prelude: RegExp): Map<string, string> {
    const body = ruleBody(source, prelude)
    if (body === undefined) {
      throw new Error(`rule not found: ${prelude.source}`)
    }
    return declarations(body)
  }

  it('pins the link into the frame’s corner, above the dimming veil', () => {
    const fine = rule(css, /\.review-photo__open\s*(?=\{)/)
    expect(fine.get('position')).toBe('absolute')
    // Anchored top/right: the answer buttons own the bottom of the screen.
    expect(fine.get('top')).toBe('0.5rem')
    expect(fine.get('right')).toBe('0.5rem')
    expect(fine.get('bottom')).toBeUndefined()
    // The face box's `box-shadow` veil is drawn after the image; the link has to
    // sit above it to stay legible and clickable.
    expect(Number(fine.get('z-index'))).toBeGreaterThan(0)
    // Present but quiet on a pointer that can hover it into full strength.
    expect(Number(fine.get('opacity'))).toBeGreaterThan(0.5)
    expect(Number(fine.get('opacity'))).toBeLessThan(1)
  })

  it('gives touch a full-size target at full strength instead of a hover reveal', () => {
    const touch = ruleBody(css, /@media\s*\(hover:\s*none\)\s*/, /review-photo__open/)
    expect(touch).toBeDefined()
    const onTouch = rule(touch ?? '', /\.review-photo__open\s*(?=\{)/)
    expect(parseFloat(onTouch.get('min-width') ?? '0') * REM_PX).toBeGreaterThanOrEqual(
      TOUCH_FLOOR_PX,
    )
    expect(parseFloat(onTouch.get('min-height') ?? '0') * REM_PX).toBeGreaterThanOrEqual(
      TOUCH_FLOOR_PX,
    )
    // There is no hover to reveal it with, so it may not be dimmed there.
    expect(onTouch.get('opacity')).toBe('1')
  })
})
