import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type UseFacesResult } from '../../hooks/useFaces'
import i18n from '../../i18n'
import { type Bbox, type FaceView } from '../../services/people'
import * as people from '../../services/people'

import { FacesPanel } from './FacesPanel'

vi.mock('../../services/people', async () => {
  const actual = await vi.importActual<typeof people>('../../services/people')
  return { ...actual, fetchSubjects: vi.fn() }
})

const fetchSubjectsMock = vi.mocked(people.fetchSubjects)

function faceView(overrides: Partial<FaceView> = {}): FaceView {
  return {
    face_index: 0,
    bbox: [0.1, 0.2, 0.3, 0.4] as Bbox,
    det_score: 0.9,
    action: 'create_marker',
    suggestions: [],
    ...overrides,
  }
}

function facesResult(overrides: Partial<UseFacesResult> = {}): UseFacesResult {
  return {
    status: 'ready',
    faces: [],
    selected: null,
    busy: false,
    actionError: false,
    select: vi.fn(),
    acceptSuggestion: vi.fn(),
    assignName: vi.fn(),
    unassign: vi.fn(),
    ...overrides,
  }
}

const onHover = vi.fn()
const onClose = vi.fn()

function renderPanel(faces: UseFacesResult, canWrite = true, hovered: number | null = null) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FacesPanel
        faces={faces}
        canWrite={canWrite}
        hovered={hovered}
        onHover={onHover}
        onClose={onClose}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchSubjectsMock.mockResolvedValue([])
})

describe('FacesPanel', () => {
  it('lists a row per face, chipped by how far it has got through naming', () => {
    renderPanel(
      facesResult({
        faces: [
          faceView({ face_index: 0 }),
          faceView({ face_index: 1, marker_uid: 'mk_1', subject_name: 'Alice' }),
        ],
      }),
    )

    expect(screen.getByRole('button', { name: 'Select face #1' })).toHaveAttribute(
      'data-face-state',
      'unmatched',
    )
    const named = screen.getByRole('button', { name: 'Select face #2' })
    expect(named).toHaveAttribute('data-face-state', 'assigned')
    expect(named).toHaveTextContent('Alice')
  })

  it('numbers rows by position, so a marker with no detected face is not "#0"', () => {
    // Markers with no detected face carry negative face indexes.
    renderPanel(facesResult({ faces: [faceView({ face_index: -1, marker_uid: 'mk_1' })] }))
    expect(screen.getByRole('button', { name: 'Select face #1' })).toBeInTheDocument()
  })

  it('selects a face when its row is clicked, and deselects it when clicked again', async () => {
    const user = userEvent.setup()
    const select = vi.fn()
    const face = faceView({ face_index: 0 })

    const { rerender } = renderPanel(facesResult({ faces: [face], select }))
    await user.click(screen.getByRole('button', { name: 'Select face #1' }))
    expect(select).toHaveBeenCalledWith(0)

    rerender(
      <I18nextProvider i18n={i18n}>
        <FacesPanel
          faces={facesResult({ faces: [face], selected: face, select })}
          canWrite
          hovered={null}
          onHover={onHover}
          onClose={onClose}
        />
      </I18nextProvider>,
    )
    await user.click(screen.getByRole('button', { name: 'Select face #1' }))
    expect(select).toHaveBeenLastCalledWith(null)
  })

  it('reports the hovered row so the box on the photo can highlight', async () => {
    const user = userEvent.setup()
    renderPanel(facesResult({ faces: [faceView({ face_index: 0 })] }))

    await user.hover(screen.getByRole('button', { name: 'Select face #1' }))
    expect(onHover).toHaveBeenCalledWith(0)
  })

  it('opens the assignment controls under the selected row', async () => {
    const selected = faceView({
      face_index: 0,
      suggestions: [{ subject_uid: 'su_1', subject_name: 'Alice', distance: 0.2, confidence: 0.8 }],
    })
    renderPanel(facesResult({ faces: [selected], selected }))

    expect(screen.getByLabelText('Name this face')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: 'Alice · 80%' })).toBeInTheDocument()
  })

  it('accepts a suggestion with one tap', async () => {
    const user = userEvent.setup()
    const acceptSuggestion = vi.fn()
    const suggestion = {
      subject_uid: 'su_1',
      subject_name: 'Alice',
      distance: 0.2,
      confidence: 0.8,
    }
    const selected = faceView({ face_index: 0, suggestions: [suggestion] })
    renderPanel(facesResult({ faces: [selected], selected, acceptSuggestion }))

    await user.click(screen.getByRole('button', { name: 'Alice · 80%' }))
    expect(acceptSuggestion).toHaveBeenCalledWith(selected, suggestion)
  })

  it('offers reassignment and removal for an already-named face', async () => {
    const user = userEvent.setup()
    const unassign = vi.fn()
    const selected = faceView({
      face_index: 0,
      marker_uid: 'mk_1',
      subject_uid: 'su_1',
      subject_name: 'Alice',
      suggestions: [{ subject_uid: 'su_2', subject_name: 'Bob', distance: 0.3, confidence: 0.7 }],
    })
    renderPanel(facesResult({ faces: [selected], selected, unassign }))

    // The name is not one stray click from being replaced: the alternatives only
    // appear once reassignment is asked for.
    expect(screen.getByText('Assigned to Alice')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bob · 70%' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Reassign' }))
    expect(screen.getByRole('button', { name: 'Bob · 70%' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Remove person' }))
    expect(unassign).toHaveBeenCalledWith(selected)
  })

  it('names a face with an existing person from the typeahead', async () => {
    const user = userEvent.setup()
    const acceptSuggestion = vi.fn()
    fetchSubjectsMock.mockResolvedValue([
      {
        uid: 'su_9',
        slug: 'alice',
        name: 'Alice',
        type: 'person',
        favorite: false,
        private: false,
        notes: '',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        marker_count: 12,
      },
    ])
    const selected = faceView({ face_index: 0 })
    renderPanel(facesResult({ faces: [selected], selected, acceptSuggestion }))

    await waitFor(() => {
      expect(screen.getByLabelText('Name')).toBeEnabled()
    })
    await user.type(screen.getByLabelText('Name'), 'ali')
    await user.click(await screen.findByRole('option', { name: /Alice/ }))

    expect(acceptSuggestion).toHaveBeenCalledWith(selected, {
      subject_uid: 'su_9',
      subject_name: 'Alice',
    })
  })

  it('shows a viewer the people, with nothing to click', () => {
    renderPanel(facesResult({ faces: [faceView({ face_index: 0, subject_name: 'Alice' })] }), false)

    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Select face #1' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })

  it('reports a failed assignment', () => {
    renderPanel(facesResult({ faces: [faceView()], actionError: true }))
    expect(screen.getByRole('alert')).toHaveTextContent('Could not save the assignment.')
  })
})
