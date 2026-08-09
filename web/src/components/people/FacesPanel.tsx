import { useEffect, useMemo, useRef } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'
import { type UseFacesResult } from '../../hooks/useFaces'
import { padBbox, squareCrop } from '../../lib/faceGeometry'
import { type FaceState, faceState, hasEmbedding } from '../../lib/faceState'
import { approximateAge } from '../../lib/lifeYears'
import { type FaceView } from '../../services/people'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'
import { FaceAssignPanel } from './FaceAssignPanel'
import { FaceCrop } from './FaceCrop'

/**
 * The edge length of a row's face crop, in CSS pixels. Bigger than the 24px chip
 * in `PeoplePanel`: this is the screen where a face gets its name, so the crop has
 * to be recognisable on its own rather than merely confirm a name written beside
 * it — but it still has to leave the row a row, not a tile.
 */
const ROW_FACE_SIZE = 44

/**
 * How much context a row's crop keeps around the face box. Between the chip's
 * 15 % and the review card's 30 %: at 44px a little hair and chin is what turns a
 * crop into a person, and the row has the width to spend on it.
 */
const ROW_FACE_PADDING = 0.25

/** Props for {@link FacesPanel}. */
export interface FacesPanelProps {
  /** The photo the faces are on — the row crops are cut from its thumbnail. */
  photoUid: string
  /** The faces state machine, shared with the overlay drawn on the photo. */
  faces: UseFacesResult
  /** Whether the viewer may assign people (editors and admins). */
  canWrite: boolean
  /**
   * The photo's capture time (ISO), or undefined when it has none. With it — and
   * a named person whose birth year is known — each row can say roughly how old
   * they were here. Without it the rows simply carry no age.
   */
  takenAt?: string
  /** The `face_index` hovered on the photo, or null. Highlights its row. */
  hovered: number | null
  /**
   * Reports the hovered — or focused — row, so the overlay can highlight its box
   * and draw its name. Focus counts because a finger never hovers and neither
   * does the keyboard.
   */
  onHover: (faceIndex: number | null) => void
  /** Closes the panel (same as toggling faces off). */
  onClose: () => void
}

/** Chip style per naming state — the same two colours the boxes on the photo use. */
const STATE_CHIP: Record<FaceState, string> = {
  named: 'text-bg-success',
  unnamed: 'text-bg-warning',
}

/**
 * The faces sidebar of the photo detail: one row per detected face, and the
 * assignment controls for the selected one. It appears beside the photo (on a
 * phone, in a bottom sheet under it — the photo has to stay on screen, or the
 * rows have no boxes left to match) whenever the face boxes are shown, and is the
 * only place people are named — the boxes on the photo and these rows drive the
 * same selection, so clicking either one gets you there.
 *
 * **Every row leads with a crop of its own face.** It used to lead with the words
 * „Obličej #4", which asked the reader to find a tiny numeric badge somewhere on
 * the photo before they could name anybody — on a fifteen-person group photo that
 * is the whole job. The number survives as a small badge, matching the one drawn
 * on the box, but it is now the cross-reference rather than the identity.
 *
 * Rows are numbered by position, matching the number drawn on each box: `face_index`
 * cannot be used, as markers with no detected face carry negative ones. That
 * position is reading order (`useFaces` sorts on arrival), so #1 is the leftmost
 * face of the top row and the rows run down the photo the way the eye does.
 *
 * A row is either named or not — the same two states the boxes use. What it does
 * carry beyond that is a small mark when the face has no embedding
 * ({@link hasEmbedding}): that one is worth knowing, because such a face can only
 * ever be named here by hand — no suggestion, no similarity search and no review
 * game will bring it up.
 *
 * Pointing at a row lights its box on the photo (`onHover`), and the pairing is
 * reported from **focus** as well, so tabbing through the rows walks the boxes too.
 *
 * A viewer may not name anybody, so their rows are plain rows rather than dead
 * buttons — the app-wide rule (see `ReasonedButton`) is that a role never leaves
 * a greyed-out control behind. They still light their box on hover, because
 * „which one is that?" is a viewer's question too; a one-line note above the list
 * says why there is nothing here to press.
 */
export function FacesPanel({
  photoUid,
  faces,
  canWrite,
  takenAt,
  hovered,
  onHover,
  onClose,
}: FacesPanelProps) {
  const { t } = useTranslation()
  const { subjects, loading: subjectsLoading } = useSubjects()

  const selected = faces.selected
  const frame = faces.frame
  const listRef = useRef<HTMLDivElement>(null)

  // Birth year by subject uid, so a row can date the face it shows without a
  // lookup per render. The subject list is already loaded here for the assign
  // control, so this costs nothing extra.
  const birthYears = useMemo(() => {
    const byUID = new Map<string, number>()
    for (const subject of subjects) {
      if (subject.birth_year !== null) {
        byUID.set(subject.uid, subject.birth_year)
      }
    }
    return byUID
  }, [subjects])

  /**
   * Roughly how old the named person was on this photograph, or null when it
   * cannot be said — the face is unnamed, nobody recorded a birth year, the
   * photo has no date, or the two do not admit an age (see `approximateAge`).
   */
  const faceAge = (face: FaceView): number | null => {
    const birthYear = face.subject_uid === undefined ? undefined : birthYears.get(face.subject_uid)
    return approximateAge(takenAt, birthYear)
  }

  /**
   * The row's leading picture: a crop of the actual face, or the generic person
   * icon while the frame is unknown (only ever during loading) — the slot is
   * always filled, so the list does not jump as the frame arrives.
   */
  const faceGlyph = (face: FaceView) => {
    if (frame === null) {
      return <Icon name={ENTITY_STYLE.person.icon} />
    }
    return (
      <FaceCrop
        photoUid={photoUid}
        crop={squareCrop(padBbox(face.bbox, ROW_FACE_PADDING), frame)}
        frame={frame}
        // The row's own label already says whose face this is; a second
        // announcement of the same name is noise.
        label=""
        size={ROW_FACE_SIZE}
        className="rounded-circle flex-shrink-0"
      />
    )
  }

  // Tapping a box on the photo selects a face from the other side of the pair,
  // and on a phone this list scrolls (and lives in a drawer below the photo) —
  // so the row it selected can easily sit out of sight. Bring it back, which is
  // what makes the box↔row pairing legible on touch, where the hover highlight
  // that does it on a mouse never fires. `nearest` leaves an already-visible row
  // (a row the user just clicked here) exactly where it is.
  const selectedIndex = selected?.face_index
  useEffect(() => {
    if (selectedIndex === undefined) {
      return
    }
    listRef.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [selectedIndex])

  return (
    <Card>
      <Card.Header className="d-flex justify-content-between align-items-center">
        <span>{t('faces.count', { count: faces.faces.length })}</span>
        <Button
          variant="link"
          size="sm"
          className="p-0 text-reset text-decoration-none"
          aria-label={t('faces.panel.closePanel')}
          onClick={onClose}
        >
          <Icon name="x-lg" />
        </Button>
      </Card.Header>
      {/* The height cap is a class in `viewer.css`, not an inline style: on a
          phone the drawer is a short bottom sheet that scrolls itself, and the
          sheet has to be able to LIFT the cap — an inline style outranks the
          media query that would. */}
      <Card.Body className="kk-viewer__panel-scroll">
        {faces.actionError && <Alert variant="danger">{t('faces.assignError')}</Alert>}
        {faces.status === 'loading' && (
          <Spinner animation="border" size="sm" role="status">
            <span className="visually-hidden">{t('faces.loading')}</span>
          </Spinner>
        )}
        {faces.status === 'error' && <Alert variant="danger">{t('faces.error')}</Alert>}

        {/* A viewer's rows are plain rows, not buttons — but they sit in a panel
            whose whole point is naming, and an unexplained list of things that
            do not respond to a click reads as a broken panel. So the panel says
            once, in words, what the missing controls would have said one by one.
            The note is placed above the list because that is where a reader
            starts, and it is not an `Alert`: nothing is wrong. */}
        {!canWrite && faces.faces.length > 0 && (
          <p className="text-secondary small mb-2">{t('faces.viewerNote')}</p>
        )}

        <div className="list-group list-group-flush" ref={listRef}>
          {faces.faces.map((face, position) => {
            const state = faceState(face)
            const number = position + 1
            const isSelected = selected?.face_index === face.face_index
            const embedded = hasEmbedding(face)
            const chip = state === 'named' ? (face.subject_name ?? '') : t('faces.state.unnamed')
            const age = faceAge(face)
            // An editor's row is a button whose aria-label replaces its content,
            // so the age has to travel inside that label or a screen reader
            // never hears it. Composed here rather than as a second key: it is
            // the same sentence with one more clause.
            const rowName = age === null ? chip : `${chip}, ${t('subject.age', { count: age })}`
            const row = (
              <>
                {/* The cross-reference to the photo, drawn like the badge on the
                    box so the two read as the same mark. */}
                <span className="badge text-bg-dark flex-shrink-0">{number}</span>
                {faceGlyph(face)}
                <span className={`badge text-truncate ${STATE_CHIP[state]}`}>{chip}</span>
                {/* How old they were here, once somebody has recorded when this
                    person was born. It rides beside the name rather than in it,
                    because the name is what the row is keyed on and the age is
                    what this particular photograph adds to it. */}
                {age !== null && (
                  <span className="small opacity-75 flex-shrink-0">
                    {t('subject.age', { count: age })}
                  </span>
                )}
                {!embedded && (
                  // The row of a `canWrite` viewer is a button whose aria-label
                  // replaces its content, so it says this in its own label instead;
                  // the hidden text is what a viewer's plain row announces.
                  // `opacity` rather than `text-secondary`: it stays muted on a
                  // plain row and still legible on the selected (primary) one,
                  // where a fixed secondary grey all but disappears.
                  <span className="opacity-75" title={t('faces.noEmbedding.mark')}>
                    <Icon name="slash-circle" />
                    <span className="visually-hidden">{t('faces.noEmbedding.mark')}</span>
                  </span>
                )}
              </>
            )

            return (
              <div key={face.face_index} data-selected={isSelected ? 'true' : undefined}>
                {canWrite ? (
                  <button
                    type="button"
                    className={`list-group-item list-group-item-action d-flex align-items-center gap-2 ${
                      isSelected ? 'active' : ''
                    } ${hovered === face.face_index && !isSelected ? 'bg-body-secondary' : ''}`}
                    aria-pressed={isSelected}
                    aria-label={
                      embedded
                        ? t('faces.row.select', { number, name: rowName })
                        : t('faces.row.selectNoEmbedding', { number, name: rowName })
                    }
                    data-face-state={state}
                    data-embedding={embedded ? undefined : 'none'}
                    onClick={() => {
                      faces.select(isSelected ? null : face.face_index)
                    }}
                    onMouseEnter={() => {
                      onHover(face.face_index)
                    }}
                    onMouseLeave={() => {
                      onHover(null)
                    }}
                    // A finger never hovers and the keyboard never will either, so
                    // the box↔row pairing is reported from focus as well — the same
                    // way `FaceOverlay` reports it from the other side.
                    onFocus={() => {
                      onHover(face.face_index)
                    }}
                    onBlur={() => {
                      onHover(null)
                    }}
                  >
                    {row}
                  </button>
                ) : (
                  // A viewer's row is inert, but still pairs with the photo on
                  // hover: with the name labels on the boxes shown one at a time,
                  // this is how a viewer asks "which one is that?".
                  <div
                    className={`list-group-item d-flex align-items-center gap-2 ${
                      hovered === face.face_index ? 'bg-body-secondary' : ''
                    }`}
                    data-face-state={state}
                    data-embedding={embedded ? undefined : 'none'}
                    onMouseEnter={() => {
                      onHover(face.face_index)
                    }}
                    onMouseLeave={() => {
                      onHover(null)
                    }}
                  >
                    {row}
                  </div>
                )}

                {canWrite && isSelected && (
                  <FaceAssignPanel
                    // Remounting on selection change resets the reassign mode and
                    // the typed name, so no state leaks from the previous face.
                    key={face.face_index}
                    face={face}
                    subjects={subjects}
                    subjectsLoading={subjectsLoading}
                    busy={faces.busy}
                    onAcceptSuggestion={(subject) => {
                      faces.acceptSuggestion(face, subject)
                    }}
                    onAssignName={(name) => {
                      faces.assignName(face, name)
                    }}
                    onUnassign={() => {
                      faces.unassign(face)
                    }}
                    onClose={() => {
                      faces.select(null)
                    }}
                  />
                )}
              </div>
            )
          })}
        </div>
      </Card.Body>
    </Card>
  )
}
