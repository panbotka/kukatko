import { type Candidate } from '../../services/faces'

import { type ReviewStageProps } from './ReviewStage'

/**
 * The preview the lightbox asks for. `fit_*` (the whole frame) because the face
 * rectangle's coordinates are relative to the full photo, and 1280 because the
 * overlay is the whole screen — the same size the review game plays at.
 */
export const CANDIDATE_LIGHTBOX_SIZE = 'fit_1280'

/**
 * The stage for one face candidate: the photo, its catalogue dimensions, the
 * face box to mark and the way out to the photo's own page.
 *
 * It lives here rather than in each page because `/faces`, `/recognition` and a
 * subject page's candidate section all enlarge the very same kind of item — three
 * copies of this would be three chances for one of them to hand the stage a
 * square tile or forget the corner anchor.
 */
export function candidateStage(candidate: Candidate, alt: string): ReviewStageProps {
  return {
    photoUid: candidate.photo.uid,
    fileWidth: candidate.photo.file_width,
    fileHeight: candidate.photo.file_height,
    orientation: candidate.photo.file_orientation ?? 0,
    size: CANDIDATE_LIGHTBOX_SIZE,
    bbox: candidate.bbox.relative,
    href: `/photos/${candidate.photo.uid}`,
    alt,
  }
}
