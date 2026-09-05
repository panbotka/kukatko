import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'
import {
  mergeSubject,
  type MergeResult,
  type Subject,
  type SubjectCount,
} from '../../services/people'
import { Icon } from '../Icon'
import Modal from '../Modal'
import { AddAutocomplete } from '../photo/AddAutocomplete'

import { SubjectSummary } from './SubjectSummary'

/** Props for {@link MergeSubjectModal}. */
export interface MergeSubjectModalProps {
  /** The person being merged away — the one whose page this is. */
  subject: Subject
  /** Whether the dialog is visible. */
  show: boolean
  /** Dismisses the dialog without merging. */
  onHide: () => void
  /** Called with the keeper and the result once the merge has gone through. */
  onMerged: (keeper: SubjectCount, result: MergeResult) => void
}

/**
 * "Merge into another person": the repair for the same person catalogued twice.
 *
 * It is two steps on purpose. First a pick — a typeahead over everyone else in
 * the library, which cannot offer to *create* a person, because merging into
 * somebody who does not exist yet is a rename with extra steps. Then a
 * confirmation that puts the two records side by side, each with its picture and
 * its counts, and says plainly which one survives and that the other is deleted
 * for good. A merge cannot be undone: every marker, every face and every
 * confirmed or rejected guess moves to the keeper and the source's record is
 * gone, so the dialog spends its space on making the choice legible rather than
 * on being quick to dismiss.
 *
 * The current person is filtered out of the picker: merging someone into
 * themselves describes nothing, and the backend refuses it anyway.
 */
export function MergeSubjectModal({ subject, show, onHide, onMerged }: MergeSubjectModalProps) {
  const { t } = useTranslation()
  const { subjects, loading } = useSubjects()
  const [keeper, setKeeper] = useState<SubjectCount | null>(null)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  // The page keeps this dialog mounted between openings, so a pick (or a failure)
  // would otherwise greet the next opening. Re-seed the moment it opens.
  const [wasOpen, setWasOpen] = useState(show)
  if (show !== wasOpen) {
    setWasOpen(show)
    if (show) {
      setKeeper(null)
      setFailed(false)
    }
  }

  const mine = subjects.find((candidate) => candidate.uid === subject.uid)
  const others = subjects.filter((candidate) => candidate.uid !== subject.uid)

  async function merge() {
    if (keeper === null) {
      return
    }
    setBusy(true)
    setFailed(false)
    try {
      const result = await mergeSubject(subject.uid, keeper.uid)
      onMerged(keeper, result)
    } catch {
      setFailed(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      show={show}
      onHide={onHide}
      centered
      scrollable
      // A merge in flight cannot be dismissed by the backdrop or by Escape: it is
      // irreversible, and its outcome is worth waiting for.
      backdrop={busy ? 'static' : true}
      keyboard={!busy}
    >
      <Modal.Header closeButton={!busy}>
        <Modal.Title>{t('subject.merge.title')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {failed && (
          <Alert variant="danger" className="py-2">
            {t('subject.merge.error')}
          </Alert>
        )}
        {keeper === null ? (
          <>
            <p className="small text-secondary">
              {t('subject.merge.pickHint', { name: subject.name })}
            </p>
            <AddAutocomplete
              id="merge-subject-target"
              label={t('subject.merge.pickLabel')}
              disabled={loading}
              options={others.map((candidate) => ({
                uid: candidate.uid,
                label: candidate.name,
                // A merge is decided by which record is the substantial one, and
                // the gallery it opens counts photos — so the hint counts photos.
                hint: String(candidate.photo_count),
              }))}
              onAdd={(uid) => {
                setKeeper(others.find((candidate) => candidate.uid === uid) ?? null)
              }}
            />
          </>
        ) : (
          <>
            <div className="d-flex flex-column gap-3 mb-3">
              {mine !== undefined && (
                <SubjectSummary subject={mine} role={t('subject.merge.roleSource')} />
              )}
              <div className="text-center text-secondary" aria-hidden="true">
                <Icon name="chevron-down" />
              </div>
              <SubjectSummary subject={keeper} role={t('subject.merge.roleKeeper')} />
            </div>
            <Alert variant="warning" className="py-2 mb-0">
              {t('subject.merge.warning', { source: subject.name, keeper: keeper.name })}
            </Alert>
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        {keeper !== null && (
          <Button
            variant="link"
            disabled={busy}
            onClick={() => {
              setKeeper(null)
            }}
          >
            {t('subject.merge.back')}
          </Button>
        )}
        <Button variant="secondary" onClick={onHide} disabled={busy}>
          {t('subject.merge.cancel')}
        </Button>
        <Button
          variant="danger"
          disabled={busy || keeper === null}
          onClick={() => {
            void merge()
          }}
        >
          {t('subject.merge.confirm')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
