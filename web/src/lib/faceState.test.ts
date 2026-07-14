import { describe, expect, it } from 'vitest'

import { type FaceView } from '../services/people'
import { faceState, isNamed } from './faceState'

/** Builds a face view with only the fields the classifier reads. */
function face(overrides: Partial<FaceView>): FaceView {
  return {
    face_index: 0,
    bbox: [0.1, 0.1, 0.2, 0.2],
    det_score: 0.9,
    action: 'create_marker',
    suggestions: [],
    ...overrides,
  }
}

describe('faceState', () => {
  it('calls a face that names a subject assigned', () => {
    expect(faceState(face({ marker_uid: 'mk1', subject_uid: 'su1', subject_name: 'Alice' }))).toBe(
      'assigned',
    )
  })

  it('calls a marker without a person unassigned', () => {
    expect(faceState(face({ marker_uid: 'mk1' }))).toBe('unassigned')
  })

  it('calls a bare detection unmatched', () => {
    expect(faceState(face({}))).toBe('unmatched')
  })

  it('treats an empty name as no name, not as an assignment', () => {
    // The API omits the field, but an optimistic unassign patches it to ''.
    expect(faceState(face({ marker_uid: 'mk1', subject_name: '' }))).toBe('unassigned')
    expect(isNamed(face({ subject_name: '' }))).toBe(false)
  })

  it('reports named faces', () => {
    expect(isNamed(face({ subject_name: 'Alice' }))).toBe(true)
    expect(isNamed(face({}))).toBe(false)
  })
})
