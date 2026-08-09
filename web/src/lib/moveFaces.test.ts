import { describe, expect, it } from 'vitest'

import { type FaceView } from '../services/people'

import { moveRequests } from './moveFaces'

/** A face view as `GET /photos/{uid}/faces` returns one. */
function face(overrides: Partial<FaceView>): FaceView {
  return {
    face_index: 0,
    bbox: [0.1, 0.1, 0.2, 0.2],
    det_score: 0.9,
    action: 'already_done',
    suggestions: [],
    ...overrides,
  }
}

describe('moveRequests', () => {
  it('reassigns every marker the person holds on the photo', () => {
    const faces = [
      face({ face_index: 0, marker_uid: 'mk1', subject_uid: 'su_a' }),
      face({ face_index: 1, marker_uid: 'mk2', subject_uid: 'su_a' }),
    ]

    expect(moveRequests(faces, 'su_a', { subjectUid: 'su_b' })).toEqual([
      { action: 'assign_person', marker_uid: 'mk1', subject_uid: 'su_b', face_index: 0 },
      { action: 'assign_person', marker_uid: 'mk2', subject_uid: 'su_b', face_index: 1 },
    ])
  })

  it('leaves everyone else on the photo alone', () => {
    const faces = [
      face({ face_index: 0, marker_uid: 'mk1', subject_uid: 'su_other' }),
      face({ face_index: 1, marker_uid: 'mk2' }),
      face({ face_index: 2, marker_uid: 'mk3', subject_uid: 'su_a' }),
    ]

    const requests = moveRequests(faces, 'su_a', { subjectUid: 'su_b' })

    expect(requests).toHaveLength(1)
    expect(requests[0].marker_uid).toBe('mk3')
  })

  it('skips a face with no marker, since there is nothing to reassign', () => {
    const faces = [
      face({ face_index: 0, subject_uid: 'su_a' }),
      face({ face_index: 1, subject_uid: 'su_a', marker_uid: '' }),
    ]

    expect(moveRequests(faces, 'su_a', { subjectUid: 'su_b' })).toEqual([])
  })

  it('moves a marker no detection claimed without naming a face slot', () => {
    // The backend hands unmatched markers a negative index (they name no face
    // row); sending it would ask the backend to cache the link onto nothing.
    const faces = [face({ face_index: -1, marker_uid: 'mk9', subject_uid: 'su_a' })]

    expect(moveRequests(faces, 'su_a', { subjectUid: 'su_b' })).toEqual([
      { action: 'assign_person', marker_uid: 'mk9', subject_uid: 'su_b' },
    ])
  })

  it('names a brand-new person by name, for the backend to find or create', () => {
    const faces = [face({ marker_uid: 'mk1', subject_uid: 'su_a' })]

    expect(moveRequests(faces, 'su_a', { subjectName: 'Ludmila' })).toEqual([
      { action: 'assign_person', marker_uid: 'mk1', subject_name: 'Ludmila', face_index: 0 },
    ])
  })
})
