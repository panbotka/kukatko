import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseFacesResult } from '../../hooks/useFaces'
import { isNamed } from '../../lib/faceState'
import { type FaceView } from '../../services/people'
import { ENTITY_STYLE } from '../entityStyle'
import { FaceCrop } from '../people/FaceCrop'

/**
 * The edge length of a chip's face crop, in CSS pixels. Big enough to recognise a
 * face by: the crop is how the reader answers "who is this", and at the 24 px it
 * used to be it read as a coloured dot beside the text rather than as a person.
 * It also makes the chip itself a comfortable touch target on a phone.
 */
const CHIP_FACE_SIZE = 40

/**
 * How many unnamed faces the panel shows before it folds the rest away.
 *
 * A photograph of an audience detects a crowd — eighteen faces on one concert
 * shot in the library — and each one is a chip nobody will ever name. Listed in
 * full they push the comments, the technical details and everything below them
 * off the panel, so the tail is collapsed behind one control. Named people are
 * never affected: they are the answer to "who is in this photo" and there are
 * never unmanageably many of them.
 */
const UNNAMED_CHIP_LIMIT = 6

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
 * are rose chips, unnamed detections neutral chips an editor can still name; a
 * viewer sees only the named people, read-only.
 *
 * Each chip carries a crop of its own face, so "who is on this photo" is answered
 * by looking rather than by reading — and an unnamed detection stops being an
 * anonymous "Face 2" the reader has to open the panel to identify. The crop is its
 * own small rendition, cut server-side (see {@link FaceCrop}), so a row of chips
 * costs a few kilobytes rather than a copy of the photograph per chip.
 *
 * Chips are numbered by position, matching the numbers on the boxes and in the
 * faces panel: `face_index` is negative for markers with no detected face.
 *
 * A crowd is folded away rather than listed: past {@link UNNAMED_CHIP_LIMIT}
 * unnamed faces the rest sit behind one control that says how many there are and
 * unfolds them in place. Named people are always listed. Nothing is stored — the
 * fold is state of this render of this photograph, and moving to the next photo
 * folds it back up.
 */
export function PeoplePanel({
  photoUid,
  faces,
  canWrite,
  loading = false,
  onEditFace,
}: PeoplePanelProps) {
  const { t } = useTranslation()
  // The unfold belongs to the photograph it was asked for, not to the panel: the
  // panel stays mounted while the reader walks the library, so remembering *which*
  // photo is unfolded is what folds the next one back up, with no effect to run.
  const [unfoldedFor, setUnfoldedFor] = useState<string | null>(null)
  const unfolded = unfoldedFor === photoUid
  const busyLoading = loading || faces.status === 'loading'
  const selected = faces.selected

  /** The chip's leading glyph: a crop of the face the chip is about. */
  const faceGlyph = (face: FaceView) => (
    <FaceCrop
      photoUid={photoUid}
      bbox={face.bbox}
      // The chip's own text names the person; the crop showing the same name
      // again would only make a screen reader say it twice.
      label=""
      size={CHIP_FACE_SIZE}
      className="rounded-circle flex-shrink-0"
    />
  )
  // Viewers only care about the people who have a name; an editor also sees the
  // unnamed detections so they can name them.
  const visible = faces.faces
    .map((face, position) => ({ face, number: position + 1 }))
    .filter(({ face }) => canWrite || isNamed(face))

  // Keep every named person and the first few unnamed detections, in the order
  // the faces come in — a name that sits behind the crowd keeps its place rather
  // than being sorted to the front.
  const unnamedTotal = visible.filter(({ face }) => !isNamed(face)).length
  const unnamedLimit = unfolded ? unnamedTotal : UNNAMED_CHIP_LIMIT
  let unnamedShown = 0
  const shown = visible.filter(({ face }) => {
    if (isNamed(face)) return true
    if (unnamedShown >= unnamedLimit) return false
    unnamedShown += 1
    return true
  })
  const hidden = unnamedTotal - unnamedShown

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
          {shown.length === 0 && (
            <span className="text-secondary small">{t('photo.organize.noPeople')}</span>
          )}
          {shown.map(({ face, number }) => {
            const named = isNamed(face)
            // A named chip says the name; an unnamed one says only its number —
            // the picture is what identifies it, and the number is the tie to the
            // box on the photo and to the row in the faces panel. Spelling
            // "Nepojmenovaný obličej 12" out made every anonymous chip 204 px
            // wide, one per row on the drawer: six of them were six rows of text
            // saying nothing. The full sentence stays as the chip's label, where
            // a screen reader still reads it.
            const text = named
              ? (face.subject_name ?? '')
              : t('photo.organize.faceNumber', {
                  index: number,
                })
            // `ps-1` pulls the pill in around the crop, which is now a portrait
            // rather than a dot and needs no padding of its own on that side.
            const chipClass = `badge rounded-pill d-inline-flex align-items-center gap-2 ps-1 pe-3 ${
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
          {unnamedTotal > UNNAMED_CHIP_LIMIT && (
            /* The control stands where the folded chips would be, so unfolding
               reads as the list growing rather than as a new panel opening. */
            <Button
              variant="outline-secondary"
              size="sm"
              className="rounded-pill"
              aria-expanded={unfolded}
              onClick={() => {
                setUnfoldedFor(unfolded ? null : photoUid)
              }}
            >
              {unfolded
                ? t('photo.organize.foldPeople')
                : t('photo.organize.morePeople', { count: hidden })}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
