import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ToggleButton from 'react-bootstrap/ToggleButton'
import ToggleButtonGroup from 'react-bootstrap/ToggleButtonGroup'
import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'
import { ApiError } from '../../services/auth'
import {
  addRelation,
  type AddRelationRequest,
  type NewPerson,
  type Relative,
} from '../../services/family'
import Modal from '../Modal'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/**
 * The four relations the strip draws, which are also the four a `+` can record —
 * and, since the backend grew the `sibling` role, the four roles the endpoint
 * itself accepts.
 *
 * A sibling is still derived rather than stored: recording one makes the person
 * another child of the family the subject is a child in. What changed is that the
 * family may now be created on the spot with no parents in it, so a sibling can
 * be recorded for somebody whose parents the library has never heard of.
 */
export type RelationKind = 'parent' | 'sibling' | 'partner' | 'child'

/** The order the picker offers the relations in, matching the strip's rows. */
const KINDS: readonly RelationKind[] = ['parent', 'sibling', 'partner', 'child']

/**
 * Who the relation is to: somebody the library already knows, or somebody it is
 * about to. The dialog treats the two alike everywhere except in the one field
 * of the request they produce — `subject_uid` against `new_subject`.
 */
type Target = { existing: string; person?: never } | { existing?: never; person: NewPerson }

/** Props for {@link AddRelationModal}. */
export interface AddRelationModalProps {
  /** The person the relation is recorded on. */
  subjectUid: string
  /** Their name, for the dialog's copy. */
  subjectName: string
  /**
   * The subject's recorded parents, which is what the dialog names when it
   * explains where a sibling will hang. An empty list is not a dead end any more:
   * the two then share a family with no parents in it.
   */
  parents: readonly Relative[]
  /** Which row's `+` opened the dialog; the picker starts there. */
  kind: RelationKind
  /** Whether the dialog is visible. */
  show: boolean
  /** Dismisses the dialog. */
  onHide: () => void
  /** Called after every successful add, so the strip refetches its relations. */
  onAdded: () => void
}

/**
 * "Add a relative": the dialog behind every `+` of the family strip, and the
 * half of the family tree that decides whether the tree ever gets filled in.
 *
 * Drawing a tree is an afternoon; recording the relationships of a hundred and
 * eighteen people is several evenings of clicking, so this dialog is built for
 * the long sitting rather than for the single use:
 *
 * - **it stays open.** Each add clears the field, keeps the chosen relation and
 *   lists what has been recorded so far, so a person's four children are four
 *   names typed in a row rather than four trips through a dialog;
 * - **it creates inline.** A name the library does not know is offered for
 *   creation and posted as `new_subject`, which the backend creates and relates
 *   in one transaction. Without it every great-grandmother is a trip to another
 *   screen and back, once per person, for every generation nobody wrote down;
 * - **it is keyboard-operable throughout.** The field takes focus on open, the
 *   picker is a radio group the arrow keys move through, and the field's own
 *   Up/Down/Enter pick or create without reaching for the mouse;
 * - **it searches without diacritics.** `AddAutocomplete` folds accents and case,
 *   so `necasova` finds `Nečasová` — the names in this library are full of
 *   diacritics and nobody types them into a search field.
 *
 * The new person takes the plain defaults a subject gets anywhere else; years,
 * nickname and notes are edited on their own page, which is where that belongs.
 */
export function AddRelationModal({
  subjectUid,
  subjectName,
  parents,
  kind,
  show,
  onHide,
  onAdded,
}: AddRelationModalProps) {
  const { t } = useTranslation()
  const { subjects, loading } = useSubjects()
  const [chosen, setChosen] = useState<RelationKind>(kind)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // What this sitting has recorded, so a reader who has typed six names can see
  // the six rather than an empty field that gives no account of itself.
  const [added, setAdded] = useState<string[]>([])

  // The page keeps the dialog mounted for as long as it is open and drops it
  // afterwards, but the row a reader opens it from can change between openings —
  // so an opening resets the picker to the row that asked for it, and clears
  // what the previous one recorded.
  const [wasOpen, setWasOpen] = useState(show)
  if (show !== wasOpen) {
    setWasOpen(show)
    if (show) {
      setChosen(kind)
      setError(null)
      setAdded([])
    }
  }

  /** Turns a failed request into a sentence in the reader's language. */
  function describe(err: unknown): string {
    if (err instanceof ApiError && err.status === 409) {
      // The state of the tree is in the way: a cycle, or a person who is already
      // a child in another family. The same request would have been accepted
      // against different rows, so it is not the reader who got it wrong.
      return t('family.add.conflict')
    }
    if (err instanceof ApiError && err.status === 400) {
      return t('family.add.invalid')
    }
    return t('family.add.failed')
  }

  /** Runs one add with the busy/error plumbing; reports whether it landed. */
  async function record(target: Target, name: string): Promise<boolean> {
    setBusy(true)
    setError(null)
    try {
      // All four rows are one request in one audited transaction, the sibling
      // included: the backend puts the pair in the family the subject is a child
      // in, or creates a parentless one for them. Walking the parents from here,
      // as this dialog used to, cost a request per parent and could not record a
      // sibling at all for somebody whose parents nobody wrote down.
      const request: AddRelationRequest =
        target.existing === undefined
          ? { role: chosen, new_subject: target.person }
          : { role: chosen, subject_uid: target.existing }
      await addRelation(subjectUid, request)
      setAdded((previous) => [...previous, name])
      onAdded()
      return true
    } catch (err: unknown) {
      setError(describe(err))
      return false
    } finally {
      setBusy(false)
    }
  }

  // Relating a person to themselves is the one thing the field must not offer;
  // everything else the backend judges against the rows.
  const options = subjects.filter((candidate) => candidate.uid !== subjectUid)

  // `fullscreen="sm-down"` like the app's other pickers: on a phone the field,
  // its suggestion list and the on-screen keyboard together need the whole
  // screen, and a third of it left one clipped half-row of suggestions. The
  // dialog stays `centered` and `scrollable` on desktop, where the list is a
  // fixed overlay that neither the body's `overflow: auto` clips nor the
  // dialog's own height has to grow for — so it does not resize per keystroke.
  return (
    <Modal show={show} onHide={onHide} centered scrollable fullscreen="sm-down">
      <Modal.Header closeButton>
        <Modal.Title>{t('family.add.title', { name: subjectName })}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <span className="form-label d-block mb-1" id="relation-kind-heading">
          {t('family.add.kindHeading')}
        </span>
        <ToggleButtonGroup
          type="radio"
          name="relation-kind"
          value={chosen}
          aria-labelledby="relation-kind-heading"
          className="mb-3 flex-wrap"
          onChange={(value: RelationKind) => {
            setChosen(value)
            setError(null)
          }}
        >
          {KINDS.map((option) => (
            <ToggleButton
              key={option}
              id={`relation-kind-${option}`}
              value={option}
              variant="outline-secondary"
              size="sm"
              className="kukatko-tap-target"
            >
              {t(`family.role.${option}`)}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>

        {chosen === 'sibling' && (
          <p className="small text-secondary">
            {parents.length > 0
              ? t('family.add.siblingHint', {
                  parents: parents.map((parent) => parent.name).join(', '),
                })
              : t('family.add.siblingHintNoParents')}
          </p>
        )}

        {added.length > 0 && (
          <Alert variant="success" className="py-2 small">
            {t('family.add.recorded', { names: added.join(', ') })}
          </Alert>
        )}

        {error !== null && (
          <Alert variant="danger" className="py-2 small">
            {error}
          </Alert>
        )}

        <AddAutocomplete
          id="add-relation-person"
          label={t('family.add.pickLabel')}
          autoFocus
          disabled={busy || loading}
          options={options.map((candidate) => ({
            uid: candidate.uid,
            label: candidate.name,
            // Searchable by nickname too: in a village archive the handle is
            // very often the only name anybody remembers.
            alias: candidate.nickname,
            hint: String(candidate.photo_count),
          }))}
          onAdd={(uid) => {
            const picked = options.find((candidate) => candidate.uid === uid)
            void record({ existing: uid }, picked?.name ?? '')
          }}
          onCreate={(name) => record({ person: { name } }, name)}
        />
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={busy}>
          {t('family.add.close')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
