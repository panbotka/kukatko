import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseFacesResult } from '../../hooks/useFaces'
import { isNamed } from '../../lib/faceState'
import {
  attachPerson,
  createSubject,
  detachPerson,
  type FaceView,
  type PhotoSubject,
} from '../../services/people'
import { EntityChip } from '../EntityChip'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'
import { FaceCrop } from '../people/FaceCrop'

import { AttachPersonField } from './AttachPersonField'

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
  /**
   * Who was attached to this media item **by hand** — no face, no box, no crop.
   * The detail response carries them; both mutations here answer with the whole
   * resulting list, so the block never re-reads the photo to redraw itself.
   */
  people: PhotoSubject[]
  /** Whether the current user may name/clear people (editor/admin). */
  canWrite: boolean
  /**
   * Whether a click on a face chip can actually reach the faces panel. It cannot
   * on a photo whose boxes the viewer refuses to draw (a saved crop leaves a
   * frame they were never measured against), and a chip that silently does
   * nothing is worse than a chip that does not offer the click. Defaults true.
   */
  canOpenFaces?: boolean
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
  /** Called with the hand-attached people a mutation answered with. */
  onPeopleChanged: (people: PhotoSubject[]) => void
}

/**
 * The People sub-block of the Organize card: who is on this photo or video, from
 * both of the two ways the app knows.
 *
 * **The faces the detector found** are person chips (rose, like every other person
 * chip in the app) over the same {@link useFaces} state machine that drives the
 * on-image overlay. It answers "who is in this photo" without turning the face
 * boxes on — they are off by default — and an editor's click on a chip opens the
 * faces panel at that face. Named faces are rose chips, unnamed detections neutral
 * chips an editor can still name; a viewer sees only the named people, read-only.
 *
 * Each face chip carries a crop of its own face, so "who is on this photo" is
 * answered by looking rather than by reading — and an unnamed detection stops
 * being an anonymous "Face 2" the reader has to open the panel to identify. The
 * crop is its own small rendition, cut server-side (see {@link FaceCrop}), so a
 * row of chips costs a few kilobytes rather than a copy of the photograph per
 * chip.
 *
 * Chips are numbered by position, matching the numbers on the boxes and in the
 * faces panel: `face_index` is negative for markers with no detected face.
 *
 * **The people somebody attached by hand** are the other half, and they are what
 * makes the block complete: face detection on a video only ever looks at the
 * poster frame, so whoever appears later in the clip is invisible to it, and on a
 * still it misses profiles, backs of heads and faces in a crowd. Such a person has
 * no crop to show, so the chip carries the generic person glyph instead — which is
 * exactly what tells the two kinds apart at a glance — and, for an editor, an X
 * that detaches them again. The add control is offered on every medium and
 * whatever the detector found, because "somebody in the background nobody
 * detected" is not a fallback case, it is the ordinary one.
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
  people,
  canWrite,
  canOpenFaces = true,
  loading = false,
  onEditFace,
  onPeopleChanged,
}: PeoplePanelProps) {
  const { t } = useTranslation()
  // The unfold belongs to the photograph it was asked for, not to the panel: the
  // panel stays mounted while the reader walks the library, so remembering *which*
  // photo is unfolded is what folds the next one back up, with no effect to run.
  const [unfoldedFor, setUnfoldedFor] = useState<string | null>(null)
  // Likewise the open picker: it is about the photo it was opened on, so stepping
  // to the next one puts the panel back to its resting state.
  const [addingFor, setAddingFor] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const unfolded = unfoldedFor === photoUid
  const adding = addingFor === photoUid
  const busyLoading = loading || faces.status === 'loading'
  const selected = faces.selected

  /** Runs one attach/detach with the busy/error plumbing; reports success. */
  async function run(action: () => Promise<PhotoSubject[]>): Promise<boolean> {
    setBusy(true)
    setError(false)
    try {
      // The mutation answers the resulting list, so the block redraws from the
      // reply — a failure leaves the list exactly as it was.
      onPeopleChanged(await action())
      return true
    } catch {
      setError(true)
      return false
    } finally {
      setBusy(false)
    }
  }

  function attach(subjectUid: string): void {
    void run(() => attachPerson(photoUid, subjectUid))
  }

  /**
   * Creates a subject of that name and attaches it in one action. The field only
   * offers this for a name no subject in the library carries, so it never
   * duplicates one; the new person takes the plain defaults the People page gives.
   */
  function createAndAttach(name: string): Promise<boolean> {
    return run(async () => {
      const subject = await createSubject({
        name,
        type: 'person',
        favorite: false,
        private: false,
        notes: '',
        cover_photo_uid: null,
        birth_year: null,
        death_year: null,
      })
      return attachPerson(photoUid, subject.uid)
    })
  }

  function detach(subjectUid: string): void {
    void run(() => detachPerson(photoUid, subjectUid))
  }

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
  // A chip only offers the click when there is somewhere for it to lead.
  const chipsOpenFaces = canWrite && canOpenFaces

  return (
    <div>
      <div className="small text-secondary mb-1">{t('photo.organize.people')}</div>

      {faces.actionError && (
        <Alert variant="danger" className="py-2 small">
          {t('faces.assignError')}
        </Alert>
      )}

      {error && (
        <Alert variant="danger" className="py-2 small">
          {t('photo.organize.error')}
        </Alert>
      )}

      {busyLoading ? (
        <Spinner animation="border" size="sm" role="status">
          <span className="visually-hidden">{t('faces.loading')}</span>
        </Spinner>
      ) : (
        <>
          <div className="d-flex flex-wrap gap-2 mb-2">
            {shown.length === 0 && people.length === 0 && (
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
              if (!chipsOpenFaces) {
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
            {/* Attached by hand: no face to show, so the generic person glyph —
                which is what makes the two kinds of chip tell themselves apart. */}
            {people.map((subject) => (
              <EntityChip
                key={subject.subject_uid}
                kind="person"
                to={`/people/${subject.subject_uid}`}
                remove={
                  canWrite
                    ? {
                        label: t('photo.organize.removePerson', { name: subject.name }),
                        onRemove: () => {
                          detach(subject.subject_uid)
                        },
                      }
                    : undefined
                }
              >
                {subject.name}
              </EntityChip>
            ))}
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

          {canWrite && (
            <>
              <Button
                variant="outline-secondary"
                size="sm"
                className="rounded-pill d-inline-flex align-items-center gap-1"
                aria-expanded={adding}
                disabled={busy}
                onClick={() => {
                  setAddingFor(adding ? null : photoUid)
                }}
              >
                <Icon name="person-plus" />
                {t('photo.organize.addPerson')}
              </Button>
              {adding && (
                <div className="mt-2">
                  <AttachPersonField
                    attached={people.map((subject) => ({
                      uid: subject.subject_uid,
                      name: subject.name,
                    }))}
                    busy={busy}
                    onPick={attach}
                    onCreate={createAndAttach}
                  />
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  )
}
