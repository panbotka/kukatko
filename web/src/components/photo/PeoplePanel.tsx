import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseFacesResult } from '../../hooks/useFaces'
import { padBbox, squareCrop } from '../../lib/faceGeometry'
import { isNamed } from '../../lib/faceState'
import { type FaceView } from '../../services/people'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'
import { FaceCrop } from '../people/FaceCrop'

/**
 * The edge length of a chip's face crop, in CSS pixels. It is sized to the chip's
 * text rather than to a portrait: the crop is an identity cue beside the name, not
 * a picture in its own right.
 */
const CHIP_FACE_SIZE = 24

/**
 * How much context a chip's crop keeps around the face box. Tighter than the
 * people grid's 30 %: at 24px there is no room to spend on shoulders, and the
 * name is right there beside it doing the naming.
 */
const CHIP_FACE_PADDING = 0.15

/** Props for {@link PeoplePanel}. */
export interface PeoplePanelProps {
  /** The photo whose faces these are — the crops are cut from its thumbnail. */
  photoUid: string
  /** The face state machine shared with the on-image overlay ({@link UseFacesResult}). */
  faces: UseFacesResult
  /** Whether the current user may name/clear people (editor/admin). */
  canWrite: boolean
  /**
   * True while a neighbour photo is loading: the faces belong to the target uid,
   * not the photo still on screen, so the chips are held back to a spinner rather
   * than showing another photo's people.
   */
  loading?: boolean
  /**
   * Called with a face's `face_index` when an editor clicks its chip: the page
   * shows the faces panel and selects that face there. Assignment lives in exactly
   * one place, and these chips are the way to reach it without knowing about `m`.
   */
  onEditFace: (faceIndex: number) => void
}

/**
 * The People sub-block of the Organize card: the photo's detected faces as person
 * chips (rose, like every other person chip in the app), reusing the same
 * {@link useFaces} state machine that drives the on-image overlay. It answers "who
 * is in this photo" without turning the face boxes on — they are off by default —
 * and an editor's click on a chip opens the faces panel at that face. Named faces
 * are rose chips, unassigned detections neutral chips an editor can still name; a
 * viewer sees only the named people, read-only.
 *
 * Each chip carries a crop of its own face, so "who is on this photo" is answered
 * by looking rather than by reading — and an unnamed detection stops being an
 * anonymous "Face 2" the reader has to open the panel to identify. The crop is cut
 * from the photo's cached thumbnail in the browser (see {@link FaceCrop}); it is
 * dropped when the frame is unknown, which is only ever while loading.
 *
 * Chips are numbered by position, matching the numbers on the boxes and in the
 * faces panel: `face_index` is negative for markers with no detected face.
 */
export function PeoplePanel({
  photoUid,
  faces,
  canWrite,
  loading = false,
  onEditFace,
}: PeoplePanelProps) {
  const { t } = useTranslation()
  const busyLoading = loading || faces.status === 'loading'
  const selected = faces.selected
  const frame = faces.frame

  /**
   * The chip's leading glyph: a crop of the actual face where the geometry allows
   * one, and the generic person icon where it does not — a chip always has
   * something in that slot, so the row never jumps as the frame arrives.
   */
  const faceGlyph = (face: FaceView) => {
    if (frame === null) {
      return <Icon name={ENTITY_STYLE.person.icon} />
    }
    return (
      <FaceCrop
        photoUid={photoUid}
        crop={squareCrop(padBbox(face.bbox, CHIP_FACE_PADDING), frame)}
        frame={frame}
        // The chip's own text names the person; the crop showing the same name
        // again would only make a screen reader say it twice.
        label=""
        size={CHIP_FACE_SIZE}
        className="rounded-circle flex-shrink-0"
      />
    )
  }
  // Viewers only care about the people who have a name; an editor also sees the
  // unnamed detections so they can name them.
  const visible = faces.faces
    .map((face, position) => ({ face, number: position + 1 }))
    .filter(({ face }) => canWrite || isNamed(face))

  return (
    <div>
      <div className="small text-secondary mb-1">{t('photo.organize.people')}</div>

      {faces.actionError && (
        <Alert variant="danger" className="py-2 small">
          {t('faces.assignError')}
        </Alert>
      )}

      {busyLoading ? (
        <Spinner animation="border" size="sm" role="status">
          <span className="visually-hidden">{t('faces.loading')}</span>
        </Spinner>
      ) : (
        <div className="d-flex flex-wrap gap-2 mb-2">
          {visible.length === 0 && (
            <span className="text-secondary small">{t('photo.organize.noPeople')}</span>
          )}
          {visible.map(({ face, number }) => {
            const named = isNamed(face)
            const text = named ? (face.subject_name ?? '') : t('faces.unnamed', { index: number })
            const chipClass = `badge rounded-pill d-inline-flex align-items-center gap-1 ${
              named ? ENTITY_STYLE.person.className : 'text-bg-secondary'
            }`
            if (!canWrite) {
              return (
                <span key={face.face_index} className={chipClass}>
                  {faceGlyph(face)}
                  {text}
                </span>
              )
            }
            return (
              <button
                key={face.face_index}
                type="button"
                className={`${chipClass} border-0`}
                aria-pressed={selected?.face_index === face.face_index}
                aria-label={
                  named
                    ? t('photo.organize.editPerson', { name: text })
                    : t('photo.organize.namePerson', { index: number })
                }
                onClick={() => {
                  onEditFace(face.face_index)
                }}
              >
                {faceGlyph(face)}
                {text}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
