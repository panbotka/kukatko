import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Candidate, type CandidateResult } from '../services/faces'
import { type Photo } from '../services/photos'

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

/** makeResult wraps candidates in a full result, tallying the action counts. */
function makeResult(
  candidates: Candidate[],
  overrides: Partial<CandidateResult> = {},
): CandidateResult {
  return {
    subject_uid: 'su_1',
    source_photo_count: 5,
    source_face_count: 8,
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

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  rejectMock.mockReset().mockResolvedValue(undefined)
  subjectsMock.mockReset().mockResolvedValue([])
  assignMock.mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
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
    expect(screen.getByTestId('candidate-bbox')).toBeInTheDocument()
    expect(screen.getByText('70% match')).toBeInTheDocument()
    expect(searchMock).toHaveBeenCalledWith(
      'su_1',
      { threshold: 0.5, limit: 0 },
      expect.any(AbortSignal),
    )
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
