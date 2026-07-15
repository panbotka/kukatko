import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Candidate } from '../services/faces'
import { type Photo } from '../services/photos'
import { type Subject } from '../services/people'
import { type SweepMessage } from '../services/recognition'

import { RecognitionPage } from './RecognitionPage'

vi.mock('../services/recognition', () => ({ streamSweep: vi.fn() }))
vi.mock('../services/feedback', () => ({ rejectFace: vi.fn(), unrejectFace: vi.fn() }))
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, assignFace: vi.fn() }
})

const { streamSweep } = await import('../services/recognition')
const { rejectFace } = await import('../services/feedback')
const { assignFace } = await import('../services/people')
const streamMock = vi.mocked(streamSweep)
const rejectMock = vi.mocked(rejectFace)
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

/** makeCandidate builds a candidate at photo `uid`. */
function makeCandidate(uid: string): Candidate {
  return {
    photo: makePhoto(uid),
    face_index: 0,
    bbox: { relative: [0.1, 0.1, 0.3, 0.3], pixel: [100, 80, 300, 240] },
    distance: 0.2,
    match_count: 1,
    action: 'create_marker',
  }
}

/** makeSubject builds a subject with the fields the card reads. */
function makeSubject(uid: string, name: string): Subject {
  return { uid, name } as unknown as Subject
}

/** progress builds a progress message. */
function progress(scanned: number, total: number, name: string): SweepMessage {
  return { type: 'progress', progress: { scanned, total, name } }
}

/** personMessage builds a person message from a subject and its candidates. */
function personMessage(subject: Subject, candidates: Candidate[]): SweepMessage {
  return {
    type: 'person',
    person: {
      subject,
      candidates,
      counts: { create_marker: candidates.length, assign_person: 0, already_done: 0 },
      actionable: candidates.length,
    },
  }
}

/** summaryMessage builds a terminal summary message. */
function summaryMessage(): SweepMessage {
  return {
    type: 'summary',
    summary: {
      people_scanned: 1,
      people_with_matches: 1,
      total_actionable: 1,
      total_already_done: 0,
      capped: false,
      subjects_total: 1,
    },
  }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/recognition']}>
        <RecognitionPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** startScan clicks the Scan button. */
function startScan() {
  fireEvent.click(screen.getByRole('button', { name: 'Scan' }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  streamMock.mockReset()
  rejectMock.mockReset().mockResolvedValue(undefined)
  assignMock.mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecognitionPage', () => {
  it('renders live progress and person cards while the scan is running', async () => {
    const alice = makeSubject('su_alice', 'Alice')
    // The stream stays open (never resolves) so the page remains in the scanning phase.
    streamMock.mockImplementation((_params, onMessage) => {
      onMessage(progress(2, 3, 'Bob'))
      onMessage(personMessage(alice, [makeCandidate('p1')]))
      return new Promise<void>(() => undefined)
    })

    renderPage()
    startScan()

    expect(await screen.findByTestId('sweep-progress')).toBeInTheDocument()
    expect(screen.getByText(/Scanning Bob/)).toBeInTheDocument()
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Alice' })).toBeInTheDocument()
    expect(screen.getByTestId('candidate-card')).toBeInTheDocument()
  })

  it('drops a person card when its last candidate is cleared', async () => {
    const alice = makeSubject('su_alice', 'Alice')
    streamMock.mockImplementation((_params, onMessage) => {
      onMessage(personMessage(alice, [makeCandidate('p1')]))
      onMessage(summaryMessage())
      return Promise.resolve()
    })

    renderPage()
    startScan()

    // Confirm the person's only candidate; the whole card must then disappear.
    fireEvent.click(await screen.findByRole('button', { name: /it's this person/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(
        'p1',
        expect.objectContaining({ action: 'create_marker' }),
      )
    })
    await waitFor(() => {
      expect(screen.queryByTestId('person-sweep-card')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Nothing left to confirm')).toBeInTheDocument()
  })

  it('confirms a whole person with "Confirm all"', async () => {
    const alice = makeSubject('su_alice', 'Alice')
    streamMock.mockImplementation((_params, onMessage) => {
      onMessage(personMessage(alice, [makeCandidate('p1'), makeCandidate('p2')]))
      onMessage(summaryMessage())
      return Promise.resolve()
    })

    renderPage()
    startScan()

    await screen.findByTestId('person-sweep-card')
    fireEvent.click(screen.getByRole('button', { name: /Confirm all/i }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledTimes(2)
    })
    expect(assignMock).toHaveBeenCalledWith('p1', expect.anything())
    expect(assignMock).toHaveBeenCalledWith('p2', expect.anything())
    await waitFor(() => {
      expect(screen.queryByTestId('person-sweep-card')).not.toBeInTheDocument()
    })
  })

  it('cancels a running scan', async () => {
    streamMock.mockImplementation((_params, onMessage, signal) => {
      onMessage(progress(1, 5, 'Alice'))
      return new Promise<void>((_resolve, reject) => {
        signal?.addEventListener('abort', () => {
          reject(new DOMException('aborted', 'AbortError'))
        })
      })
    })

    renderPage()
    startScan()

    expect(await screen.findByTestId('sweep-progress')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan' })).toBeInTheDocument()
    })
    expect(screen.queryByTestId('sweep-progress')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
