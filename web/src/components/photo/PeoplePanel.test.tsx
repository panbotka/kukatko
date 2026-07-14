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

function renderPanel(faces: UseFacesResult, canWrite = true, loading = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <PeoplePanel faces={faces} canWrite={canWrite} loading={loading} onEditFace={onEditFace} />
    </I18nextProvider>,
  )
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
})
