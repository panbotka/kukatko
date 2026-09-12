import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type UseFacesResult } from '../../hooks/useFaces'
import i18n from '../../i18n'
import {
  type Bbox,
  type FaceView,
  type PhotoSubject,
  type SubjectCount,
} from '../../services/people'

import { PeoplePanel } from './PeoplePanel'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return {
    ...actual,
    fetchSubjects: vi.fn(),
    attachPerson: vi.fn(),
    detachPerson: vi.fn(),
    createSubject: vi.fn(),
  }
})

const { fetchSubjects, attachPerson, detachPerson, createSubject } =
  await import('../../services/people')
const fetchSubjectsMock = vi.mocked(fetchSubjects)
const attachPersonMock = vi.mocked(attachPerson)
const detachPersonMock = vi.mocked(detachPerson)
const createSubjectMock = vi.mocked(createSubject)

/** A subject the picker may offer, with the counts the list carries. */
function subjectCount(uid: string, name: string): SubjectCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    nickname: '',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: 3,
    photo_count: 2,
  }
}

/** Somebody attached to the media item by hand — no box, no face, no crop. */
function attached(uid: string, name: string): PhotoSubject {
  return {
    subject_uid: uid,
    slug: name.toLowerCase(),
    name,
    type: 'person',
    marker_uid: `mk_${uid}`,
    attached_at: '2026-02-03T10:00:00Z',
  }
}

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
    confirmAll: vi.fn(),
    cancelConfirmAll: vi.fn(),
    confirmAllState: { running: false, current: 0, total: 0, failed: 0 },
    ...overrides,
  }
}

const onEditFace = vi.fn()
const onPeopleChanged = vi.fn()

/** Everything past the faces the panel may be handed, each with its resting value. */
interface PanelOptions {
  canWrite?: boolean
  loading?: boolean
  photoUid?: string
  people?: PhotoSubject[]
  canOpenFaces?: boolean
}

function panel(faces: UseFacesResult, options: PanelOptions = {}) {
  const {
    canWrite = true,
    loading = false,
    photoUid = 'photo1',
    people = [],
    canOpenFaces = true,
  } = options
  return (
    <I18nextProvider i18n={i18n}>
      {/* The hand-attached chips link to their person, so the panel needs a router. */}
      <MemoryRouter>
        <PeoplePanel
          photoUid={photoUid}
          faces={faces}
          people={people}
          canWrite={canWrite}
          canOpenFaces={canOpenFaces}
          loading={loading}
          onEditFace={onEditFace}
          onPeopleChanged={onPeopleChanged}
        />
      </MemoryRouter>
    </I18nextProvider>
  )
}

function renderPanel(faces: UseFacesResult, options: PanelOptions = {}) {
  return render(panel(faces, options))
}

/** Opens the picker and names somebody in it, the way a reader does. */
async function addPerson(user: ReturnType<typeof userEvent.setup>, name: string | RegExp) {
  await user.click(screen.getByRole('button', { name: 'Add who is here' }))
  await user.type(await screen.findByLabelText('Who is here?'), 'Ali')
  await user.click(await screen.findByRole('option', { name }))
}

/** `count` unnamed detections, numbered from 1 as the panel numbers them. */
function crowd(count: number): FaceView[] {
  return Array.from({ length: count }, (_v, index) => faceView({ face_index: index }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchSubjectsMock.mockResolvedValue([subjectCount('su_a', 'Alice'), subjectCount('su_b', 'Bob')])
  attachPersonMock.mockResolvedValue([attached('su_a', 'Alice')])
  detachPersonMock.mockResolvedValue([])
})

describe('PeoplePanel', () => {
  it('says so when nobody is on the media item at all', () => {
    renderPanel(facesResult({ faces: [] }))
    // Neither "photo" nor "people": the same block answers for a video, and a
    // subject can be the dog.
    expect(screen.getByText('Nobody here yet.')).toBeInTheDocument()
  })

  it('renders a named person and an unnamed detection as editable chips', () => {
    renderPanel(
      facesResult({
        faces: [
          faceView({ face_index: 0, subject_name: 'Alice', marker_uid: 'mk_1' }),
          faceView({ face_index: 1 }),
        ],
      }),
    )
    // A named person is an editable chip; an unnamed detection is a nameable one.
    expect(screen.getByRole('button', { name: 'Edit Alice' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Name unnamed face 2' })).toBeInTheDocument()
  })

  it('hands a clicked chip to the faces panel instead of naming it here', async () => {
    const user = userEvent.setup()
    renderPanel(facesResult({ faces: [faceView({ face_index: 0 })] }))

    await user.click(screen.getByRole('button', { name: 'Name unnamed face 1' }))
    // Assignment lives in exactly one place — the faces panel beside the photo.
    expect(onEditFace).toHaveBeenCalledWith(0)
  })

  it('never names a face itself, not even for the selected one', () => {
    const selected = faceView({ face_index: 0 })
    renderPanel(facesResult({ faces: [selected], selected }))
    expect(screen.queryByLabelText('Name this face')).not.toBeInTheDocument()
  })

  it('shows only named people, read-only, to a viewer', () => {
    const { container } = renderPanel(
      facesResult({
        faces: [
          faceView({ face_index: 0, subject_name: 'Alice', marker_uid: 'mk_1' }),
          faceView({ face_index: 1 }),
        ],
      }),
      { canWrite: false },
    )
    // Named person visible read-only; the unnamed detection and every control gone.
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.queryByText('Unnamed face 2')).not.toBeInTheDocument()
    expect(container.querySelector('button')).toBeNull()
  })

  it('holds the chips behind a spinner while a neighbour photo loads', () => {
    renderPanel(facesResult({ faces: [faceView()] }), { loading: true })
    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('lists every unnamed face while there are few of them, with no control', () => {
    renderPanel(facesResult({ faces: crowd(6) }))

    expect(screen.getByRole('button', { name: 'Name unnamed face 6' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /more faces/ })).not.toBeInTheDocument()
  })

  it('folds a crowd of unnamed faces away behind a control that counts them', () => {
    renderPanel(facesResult({ faces: crowd(18) }))

    expect(screen.getByRole('button', { name: 'Name unnamed face 6' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Name unnamed face 7' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show 12 more faces' })).toBeInTheDocument()
  })

  it('unfolds the rest in place, and folds them back', async () => {
    const user = userEvent.setup()
    renderPanel(facesResult({ faces: crowd(18) }))

    await user.click(screen.getByRole('button', { name: 'Show 12 more faces' }))
    expect(screen.getByRole('button', { name: 'Name unnamed face 18' })).toBeInTheDocument()

    const fold = screen.getByRole('button', { name: 'Show fewer faces' })
    expect(fold).toHaveAttribute('aria-expanded', 'true')
    await user.click(fold)
    expect(screen.queryByRole('button', { name: 'Name unnamed face 18' })).not.toBeInTheDocument()
  })

  it('always shows a named person, however deep in the crowd they stand', () => {
    const faces = crowd(18)
    faces[16] = faceView({ face_index: 16, subject_name: 'Alice', marker_uid: 'mk_1' })

    renderPanel(facesResult({ faces }))

    // The crowd is folded, yet Alice — the answer to "who is in this photo" — is not.
    expect(screen.getByRole('button', { name: 'Edit Alice' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show 11 more faces' })).toBeInTheDocument()
  })

  it('takes the next face out of the fold when one of the shown ones is named', () => {
    const faces = crowd(18)
    const { rerender } = render(panel(facesResult({ faces })))
    expect(screen.getByRole('button', { name: 'Show 12 more faces' })).toBeInTheDocument()

    // Naming face 3 in the faces panel comes back as a patched list: it stays on
    // screen as a person, and the seventh detection moves up into its place.
    const named = [...faces]
    named[2] = faceView({ face_index: 2, subject_name: 'Alice', marker_uid: 'mk_1' })
    rerender(panel(facesResult({ faces: named })))

    expect(screen.getByRole('button', { name: 'Edit Alice' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Name unnamed face 7' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show 11 more faces' })).toBeInTheDocument()
  })

  it('offers no control when every face already has a name', () => {
    renderPanel(
      facesResult({
        faces: Array.from({ length: 9 }, (_v, index) =>
          faceView({
            face_index: index,
            subject_name: `Person ${index}`,
            marker_uid: `mk_${index}`,
          }),
        ),
      }),
    )

    expect(screen.getByRole('button', { name: 'Edit Person 8' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /more faces/ })).not.toBeInTheDocument()
  })

  it('folds the crowd back up on the next photo', async () => {
    const user = userEvent.setup()
    const faces = facesResult({ faces: crowd(18) })
    const { rerender } = render(panel(faces))

    await user.click(screen.getByRole('button', { name: 'Show 12 more faces' }))
    expect(screen.getByRole('button', { name: 'Name unnamed face 18' })).toBeInTheDocument()

    // The panel is not remounted between neighbours; the next photo starts folded.
    rerender(panel(faces, { photoUid: 'photo2' }))
    expect(screen.queryByRole('button', { name: 'Name unnamed face 18' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show 12 more faces' })).toBeInTheDocument()
  })

  it('offers no click on a chip that has no faces panel to open', () => {
    // A saved crop leaves a frame the boxes were never measured against, so the
    // viewer keeps them off — and a chip that silently falls back to the metadata
    // is worse than a chip that does not offer the click.
    renderPanel(facesResult({ faces: [faceView({ face_index: 0, subject_name: 'Alice' })] }), {
      canOpenFaces: false,
    })

    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit Alice' })).not.toBeInTheDocument()
  })

  describe('people attached by hand', () => {
    it('lists them as chips of their own, with a remove control for an editor', () => {
      renderPanel(facesResult({ faces: [] }), { people: [attached('su_a', 'Alice')] })

      // The chip links to the person and carries no crop — it has no face to cut
      // one from, which is what tells it apart from a detected one.
      expect(screen.getByRole('link', { name: 'Alice' })).toHaveAttribute('href', '/people/su_a')
      expect(screen.getByRole('button', { name: 'Remove Alice' })).toBeInTheDocument()
      expect(screen.queryByText('Nobody here yet.')).not.toBeInTheDocument()
    })

    it('offers the add control whatever the detector found', () => {
      const { rerender } = render(panel(facesResult({ faces: [] })))
      expect(screen.getByRole('button', { name: 'Add who is here' })).toBeInTheDocument()

      // Somebody in the background of a photo full of detected faces needs
      // naming just as much, so the control is not a fallback for an empty list.
      rerender(panel(facesResult({ faces: crowd(3) })))
      expect(screen.getByRole('button', { name: 'Add who is here' })).toBeInTheDocument()
    })

    it('attaches the person picked in the shared picker and redraws from the reply', async () => {
      const user = userEvent.setup()
      attachPersonMock.mockResolvedValue([attached('su_a', 'Alice')])
      renderPanel(facesResult({ faces: [] }))

      await addPerson(user, /Alice/)

      await waitFor(() => {
        expect(attachPersonMock).toHaveBeenCalledWith('photo1', 'su_a')
      })
      // The mutation answers the whole resulting list, so nothing re-reads the photo.
      expect(onPeopleChanged).toHaveBeenCalledWith([attached('su_a', 'Alice')])
    })

    it('creates somebody the library has never heard of, then attaches them', async () => {
      const user = userEvent.setup()
      fetchSubjectsMock.mockResolvedValue([])
      createSubjectMock.mockResolvedValue({
        uid: 'su_new',
        slug: 'alice',
        name: 'Alice',
        nickname: '',
        type: 'person',
        favorite: false,
        private: false,
        notes: '',
        birth_year: null,
        death_year: null,
        created_at: '2026-02-03T10:00:00Z',
        updated_at: '2026-02-03T10:00:00Z',
      })
      attachPersonMock.mockResolvedValue([attached('su_new', 'Alice')])
      renderPanel(facesResult({ faces: [] }))

      await user.click(screen.getByRole('button', { name: 'Add who is here' }))
      await user.type(await screen.findByLabelText('Who is here?'), 'Alice')
      await user.click(await screen.findByRole('option', { name: /Create/ }))

      await waitFor(() => {
        expect(attachPersonMock).toHaveBeenCalledWith('photo1', 'su_new')
      })
      expect(createSubjectMock).toHaveBeenCalledWith(expect.objectContaining({ name: 'Alice' }))
    })

    it('detaches one again and redraws from the reply', async () => {
      const user = userEvent.setup()
      detachPersonMock.mockResolvedValue([attached('su_b', 'Bob')])
      renderPanel(facesResult({ faces: [] }), {
        people: [attached('su_a', 'Alice'), attached('su_b', 'Bob')],
      })

      await user.click(screen.getByRole('button', { name: 'Remove Alice' }))

      await waitFor(() => {
        expect(detachPersonMock).toHaveBeenCalledWith('photo1', 'su_a')
      })
      expect(onPeopleChanged).toHaveBeenCalledWith([attached('su_b', 'Bob')])
    })

    it('reports a failed write and leaves the list as it was', async () => {
      const user = userEvent.setup()
      detachPersonMock.mockRejectedValue(new Error('nope'))
      renderPanel(facesResult({ faces: [] }), { people: [attached('su_a', 'Alice')] })

      await user.click(screen.getByRole('button', { name: 'Remove Alice' }))

      expect(await screen.findByText('Could not save the change.')).toBeInTheDocument()
      expect(onPeopleChanged).not.toHaveBeenCalled()
      expect(screen.getByRole('link', { name: 'Alice' })).toBeInTheDocument()
    })

    it('shows a viewer the chips and neither control', () => {
      renderPanel(facesResult({ faces: [] }), {
        canWrite: false,
        people: [attached('su_a', 'Alice')],
      })

      expect(screen.getByRole('link', { name: 'Alice' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Remove Alice' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Add who is here' })).not.toBeInTheDocument()
    })
  })
})
