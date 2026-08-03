import { describe, expect, it } from 'vitest'

import { type FaceView } from '../services/people'
import { faceState, hasEmbedding, isNamed } from './faceState'

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
  it('calls a face that names a subject named', () => {
    expect(faceState(face({ marker_uid: 'mk1', subject_uid: 'su1', subject_name: 'Alice' }))).toBe(
      'named',
    )
  })

  it('calls a marker with no person on it unnamed', () => {
    expect(faceState(face({ marker_uid: 'mk1' }))).toBe('unnamed')
  })

  it('calls a bare detection with no marker unnamed too', () => {
    // The marker/no-marker split is the backend's bookkeeping (`create_marker` vs
    // `assign_person`); to the reader both are one click away from a name, so the
    // UI must not tell them apart.
    expect(faceState(face({}))).toBe('unnamed')
    expect(faceState(face({ marker_uid: 'mk1' }))).toBe(faceState(face({})))
  })

  it('treats an empty name as no name, not as an assignment', () => {
    // The API omits the field, but an optimistic unassign patches it to ''.
    expect(faceState(face({ marker_uid: 'mk1', subject_name: '' }))).toBe('unnamed')
    expect(isNamed(face({ subject_name: '' }))).toBe(false)
  })

  it('reports named faces', () => {
    expect(isNamed(face({ subject_name: 'Alice' }))).toBe(true)
    expect(isNamed(face({}))).toBe(false)
  })
})

describe('hasEmbedding', () => {
  it('reads a stored face slot as backed by a vector', () => {
    expect(hasEmbedding(face({ face_index: 0 }))).toBe(true)
    expect(hasEmbedding(face({ face_index: 7 }))).toBe(true)
  })

  it('reads a negative index as a marker with no face row behind it', () => {
    // `facematch` appends markers that matched no stored face under descending
    // negative indexes — those are the ones no similarity search can ever reach.
    expect(hasEmbedding(face({ face_index: -1, marker_uid: 'mk1' }))).toBe(false)
    expect(hasEmbedding(face({ face_index: -2, marker_uid: 'mk2' }))).toBe(false)
  })

  it('is independent of whether the face is named', () => {
    expect(hasEmbedding(face({ face_index: -1, subject_name: 'Alice' }))).toBe(false)
    expect(hasEmbedding(face({ face_index: 0, subject_name: 'Alice' }))).toBe(true)
  })
})
