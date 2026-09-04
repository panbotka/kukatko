import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type UseFacesResult } from '../../hooks/useFaces'
import i18n from '../../i18n'
import { type Bbox, type FaceView } from '../../services/people'

import { PeoplePanel } from './PeoplePanel'

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

const onEditFace = vi.fn()

function panel(faces: UseFacesResult, canWrite = true, loading = false, photoUid = 'photo1') {
  return (
    <I18nextProvider i18n={i18n}>
      <PeoplePanel
        photoUid={photoUid}
        faces={faces}
        canWrite={canWrite}
        loading={loading}
        onEditFace={onEditFace}
      />
    </I18nextProvider>
  )
}

function renderPanel(faces: UseFacesResult, canWrite = true, loading = false) {
  return render(panel(faces, canWrite, loading))
}

/** `count` unnamed detections, numbered from 1 as the panel numbers them. */
function crowd(count: number): FaceView[] {
  return Array.from({ length: count }, (_v, index) => faceView({ face_index: index }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

describe('PeoplePanel', () => {
  it('says so when the photo has no people', () => {
    renderPanel(facesResult({ faces: [] }))
    expect(screen.getByText('No people in this photo.')).toBeInTheDocument()
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
      false,
    )
    // Named person visible read-only; the unnamed detection and every control gone.
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.queryByText('Unnamed face 2')).not.toBeInTheDocument()
    expect(container.querySelector('button')).toBeNull()
  })

  it('holds the chips behind a spinner while a neighbour photo loads', () => {
    renderPanel(facesResult({ faces: [faceView()] }), true, true)
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
    rerender(panel(faces, true, false, 'photo2'))
    expect(screen.queryByRole('button', { name: 'Name unnamed face 18' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show 12 more faces' })).toBeInTheDocument()
  })
})
