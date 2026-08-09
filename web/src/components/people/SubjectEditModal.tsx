import { type SyntheticEvent, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { MIN_LIFE_YEAR } from '../../lib/lifeYears'
import {
  type Subject,
  SUBJECT_TYPES,
  type SubjectInput,
  type SubjectType,
  updateSubject,
} from '../../services/people'

/** Props for {@link SubjectEditModal}. */
export interface SubjectEditModalProps {
  /** The subject being edited (provides the initial field values). */
  subject: Subject
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the refreshed subject after a successful save. */
  onSaved: (subject: Subject) => void
}

/**
 * The i18n key of the dialog's inline error. A closed union rather than a bare
 * string so the typed `t()` accepts it: both messages are real keys, and adding
 * a third means adding it here too.
 */
type EditError = 'subject.edit.error' | 'subject.edit.yearsError'

/**
 * A year field's text as the number the API wants: `null` for an empty field
 * (unknown, which is the normal state), the parsed year otherwise. `NaN` for
 * anything that is not a whole number, so {@link lifeYearsValid} can reject it
 * rather than send it.
 */
function parseYear(raw: string): number | null {
  const trimmed = raw.trim()
  if (trimmed === '') {
    return null
  }
  return /^\d+$/.test(trimmed) ? Number(trimmed) : Number.NaN
}

/**
 * Whether a pair of parsed years is worth sending: each known year is a whole
 * number within 1800…this year, and a death does not precede the birth. It is
 * the client-side half of the backend's rule (`people.validateLifeYears`),
 * checked here only so a typo is answered in place instead of by a round trip.
 * The backend stays the authority — this cannot be trusted and is not the reason
 * the rule holds.
 */
function lifeYearsValid(birth: number | null, death: number | null): boolean {
  const thisYear = new Date().getFullYear()
  for (const year of [birth, death]) {
    if (year === null) {
      continue
    }
    if (!Number.isInteger(year) || year < MIN_LIFE_YEAR || year > thisYear) {
      return false
    }
  }
  return birth === null || death === null || death >= birth
}

/**
 * A modal form for editing a subject's name, type, life span and visibility
 * flags. It preserves the existing cover (set elsewhere) and submits the full
 * editable set to `PATCH /subjects/{uid}`, surfacing a validation error inline.
 * Every opening starts from the subject as stored, so a cancelled edit is really
 * discarded.
 *
 * **The birth and death year appear only for a person.** They are what turns a
 * gallery into a life — the header's „1923–1998", the „~23 let" beside a face —
 * and neither reading means anything for a pet or a place. Hiding them does not
 * erase them: whatever the record holds is still submitted, so reclassifying
 * somebody by mistake and changing it back loses nothing. Both fields are
 * clearable; an empty field means „nobody knows", which is the honest state for
 * almost every person in an archive.
 */
export function SubjectEditModal({ subject, show, onHide, onSaved }: SubjectEditModalProps) {
  const { t } = useTranslation()
  const [name, setName] = useState(subject.name)
  const [type, setType] = useState<SubjectType>(subject.type)
  const [favorite, setFavorite] = useState(subject.favorite)
  const [isPrivate, setIsPrivate] = useState(subject.private)
  const [notes, setNotes] = useState(subject.notes)
  // The years live as text, not numbers: an empty field is a real value here
  // („unknown"), and a half-typed „19" must not be coerced into a year.
  const [birthYear, setBirthYear] = useState(yearText(subject.birth_year))
  const [deathYear, setDeathYear] = useState(yearText(subject.death_year))
  const [busy, setBusy] = useState(false)
  // The i18n key of the inline error, or null while there is none.
  const [error, setError] = useState<EditError | null>(null)

  // The page keeps this dialog mounted between openings, so its state outlives a
  // single edit: re-seed every field the moment it opens. Without it a discarded
  // edit would come back in place of the stored value, and the error from a save
  // that failed would greet the user before they typed anything.
  const [wasOpen, setWasOpen] = useState(show)
  if (show !== wasOpen) {
    setWasOpen(show)
    if (show) {
      setName(subject.name)
      setType(subject.type)
      setFavorite(subject.favorite)
      setIsPrivate(subject.private)
      setNotes(subject.notes)
      setBirthYear(yearText(subject.birth_year))
      setDeathYear(yearText(subject.death_year))
      setError(null)
    }
  }

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed === '') {
      setError('subject.edit.error')
      return
    }
    const birth = parseYear(birthYear)
    const death = parseYear(deathYear)
    if (!lifeYearsValid(birth, death)) {
      setError('subject.edit.yearsError')
      return
    }
    const input: SubjectInput = {
      name: trimmed,
      type,
      favorite,
      private: isPrivate,
      notes,
      cover_photo_uid: subject.cover_photo_uid ?? null,
      birth_year: birth,
      death_year: death,
    }
    setBusy(true)
    setError(null)
    try {
      const updated = await updateSubject(subject.uid, input)
      onSaved(updated)
    } catch {
      setError('subject.edit.error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered fullscreen="sm-down">
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>{t('subject.edit.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error !== null && <p className="text-danger small">{t(error)}</p>}
          <Form.Group className="mb-3" controlId="subject-name">
            <Form.Label>{t('subject.edit.name')}</Form.Label>
            <Form.Control
              type="text"
              value={name}
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="subject-type">
            <Form.Label>{t('subject.edit.type')}</Form.Label>
            <Form.Select
              value={type}
              disabled={busy}
              onChange={(event) => {
                setType(event.target.value as SubjectType)
              }}
            >
              {SUBJECT_TYPES.map((value) => (
                <option key={value} value={value}>
                  {t(`subject.type.${value}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
          {type === 'person' && (
            <div className="row g-2 mb-3">
              <div className="col-6">
                <Form.Group controlId="subject-birth-year">
                  <Form.Label>{t('subject.edit.birthYear')}</Form.Label>
                  <Form.Control
                    type="number"
                    inputMode="numeric"
                    min={MIN_LIFE_YEAR}
                    max={new Date().getFullYear()}
                    placeholder={t('subject.edit.yearPlaceholder')}
                    value={birthYear}
                    disabled={busy}
                    onChange={(event) => {
                      setBirthYear(event.target.value)
                    }}
                  />
                </Form.Group>
              </div>
              <div className="col-6">
                <Form.Group controlId="subject-death-year">
                  <Form.Label>{t('subject.edit.deathYear')}</Form.Label>
                  <Form.Control
                    type="number"
                    inputMode="numeric"
                    min={MIN_LIFE_YEAR}
                    max={new Date().getFullYear()}
                    placeholder={t('subject.edit.yearPlaceholder')}
                    value={deathYear}
                    disabled={busy}
                    onChange={(event) => {
                      setDeathYear(event.target.value)
                    }}
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Text>{t('subject.edit.yearsHint')}</Form.Text>
              </div>
            </div>
          )}
          <Form.Check
            type="checkbox"
            id="subject-favorite"
            className="mb-2"
            label={t('subject.edit.favorite')}
            checked={favorite}
            disabled={busy}
            onChange={(event) => {
              setFavorite(event.target.checked)
            }}
          />
          <Form.Check
            type="checkbox"
            id="subject-private"
            className="mb-3"
            label={t('subject.edit.private')}
            checked={isPrivate}
            disabled={busy}
            onChange={(event) => {
              setIsPrivate(event.target.checked)
            }}
          />
          <Form.Group controlId="subject-notes">
            <Form.Label>{t('subject.edit.notes')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={notes}
              disabled={busy}
              onChange={(event) => {
                setNotes(event.target.value)
              }}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={busy}>
            {t('subject.edit.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {t('subject.edit.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

/** A stored year as the text its input shows, empty for an unknown one. */
function yearText(year: number | null): string {
  return year === null ? '' : String(year)
}
