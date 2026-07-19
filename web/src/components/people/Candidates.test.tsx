import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type Candidate, type CandidateResult } from '../../services/faces'
import { type Photo } from '../../services/photos'

import { Candidates } from './Candidates'

vi.mock('../../services/faces', () => ({ searchCandidates: vi.fn() }))
vi.mock('../../services/feedback', () => ({ rejectFace: vi.fn(), unrejectFace: vi.fn() }))
vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, assignFace: vi.fn() }
})

const { searchCandidates } = await import('../../services/faces')
const { rejectFace } = await import('../../services/feedback')
const { assignFace } = await import('../../services/people')
const searchMock = vi.mocked(searchCandidates)
const rejectMock = vi.mocked(rejectFace)
const assignMock = vi.mocked(assignFace)

/** makePhoto builds a photo carrying the fields the candidate card reads. */
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
    subject_uid: 'sj_1',
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

function renderSection(onAssigned = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <Candidates subjectUid="sj_1" onAssigned={onAssigned} />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return { onAssigned }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  rejectMock.mockReset().mockResolvedValue(undefined)
  assignMock.mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Candidates', () => {
  it('does not search on mount — the expensive search waits for the button', () => {
    renderSection()
    expect(screen.getByRole('button', { name: /Find suggestions/i })).toBeInTheDocument()
    expect(searchMock).not.toHaveBeenCalled()
  })

  it('runs the search on click with sensible defaults and renders candidate cards', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    const user = userEvent.setup()
    renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))

    expect(await screen.findByTestId('candidate-card')).toBeInTheDocument()
    expect(searchMock).toHaveBeenCalledWith(
      'sj_1',
      { threshold: 0.5, limit: 60 },
      expect.any(AbortSignal),
    )
  })

  it('confirms a candidate through the assign path, drops the card, and reloads', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    const user = userEvent.setup()
    const { onAssigned } = renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    await screen.findByTestId('candidate-card')

    fireEvent.click(screen.getByRole('button', { name: /it's this person/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'create_marker',
        face_index: 0,
        bbox: [0.1, 0.1, 0.3, 0.3],
        subject_uid: 'sj_1',
      })
    })
    // The confirmed card leaves the list, and the page is told to refresh the gallery.
    await waitFor(() => {
      expect(screen.queryByTestId('candidate-card')).not.toBeInTheDocument()
    })
    expect(onAssigned).toHaveBeenCalled()
  })

  it('confirms onto an existing marker when the candidate already has one', async () => {
    searchMock.mockResolvedValue(
      makeResult([makeCandidate('p1', 'assign_person', { marker_uid: 'mk_9' })]),
    )
    const user = userEvent.setup()
    renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    await screen.findByTestId('candidate-card')

    fireEvent.click(screen.getByRole('button', { name: /it's this person/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'assign_person',
        marker_uid: 'mk_9',
        subject_uid: 'sj_1',
      })
    })
  })

  it('rejects a candidate through the feedback path and drops the card', async () => {
    searchMock.mockResolvedValue(makeResult([makeCandidate('p1', 'create_marker')]))
    const user = userEvent.setup()
    const { onAssigned } = renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    await screen.findByTestId('candidate-card')

    fireEvent.click(screen.getByRole('button', { name: /not this person/i }))

    await waitFor(() => {
      expect(rejectMock).toHaveBeenCalledWith({
        photo_uid: 'p1',
        face_index: 0,
        subject_uid: 'sj_1',
      })
    })
    await waitFor(() => {
      expect(screen.queryByTestId('candidate-card')).not.toBeInTheDocument()
    })
    // A reject is not an assignment: the gallery is left alone.
    expect(onAssigned).not.toHaveBeenCalled()
  })

  it('explains when the subject has no tagged faces to search from', async () => {
    searchMock.mockResolvedValue(makeResult([], { reason: 'no_faces' }))
    const user = userEvent.setup()
    renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    expect(await screen.findByText('This person has no faces yet')).toBeInTheDocument()
  })

  it('explains when the faces have no embedding yet', async () => {
    searchMock.mockResolvedValue(makeResult([], { reason: 'no_embeddings' }))
    const user = userEvent.setup()
    renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    expect(await screen.findByText('No face embeddings yet')).toBeInTheDocument()
  })

  it('shows a zero-match state pointing at the full workspace', async () => {
    searchMock.mockResolvedValue(makeResult([]))
    const user = userEvent.setup()
    renderSection()

    await user.click(screen.getByRole('button', { name: /Find suggestions/i }))
    expect(await screen.findByText('No new suggestions')).toBeInTheDocument()
  })

  it('links to the full /faces workspace pre-filled with this subject', () => {
    renderSection()
    expect(screen.getByRole('link', { name: /Open full workspace/i })).toHaveAttribute(
      'href',
      '/faces?subject=sj_1',
    )
  })
})
