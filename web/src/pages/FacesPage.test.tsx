import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { type Candidate, type CandidateResult } from '../services/faces'
import { type Photo } from '../services/photos'
import { frameRatio, loadImageAs } from '../test/imageFrame'

import { FacesPage } from './FacesPage'

vi.mock('../services/faces', () => ({ searchCandidates: vi.fn() }))
vi.mock('../services/feedback', () => ({ rejectFace: vi.fn(), unrejectFace: vi.fn() }))
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubjects: vi.fn(), assignFace: vi.fn() }
})

const { searchCandidates } = await import('../services/faces')
const { rejectFace } = await import('../services/feedback')
const { fetchSubjects, assignFace } = await import('../services/people')
const searchMock = vi.mocked(searchCandidates)
const rejectMock = vi.mocked(rejectFace)
const subjectsMock = vi.mocked(fetchSubjects)
const assignMock = vi.mocked(assignFace)

/** makePhoto builds a photo with the fields the candidate card reads. */
function makePhoto(uid: string): Photo {
  return {
    uid,
    file_width: 1000,
    file_height: 800,
    file_orientation: 1,
    thumb_url: `/thumb/${uid}`,
  } as unknown as Photo
}

/** makeCandidate builds a candidate at photo `uid` with the given action. */
function makeCandidate(
  uid: string,
  action: Candidate['action'],
  extra: Partial<Candidate> = {},
): Candidate {
  return {
    photo: makePhoto(uid),
    face_index: 0,
    bbox: { relative: [0.1, 0.1, 0.3, 0.3], pixel: [100, 80, 300, 240] },
    distance: 0.3,
    match_count: 1,
    action,
    ...extra,
  }
}

/** makeCandidate at photo `uid` whose catalogue row carries the given dimensions. */
function candidateWithRow(uid: string, row: Partial<Photo>): Candidate {
  return makeCandidate(uid, 'create_marker', { photo: { ...makePhoto(uid), ...row } })
}

/** makeResult wraps candidates in a full result, tallying the action counts. */
function makeResult(
  candidates: Candidate[],
  overrides: Partial<CandidateResult> = {},
): CandidateResult {
  return {
    subject_uid: 'su_1',
    source_photo_count: 5,
    source_face_count: 8,
    exemplars_used: 5,
    source_capped: false,
    capped: false,
    faces_without_embedding: 0,
    min_match_count: 2,
    threshold: 0.5,
    counts: {
      create_marker: candidates.filter((c) => c.action === 'create_marker').length,
      assign_person: candidates.filter((c) => c.action === 'assign_person').length,
      already_done: candidates.filter((c) => c.action === 'already_done').length,
    },
    candidates,
    ...overrides,
  }
}

function renderPage(entry = '/faces') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <FacesPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The results grid — the element carrying the pinned column count. */
function gridElement(): HTMLElement {
  const el = document.querySelector<HTMLElement>('[data-density]')
  if (el === null) {
    throw new Error('results grid not rendered')
  }
  return el
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  searchMock.mockReset()
  rejectMock.mockReset().mockResolvedValue(undefined)
  subjectsMock.mockReset().mockResolvedValue([])
  assignMock.mockReset().mockResolvedValue(undefined)
})

describe('FacesPage states', () => {
  it('shows the idle empty state and runs no search before one is asked for', async () => {
    renderPage('/faces')
    expect(await screen.findByText('Pick a person and search')).toBeInTheDocument()
    expect(searchMock).not.toHaveBeenCalled()
  })

  it('explains when the subject has no faces to search from', async () => {
    searchMock.mockResolvedValue(makeResult([], { reason: 'no_faces' }))
    renderPage('/faces?subject=su_1')
    expect(await screen.findByText('This person has no faces yet')).toBeInTheDocument()
  })

  it('shows a zero-match state hinting at a lower threshold', async () => {
    searchMock.mockResolvedValue(makeResult([]))
    renderPage('/faces?subject=su_1')
    expect(await screen.findByText('No new faces')).toBeInTheDocument()
    expect(screen.getByText(/lowering the match threshold/i)).toBeInTheDocument()
  })
})

describe('FacesPage results', () => {
  it('runs the search from the URL and draws each candidate with its bbox rectangle', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    renderPage('/faces?subject=su_1')

    expect(await screen.findByTestId('candidate-card')).toBeInTheDocument()
    // The rectangle is positioned in percentages of the frame around the preview,
    // so it waits for the preview to report the frame it actually rendered at.
    expect(screen.queryByTestId('candidate-bbox')).toBeNull()
    loadImageAs(screen.getByAltText('Photo with the candidate face'), 720, 576)
    expect(screen.getByTestId('candidate-bbox')).toBeInTheDocument()
    expect(screen.getByText('70% match')).toBeInTheDocument()
    expect(searchMock).toHaveBeenCalledWith(
      'su_1',
      { threshold: 0.5, limit: 0 },
      expect.any(AbortSignal),
    )
  })

  it('marks the candidate face from the loaded preview, not from a transposed row', async () => {
    // The production shape of the bug: a row stored already-oriented (3000x4000)
    // and then rotated a second time by its orientation 6 reads as a landscape
    // 4000x3000, while the file — and the preview drawn from it — is portrait.
    // Sized from that row the rectangle's x and width are stretched by 1.78 and the
    // rightmost face lands off the photo entirely.
    const wrong = { file_width: 3000, file_height: 4000, file_orientation: 6 }
    const right = { file_width: 4000, file_height: 3000, file_orientation: 6 }

    searchMock.mockResolvedValue(makeResult([candidateWithRow('p1', wrong)]))
    const transposed = renderPage('/faces?subject=su_1')
    await screen.findByTestId('candidate-card')
    // The row is the estimate that keeps the card's height still, and while it is
    // all there is, no rectangle is drawn against it.
    expect(frameRatio(screen.getByTestId('candidate-frame'))).toBeCloseTo(4000 / 3000)
    expect(screen.queryByTestId('candidate-bbox')).toBeNull()

    loadImageAs(screen.getByAltText('Photo with the candidate face'), 1440, 1920)
    const measured = frameRatio(screen.getByTestId('candidate-frame'))
    const box = screen.getByTestId('candidate-bbox').getAttribute('style')
    transposed.unmount()

    // The same candidate on a correct row: same frame, same rectangle. Which is the
    // whole point — the row no longer decides where the box goes.
    searchMock.mockResolvedValue(makeResult([candidateWithRow('p1', right)]))
    renderPage('/faces?subject=su_1')
    await screen.findByTestId('candidate-card')
    loadImageAs(screen.getByAltText('Photo with the candidate face'), 1440, 1920)

    expect(measured).toBeCloseTo(frameRatio(screen.getByTestId('candidate-frame')))
    expect(measured).toBeCloseTo(3000 / 4000)
    expect(box).toBe(screen.getByTestId('candidate-bbox').getAttribute('style'))
  })

  it('shows the computed min_match_count and its explanation', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'create_marker')], { min_match_count: 3 }),
    )
    renderPage('/faces?subject=su_1')

    expect(
      await screen.findByText('A photo must match at least 3 source photos.'),
    ).toBeInTheDocument()
  })

  it('confirms a card in place: calls assign, flips it, and never refetches', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    renderPage('/faces?subject=su_1')

    fireEvent.click(await screen.findByRole('button', { name: /it's this person/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'create_marker',
        face_index: 0,
        bbox: [0.1, 0.1, 0.3, 0.3],
        subject_uid: 'su_1',
      })
    })
    // The card stays in the grid (flipped in place), its confirm control gone.
    expect(screen.getByTestId('candidate-card')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /it's this person/i })).not.toBeInTheDocument()
    })
    // No second search: the list did not reload.
    expect(searchMock).toHaveBeenCalledTimes(1)
  })

  it('rejects a card: persists the rejection and removes it from the grid', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    renderPage('/faces?subject=su_1')

    fireEvent.click(await screen.findByRole('button', { name: /not this person/i }))

    await waitFor(() => {
      expect(rejectMock).toHaveBeenCalledWith({
        photo_uid: 'p1',
        face_index: 0,
        subject_uid: 'su_1',
      })
    })
    await waitFor(() => {
      expect(screen.queryByTestId('candidate-card')).not.toBeInTheDocument()
    })
  })

  it('confirms with the keyboard and advances focus to the next card', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'create_marker'), makeCandidate('p2', 'create_marker')]),
    )
    renderPage('/faces?subject=su_1')
    await screen.findAllByTestId('candidate-card')

    // Focus the first card, confirm it, then reject the card focus advanced to.
    fireEvent.keyDown(document.body, { key: 'ArrowRight' })
    fireEvent.keyDown(document.body, { key: 'y' })
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(
        'p1',
        expect.objectContaining({ action: 'create_marker' }),
      )
    })

    fireEvent.keyDown(document.body, { key: 'n' })
    await waitFor(() => {
      expect(rejectMock).toHaveBeenCalledWith({
        photo_uid: 'p2',
        face_index: 0,
        subject_uid: 'su_1',
      })
    })
  })

  it('ignores a keyboard reject on an already-confirmed card', async () => {
    // An `already_done` candidate is on the grid under "All" / "Done": the face is
    // assigned to this very subject, so recording "not this person" for it would
    // persist a contradiction. The keyboard is the only way to reach it (the card
    // shows no reject button), so the guard has to live in the shortcut too.
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'already_done'), makeCandidate('p2', 'create_marker')]),
    )
    renderPage('/faces?subject=su_1')
    await screen.findAllByTestId('candidate-card')

    fireEvent.keyDown(document.body, { key: 'ArrowRight' }) // focus the done card
    fireEvent.keyDown(document.body, { key: 'n' })

    await waitFor(() => {
      expect(screen.getAllByTestId('candidate-card')).toHaveLength(2)
    })
    expect(rejectMock).not.toHaveBeenCalled()

    // The still-pending card is unaffected: rejecting it works as before.
    fireEvent.keyDown(document.body, { key: 'ArrowRight' })
    fireEvent.keyDown(document.body, { key: 'n' })
    await waitFor(() => {
      expect(rejectMock).toHaveBeenCalledWith({
        photo_uid: 'p2',
        face_index: 0,
        subject_uid: 'su_1',
      })
    })
  })

  it('confirm-all walks the tab and reports a partial failure', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'create_marker'), makeCandidate('p2', 'create_marker')]),
    )
    assignMock.mockImplementation((photoUid) =>
      photoUid === 'p2' ? Promise.reject(new Error('nope')) : Promise.resolve(),
    )
    renderPage('/faces?subject=su_1')

    // Wait for the cards (so the review list is seeded and the button is enabled)
    // before firing the batch.
    await screen.findAllByTestId('candidate-card')
    fireEvent.click(screen.getByRole('button', { name: /Confirm all/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledTimes(2)
    })
    // The one failure is reported; the other stays confirmed (not rolled back).
    expect(await screen.findByText('1 confirmation failed.')).toBeInTheDocument()
  })

  it('renders the pinned column count and moves with the stepper', async () => {
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '3')
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    renderPage('/faces?subject=su_1')
    await screen.findByTestId('candidate-card')

    expect(gridElement().style.gridTemplateColumns).toBe('repeat(3, 1fr)')

    fireEvent.click(screen.getByRole('button', { name: 'More tiles per row' }))

    await waitFor(() => {
      expect(gridElement().style.gridTemplateColumns).toBe('repeat(4, 1fr)')
    })
    // Persisted on the review tools' shared key, so the next tool opens at it.
    expect(window.localStorage.getItem(REVIEW_GRID_SCOPE.storageKey)).toBe('4')
  })

  it('enlarges a candidate, and confirming from the overlay writes what the card writes', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'create_marker'), makeCandidate('p2', 'create_marker')]),
    )
    renderPage('/faces?subject=su_1')
    await screen.findAllByTestId('candidate-card')

    fireEvent.click(screen.getAllByRole('button', { name: 'Enlarge the photo' })[0])

    const overlay = await screen.findByRole('dialog')
    expect(within(overlay).getByAltText('Photo with the candidate face')).toBeInTheDocument()
    // The way out to the photo's own page is the stage's corner anchor.
    expect(within(overlay).getByTestId('review-open-photo')).toHaveAttribute('href', '/photos/p1')

    fireEvent.click(within(overlay).getByRole('button', { name: /it's this person/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'create_marker',
        face_index: 0,
        bbox: [0.1, 0.1, 0.3, 0.3],
        subject_uid: 'su_1',
      })
    })
  })

  it('gives the overlay the keyboard: the grid does not decide underneath it', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'create_marker'), makeCandidate('p2', 'create_marker')]),
    )
    renderPage('/faces?subject=su_1')
    await screen.findAllByTestId('candidate-card')

    fireEvent.keyDown(document.body, { key: 'ArrowRight' }) // focus the first card
    fireEvent.click(screen.getAllByRole('button', { name: 'Enlarge the photo' })[1])
    await screen.findByRole('dialog')

    // `y` would confirm the focused card if the page were still listening.
    fireEvent.keyDown(document.body, { key: 'y' })
    expect(assignMock).not.toHaveBeenCalled()

    // Escape closes the overlay and the grid takes the keyboard back.
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    fireEvent.keyDown(document.body, { key: 'y' })
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(
        'p1',
        expect.objectContaining({ action: 'create_marker' }),
      )
    })
  })

  it('filters the grid by tab', async () => {
    searchMock.mockResolvedValue(
      makeResult([
        makeCandidate('p1', 'create_marker'),
        makeCandidate('p2', 'assign_person', { marker_uid: 'mk_2' }),
      ]),
    )
    renderPage('/faces?subject=su_1')
    await screen.findAllByTestId('candidate-card')
    expect(screen.getAllByTestId('candidate-card')).toHaveLength(2)

    // The "Assign" tab shows only the assign_person candidate.
    fireEvent.click(screen.getByRole('button', { name: /^Assign/ }))
    await waitFor(() => {
      expect(screen.getAllByTestId('candidate-card')).toHaveLength(1)
    })
    const card = screen.getByTestId('candidate-card')
    expect(within(card).getByText('Assign person')).toBeInTheDocument()
  })
})
