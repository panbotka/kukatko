import { describe, expect, it } from 'vitest'

import { type Bbox, type FaceView } from '../services/people'

import { buildAssign } from './faceAssign'

/** A detection with no marker behind it — the common case in this library. */
function bare(): FaceView {
  return {
    face_index: 3,
    bbox: [0.1, 0.2, 0.3, 0.4] as Bbox,
    det_score: 0.9,
    action: 'create_marker',
    suggestions: [],
  }
}

describe('buildAssign', () => {
  it('draws a new marker for a face nothing covers yet', () => {
    expect(buildAssign(bare(), { subject_uid: 'su_a' })).toEqual({
      action: 'create_marker',
      bbox: [0.1, 0.2, 0.3, 0.4],
      face_index: 3,
      subject_uid: 'su_a',
    })
  })

  it('names the marker in place when the face already has one', () => {
    expect(buildAssign({ ...bare(), marker_uid: 'mk_1' }, { subject_uid: 'su_a' })).toEqual({
      action: 'assign_person',
      marker_uid: 'mk_1',
      subject_uid: 'su_a',
    })
  })

  it('treats an empty marker uid as no marker at all', () => {
    // The backend leaves the field empty rather than absent for a face it
    // synthesised no marker for; both mean the same thing here.
    expect(buildAssign({ ...bare(), marker_uid: '' }, { subject_name: 'Alice' })).toEqual({
      action: 'create_marker',
      bbox: [0.1, 0.2, 0.3, 0.4],
      face_index: 3,
      subject_name: 'Alice',
    })
  })

  it('passes a free-text name through for the backend to find or create', () => {
    expect(buildAssign({ ...bare(), marker_uid: 'mk_1' }, { subject_name: 'Alice' })).toEqual({
      action: 'assign_person',
      marker_uid: 'mk_1',
      subject_name: 'Alice',
    })
  })
})
