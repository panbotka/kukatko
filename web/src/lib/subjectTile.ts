import { displayFrame, padBbox, squareCrop, type Frame } from './faceGeometry'

import { type Bbox, type SubjectCount } from '../services/people'

/**
 * How much context the tile crop keeps around the face box, as a fraction of the
 * box's own size on each side. A tile cropped tight to the detector's box is a
 * nose and two eyes with the chin cut off — recognisable to a machine, not to a
 * person — so the crop is padded out to hair, ears and a bit of shoulder before
 * it is squared off. It matches the outlier review's default for the same reason.
 */
const TILE_FACE_PADDING = 0.3

/** The subject's tile shows the cover photo the user chose, whole. */
export interface CoverImage {
  kind: 'cover'
  /** The photo to render. */
  photoUid: string
}

/** The subject's tile shows an automatic crop of their face. */
export interface FaceImage {
  kind: 'face'
  /** The photo the face was found on. */
  photoUid: string
  /** The squared, padded crop region, normalised against the display frame. */
  crop: Bbox
  /** The source photo's displayed frame, for the container's aspect maths. */
  frame: Frame
}

/** The subject has nothing to show; the caller keeps its placeholder. */
export interface NoImage {
  kind: 'none'
}

/** What a subject tile should render, as decided by {@link subjectTileImage}. */
export type SubjectTileImage = CoverImage | FaceImage | NoImage

/**
 * Decides what a subject's tile shows, in strict order of who gets to decide:
 *
 * 1. An explicitly set `cover_photo_uid` wins outright. Somebody chose that photo
 *    for this person; a guess must never overrule a decision, so the cover is
 *    shown whole and is not second-guessed by cropping it.
 * 2. Otherwise the subject's automatically picked face (`cover_face`, chosen
 *    server-side — see `listSubjectsSQL` for which face and why) is padded for
 *    context and squared against the photo's display frame, so the tile shows the
 *    person rather than a grey box.
 * 3. Failing both, nothing. A subject with no usable face keeps the placeholder:
 *    an app that invents a face for someone is worse than one that admits it has
 *    none.
 *
 * A cover face naming a frame with no dimensions is unusable — the crop maths
 * divides by them — and counts as nothing.
 */
export function subjectTileImage(subject: SubjectCount): SubjectTileImage {
  const cover = subject.cover_photo_uid
  if (cover !== undefined && cover !== '') {
    return { kind: 'cover', photoUid: cover }
  }

  const face = subject.cover_face
  if (face === undefined || face.photo_uid === '') {
    return { kind: 'none' }
  }
  const frame = displayFrame(face.width, face.height, face.orientation)
  if (frame.width <= 0 || frame.height <= 0) {
    return { kind: 'none' }
  }

  const bbox: Bbox = [face.x, face.y, face.w, face.h]
  return {
    kind: 'face',
    photoUid: face.photo_uid,
    crop: squareCrop(padBbox(bbox, TILE_FACE_PADDING), frame),
    frame,
  }
}
