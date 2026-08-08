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
    frame: { width: 4000, height: 3000 },
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
        photoUid="ph_1"
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
  it('lists a row per face, chipped by whether it has a name', () => {
    renderPanel(
      facesResult({
        faces: [
          faceView({ face_index: 0 }),
          faceView({ face_index: 1, marker_uid: 'mk_1', subject_name: 'Alice' }),
        ],
      }),
    )

    const unnamed = screen.getByRole('button', { name: 'Select face #1: No name' })
    expect(unnamed).toHaveAttribute('data-face-state', 'unnamed')
    expect(unnamed).toHaveTextContent('No name')
    const named = screen.getByRole('button', { name: 'Select face #2: Alice' })
    expect(named).toHaveAttribute('data-face-state', 'named')
    expect(named).toHaveTextContent('Alice')
  })

  it('chips a face a marker already covers exactly like a bare detection', () => {
    // `create_marker` vs `assign_person` is the backend's business; both rows are
    // one click from a name, so the panel says the same thing about both.
    renderPanel(
      facesResult({
        faces: [faceView({ face_index: 0 }), faceView({ face_index: 1, marker_uid: 'mk_1' })],
      }),
    )

    for (const number of [1, 2]) {
      const row = screen.getByRole('button', { name: `Select face #${String(number)}: No name` })
      expect(row).toHaveAttribute('data-face-state', 'unnamed')
      expect(row).toHaveTextContent('No name')
    }
  })

  it('says "Bez jména" in Czech, from i18n', async () => {
    await i18n.changeLanguage('cs')
    renderPanel(facesResult({ faces: [faceView({ face_index: 0 })] }))

    expect(screen.getByRole('button', { name: 'Vybrat obličej #1: Bez jména' })).toHaveTextContent(
      'Bez jména',
    )
  })

  it('numbers rows by position, so a marker with no detected face is not "#0"', () => {
    // Markers with no detected face carry negative face indexes.
    renderPanel(facesResult({ faces: [faceView({ face_index: -1, marker_uid: 'mk_1' })] }))
    expect(
      screen.getByRole('button', { name: 'Select face #1: No name (no embedding)' }),
    ).toBeInTheDocument()
  })

  it('marks a row whose face has no embedding, and leaves the rest unmarked', () => {
    // A negative face index is a marker with no face row behind it: nameable by
    // hand, but no similarity search will ever surface it.
    renderPanel(
      facesResult({
        faces: [faceView({ face_index: -1, marker_uid: 'mk_1' }), faceView({ face_index: 0 })],
      }),
    )

    const marked = screen.getByRole('button', { name: 'Select face #1: No name (no embedding)' })
    expect(marked).toHaveAttribute('data-embedding', 'none')
    expect(marked).toHaveTextContent('No embedding')
    // The mark is not a state: the row is still just "not named".
    expect(marked).toHaveAttribute('data-face-state', 'unnamed')

    const plain = screen.getByRole('button', { name: 'Select face #2: No name' })
    expect(plain).not.toHaveAttribute('data-embedding')
    expect(plain).not.toHaveTextContent('No embedding')
  })

  it('explains the missing embedding in the assignment controls, in both languages', async () => {
    const selected = faceView({ face_index: -1, marker_uid: 'mk_1' })
    const { unmount } = renderPanel(facesResult({ faces: [selected], selected }))
    expect(screen.getByText(/it can only ever be named here, by hand/i)).toBeInTheDocument()
    unmount()

    await i18n.changeLanguage('cs')
    renderPanel(facesResult({ faces: [selected], selected }))
    expect(screen.getByText(/jméno mu jde dát jen tady a ručně/i)).toBeInTheDocument()
  })

  it('says nothing about embeddings for a face that has one', () => {
    const selected = faceView({ face_index: 0 })
    renderPanel(facesResult({ faces: [selected], selected }))

    expect(screen.queryByText(/no embedding/i)).not.toBeInTheDocument()
  })

  it('selects a face when its row is clicked, and deselects it when clicked again', async () => {
    const user = userEvent.setup()
    const select = vi.fn()
    const face = faceView({ face_index: 0 })

    const { rerender } = renderPanel(facesResult({ faces: [face], select }))
    await user.click(screen.getByRole('button', { name: 'Select face #1: No name' }))
    expect(select).toHaveBeenCalledWith(0)

    rerender(
      <I18nextProvider i18n={i18n}>
        <FacesPanel
          photoUid="ph_1"
          faces={facesResult({ faces: [face], selected: face, select })}
          canWrite
          hovered={null}
          onHover={onHover}
          onClose={onClose}
        />
      </I18nextProvider>,
    )
    await user.click(screen.getByRole('button', { name: 'Select face #1: No name' }))
    expect(select).toHaveBeenLastCalledWith(null)
  })

  it('leads every row with a crop of its own face, not with an ordinal', () => {
    // The whole point of the panel: a row has to be matchable to a person by
    // LOOKING at it. "Obličej #4" sent the reader hunting for a numeric badge
    // somewhere on the photo before they could name anybody.
    const { container } = renderPanel(
      facesResult({
        faces: [faceView({ face_index: 0 }), faceView({ face_index: 1, subject_name: 'Alice' })],
      }),
    )

    const crops = container.querySelectorAll('img')
    expect(crops).toHaveLength(2)
    // Cut from a `fit_*` preview of the photo — a `tile_*` is a centre-cropped
    // square and the bbox would land beside the face (`lib/faceSource`).
    for (const crop of crops) {
      expect(crop.getAttribute('src')).toMatch(/\/photos\/ph_1\/thumb\/fit_\d+$/)
    }
    // The number survives as the cross-reference to the box on the photo…
    expect(screen.getByRole('button', { name: 'Select face #1: No name' })).toHaveTextContent('1')
    // …but the words around it are gone.
    expect(screen.queryByText(/Face #/)).not.toBeInTheDocument()
  })

  it('falls back to a person icon while the frame is still unknown', () => {
    // The crop needs the photo's frame; until it lands the slot is filled anyway,
    // so the list does not jump when it does.
    const { container } = renderPanel(
      facesResult({ faces: [faceView({ face_index: 0 })], frame: null }),
    )

    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('.bi-person-circle')).toBeInTheDocument()
  })

  it('reports the hovered row so the box on the photo can highlight', async () => {
    const user = userEvent.setup()
    renderPanel(facesResult({ faces: [faceView({ face_index: 0 })] }))

    await user.hover(screen.getByRole('button', { name: 'Select face #1: No name' }))
    expect(onHover).toHaveBeenCalledWith(0)

    await user.unhover(screen.getByRole('button', { name: 'Select face #1: No name' }))
    expect(onHover).toHaveBeenLastCalledWith(null)
  })

  it('pairs a focused row with its box too, so the keyboard walks the photo', async () => {
    const user = userEvent.setup()
    renderPanel(facesResult({ faces: [faceView({ face_index: 0 }), faceView({ face_index: 1 })] }))

    // The card's own close button is the panel's first tab stop.
    await user.tab()
    await user.tab()
    expect(screen.getByRole('button', { name: 'Select face #1: No name' })).toHaveFocus()
    expect(onHover).toHaveBeenLastCalledWith(0)

    // Moving on drops the first pairing before lighting the next one, so exactly
    // one box is ever highlighted.
    await user.tab()
    expect(onHover).toHaveBeenNthCalledWith(2, null)
    expect(onHover).toHaveBeenLastCalledWith(1)
  })

  it('marks the row the photo says is hovered', () => {
    renderPanel(
      facesResult({ faces: [faceView({ face_index: 0 }), faceView({ face_index: 1 })] }),
      true,
      1,
    )

    expect(screen.getByRole('button', { name: 'Select face #1: No name' })).not.toHaveClass(
      'bg-body-secondary',
    )
    expect(screen.getByRole('button', { name: 'Select face #2: No name' })).toHaveClass(
      'bg-body-secondary',
    )
  })

  it('brings the selected row into view, so a box tapped on the photo finds it', () => {
    // jsdom's `scrollIntoView` is an inert stub (see `test/setup.ts`); spy on it
    // to see the panel ask for the row. `restoreMocks` puts it back afterwards.
    const scrollIntoView = vi.fn()
    vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(scrollIntoView)

    const face = faceView({ face_index: 0 })
    const { unmount } = renderPanel(facesResult({ faces: [face, faceView({ face_index: 1 })] }))
    // Nothing selected → nothing to scroll to; the list stays where the user left it.
    expect(scrollIntoView).not.toHaveBeenCalled()
    unmount()

    // On a phone the panel is a scrollable drawer below the photo, so the row of
    // a face selected on the *photo* can easily sit out of sight — which would
    // leave the tap looking like it did nothing.
    renderPanel(facesResult({ faces: [face, faceView({ face_index: 1 })], selected: face }))
    expect(scrollIntoView).toHaveBeenCalledTimes(1)
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' })
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
        photo_count: 10,
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
    expect(screen.queryByRole('button', { name: 'Select face #1: Alice' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })

  it('tells a viewer why there is nothing here to press', () => {
    renderPanel(facesResult({ faces: [faceView({ face_index: 0, subject_name: 'Alice' })] }), false)

    // A list of rows that answer no click reads as a broken panel unless the
    // panel says, once, that naming is an editor's job.
    expect(
      screen.getByText(
        'You can look through the faces, but naming them needs the editor role — ask an administrator for it.',
      ),
    ).toBeInTheDocument()
  })

  it('keeps the note off an editor’s panel and off an empty one', () => {
    const note = /naming them needs the editor role/
    renderPanel(facesResult({ faces: [faceView({ face_index: 0, subject_name: 'Alice' })] }))
    expect(screen.queryByText(note)).not.toBeInTheDocument()

    renderPanel(facesResult({ faces: [] }), false)
    expect(screen.queryByText(note)).not.toBeInTheDocument()
  })

  it("pairs a viewer's inert row with the photo on hover", async () => {
    // A viewer has nothing to click, but the boxes only name the one being
    // pointed at — so hovering a row is how they ask "which one is that?".
    const user = userEvent.setup()
    const { container } = renderPanel(
      facesResult({ faces: [faceView({ face_index: 0, subject_name: 'Alice' })] }),
      false,
    )

    const [row] = container.getElementsByClassName('list-group-item')
    await user.hover(row)
    expect(onHover).toHaveBeenCalledWith(0)

    await user.unhover(row)
    expect(onHover).toHaveBeenLastCalledWith(null)
  })

  it('reports a failed assignment', () => {
    renderPanel(facesResult({ faces: [faceView()], actionError: true }))
    expect(screen.getByRole('alert')).toHaveTextContent('Could not save the assignment.')
  })
})
