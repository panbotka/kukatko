import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ProgressBar from 'react-bootstrap/ProgressBar'
import { useTranslation } from 'react-i18next'

import { useSubjects } from '../../hooks/useSubjects'
import {
  EMPTY_MOVE_SUMMARY,
  type MoveSummary,
  type MoveTarget,
  moveRequests,
} from '../../lib/moveFaces'
import { assignFace, fetchFaces } from '../../services/people'
import Modal from '../Modal'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/** Props for {@link MoveFacesModal}. */
export interface MoveFacesModalProps {
  /** The person the picked photos are currently tagged with. */
  sourceUid: string
  /** That person's name, for the dialog's copy. */
  sourceName: string
  /** The picked photos, in the order the gallery shows them. */
  photoUids: string[]
  /** Whether the dialog is visible. */
  show: boolean
  /** Dismisses the dialog. */
  onHide: () => void
  /** Called once a run has finished and changed something, so the page reloads. */
  onMoved: (summary: MoveSummary) => void
}

/** How far a run has got, so the dialog can show progress over many photos. */
interface Progress {
  done: number
  total: number
}

/**
 * "Move to another person": the repair for faces filed under the wrong name.
 *
 * A person's gallery is where a mis-assignment is actually noticed — you are
 * looking at somebody else's photo in the middle of theirs — so the fix lives
 * there: pick those photos, name who they really are, and they move. The target
 * may be somebody who does not exist yet; typing a new name creates them, the
 * same way naming a face on the photo page does.
 *
 * The move itself is deliberately *not* a bulk endpoint. Each face goes through
 * `POST /photos/{uid}/faces/assign` — the very write path the photo page and the
 * review game use — so the assignment state machine, the faces cache, the marker's
 * reviewed flag and the audit trail all behave exactly as they do everywhere else,
 * and a half-finished run leaves the photos it did reach correctly assigned rather
 * than in some intermediate state of its own.
 *
 * Photos the person holds no reassignable face on (a marker-less detection, or a
 * box-less tag left by a duplicate merge) are counted as skipped rather than
 * silently dropped: a run that moved fewer photos than were picked has to say so.
 */
export function MoveFacesModal({
  sourceUid,
  sourceName,
  photoUids,
  show,
  onHide,
  onMoved,
}: MoveFacesModalProps) {
  const { t } = useTranslation()
  const { subjects, loading } = useSubjects()
  const [progress, setProgress] = useState<Progress | null>(null)
  const [summary, setSummary] = useState<MoveSummary | null>(null)
  const [targetName, setTargetName] = useState('')

  // The page keeps this dialog mounted between openings; a finished run's summary
  // must not greet the next selection.
  const [wasOpen, setWasOpen] = useState(show)
  if (show !== wasOpen) {
    setWasOpen(show)
    if (show) {
      setProgress(null)
      setSummary(null)
      setTargetName('')
    }
  }

  const others = subjects.filter((candidate) => candidate.uid !== sourceUid)
  const running = progress !== null && summary === null

  async function move(target: MoveTarget, name: string) {
    setTargetName(name)
    setProgress({ done: 0, total: photoUids.length })
    const run = { ...EMPTY_MOVE_SUMMARY }
    for (const [index, photoUid] of photoUids.entries()) {
      // Sequentially, on purpose: a run naming a person who does not exist yet
      // relies on the first assignment creating them and the rest finding them,
      // and a person's photos are a handful, not a library.
      try {
        const faces = await fetchFaces(photoUid)
        const requests = moveRequests(faces.faces, sourceUid, target)
        if (requests.length === 0) {
          run.skipped += 1
        } else {
          for (const request of requests) {
            await assignFace(photoUid, request)
          }
          run.moved += requests.length
          run.photos += 1
        }
      } catch {
        run.failed += 1
      }
      setProgress({ done: index + 1, total: photoUids.length })
    }
    setSummary(run)
    if (run.moved > 0) {
      onMoved(run)
    }
  }

  return (
    // A run in flight cannot be dismissed — not by the backdrop, not by Escape.
    // It is a sequence of writes with no undo, and closing over it would leave
    // the reader with no account of how far it got.
    <Modal
      show={show}
      onHide={onHide}
      centered
      scrollable
      backdrop={running ? 'static' : true}
      keyboard={!running}
    >
      <Modal.Header closeButton={!running}>
        <Modal.Title>{t('subject.move.title')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {summary !== null ? (
          <MoveReport summary={summary} name={targetName} />
        ) : (
          <>
            <p className="small text-secondary">
              {t('subject.move.hint', { count: photoUids.length, name: sourceName })}
            </p>
            {progress === null ? (
              <AddAutocomplete
                id="move-faces-target"
                label={t('subject.move.pickLabel')}
                disabled={loading}
                options={others.map((candidate) => ({
                  uid: candidate.uid,
                  label: candidate.name,
                  hint: String(candidate.photo_count),
                }))}
                onAdd={(uid) => {
                  const picked = others.find((candidate) => candidate.uid === uid)
                  void move({ subjectUid: uid }, picked?.name ?? '')
                }}
                onCreate={(name) => {
                  void move({ subjectName: name }, name)
                  return Promise.resolve(true)
                }}
              />
            ) : (
              <>
                <p className="mb-2">{t('subject.move.running', { name: targetName })}</p>
                <ProgressBar
                  now={progress.total === 0 ? 100 : (progress.done / progress.total) * 100}
                  label={`${String(progress.done)}/${String(progress.total)}`}
                />
              </>
            )}
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={running}>
          {summary === null ? t('subject.move.cancel') : t('subject.move.close')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

/** The account of a finished run: what moved, and what did not and why. */
function MoveReport({ summary, name }: { summary: MoveSummary; name: string }) {
  const { t } = useTranslation()

  return (
    <>
      {/* Photos, not markers: the reader picked photos, so that is the unit the
          account has to be in — even though a photo marking this person twice
          moved two faces. */}
      <p className="mb-2">{t('subject.move.done', { count: summary.photos, name })}</p>
      {summary.skipped > 0 && (
        <Alert variant="secondary" className="py-2 mb-2">
          {t('subject.move.skipped', { count: summary.skipped })}
        </Alert>
      )}
      {summary.failed > 0 && (
        <Alert variant="danger" className="py-2 mb-0">
          {t('subject.move.failed', { count: summary.failed })}
        </Alert>
      )}
    </>
  )
}
