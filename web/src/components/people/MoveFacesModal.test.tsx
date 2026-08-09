import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type FacesResponse, type SubjectCount } from '../../services/people'

import { MoveFacesModal } from './MoveFacesModal'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn(), fetchFaces: vi.fn(), assignFace: vi.fn() }
})

const { fetchSubjects, fetchFaces, assignFace } = await import('../../services/people')
const subjectsMock = vi.mocked(fetchSubjects)
const facesMock = vi.mocked(fetchFaces)
const assignMock = vi.mocked(assignFace)

/** A listed person with counts, as `GET /subjects` returns one. */
function counted(uid: string, name: string): SubjectCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: 2,
    photo_count: 2,
  }
}

/** A photo carrying one marker of `subjectUid`, or none when it is omitted. */
function faces(photoUid: string, subjectUid?: string): FacesResponse {
  return {
    photo_uid: photoUid,
    width: 1000,
    height: 800,
    orientation: 1,
    faces: [
      {
        face_index: 0,
        bbox: [0.1, 0.1, 0.2, 0.2],
        det_score: 0.9,
        action: subjectUid === undefined ? 'assign_person' : 'already_done',
        marker_uid: `mk_${photoUid}`,
        subject_uid: subjectUid,
        suggestions: [],
      },
    ],
  }
}

function renderModal(photoUids: string[], onMoved = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <MoveFacesModal
        sourceUid="su_a"
        sourceName="Anna"
        photoUids={photoUids}
        show
        onHide={vi.fn()}
        onMoved={onMoved}
      />
    </I18nextProvider>,
  )
  return { onMoved }
}

describe('MoveFacesModal', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    subjectsMock.mockResolvedValue([counted('su_a', 'Anna'), counted('su_b', 'Božena')])
    assignMock.mockResolvedValue(undefined)
  })

  it('moves the picked photos to an existing person through the assign endpoint', async () => {
    facesMock.mockImplementation((photoUid: string) => Promise.resolve(faces(photoUid, 'su_a')))
    const user = userEvent.setup()
    const { onMoved } = renderModal(['p1', 'p2'])

    const field = await screen.findByRole('combobox', { name: 'Move to…' })
    await user.type(field, 'Bož')
    await user.click(await screen.findByRole('option', { name: /Božena/ }))

    expect(await screen.findByText('Moved 2 photos to Božena.')).toBeInTheDocument()
    expect(assignMock).toHaveBeenCalledTimes(2)
    expect(assignMock).toHaveBeenCalledWith('p1', {
      action: 'assign_person',
      marker_uid: 'mk_p1',
      subject_uid: 'su_b',
      subject_name: undefined,
      face_index: 0,
    })
    expect(onMoved).toHaveBeenCalledWith(
      expect.objectContaining({ moved: 2, photos: 2, skipped: 0, failed: 0 }),
    )
  })

  it('creates a person from a typed name, letting the backend find or create them', async () => {
    facesMock.mockImplementation((photoUid: string) => Promise.resolve(faces(photoUid, 'su_a')))
    const user = userEvent.setup()
    renderModal(['p1'])

    const field = await screen.findByRole('combobox', { name: 'Move to…' })
    await user.type(field, 'Ludmila')
    await user.click(await screen.findByRole('option', { name: /Ludmila/ }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'assign_person',
        marker_uid: 'mk_p1',
        subject_uid: undefined,
        subject_name: 'Ludmila',
        face_index: 0,
      })
    })
    expect(await screen.findByText('Moved 1 photo to Ludmila.')).toBeInTheDocument()
  })

  it('accounts for a photo this person has no movable face on', async () => {
    // The second photo is in the gallery but carries no marker of this person —
    // a box-less tag, say. Nothing is sent for it, and the report says so.
    facesMock.mockImplementation((photoUid: string) =>
      Promise.resolve(photoUid === 'p1' ? faces(photoUid, 'su_a') : faces(photoUid)),
    )
    const user = userEvent.setup()
    renderModal(['p1', 'p2'])

    const field = await screen.findByRole('combobox', { name: 'Move to…' })
    await user.type(field, 'Bož')
    await user.click(await screen.findByRole('option', { name: /Božena/ }))

    expect(await screen.findByText('Moved 1 photo to Božena.')).toBeInTheDocument()
    expect(screen.getByText(/1 photo skipped/)).toBeInTheDocument()
    expect(assignMock).toHaveBeenCalledTimes(1)
  })

  it('reports a failed photo without giving up on the rest', async () => {
    facesMock.mockImplementation((photoUid: string) =>
      photoUid === 'p1'
        ? Promise.reject(new Error('boom'))
        : Promise.resolve(faces(photoUid, 'su_a')),
    )
    const user = userEvent.setup()
    const { onMoved } = renderModal(['p1', 'p2'])

    const field = await screen.findByRole('combobox', { name: 'Move to…' })
    await user.type(field, 'Bož')
    await user.click(await screen.findByRole('option', { name: /Božena/ }))

    expect(await screen.findByText(/1 photo could not be moved/)).toBeInTheDocument()
    expect(screen.getByText('Moved 1 photo to Božena.')).toBeInTheDocument()
    expect(onMoved).toHaveBeenCalledWith(expect.objectContaining({ photos: 1, failed: 1 }))
  })

  it('does not offer moving the photos to the person they are already filed under', async () => {
    facesMock.mockResolvedValue(faces('p1', 'su_a'))
    const user = userEvent.setup()
    renderModal(['p1'])

    const field = await screen.findByRole('combobox', { name: 'Move to…' })
    await user.type(field, 'Anna')

    // Only the "create Anna" row is left, never the current person as an option.
    expect(screen.queryByRole('option', { name: /^Anna$/ })).not.toBeInTheDocument()
  })
})
