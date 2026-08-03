import { useCallback, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../../services/auth'
import {
  detachNamelessSubjects,
  fetchNamelessSubjects,
  restoreNamelessSubjects,
  type NamelessReport,
  type NamelessSubject,
  type NamelessUndoFile,
} from '../../services/maintenance'
import { RecordTable, type RecordColumn } from '../RecordTable'

/** Lifecycle of the read-only report. */
type ReportState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; report: NamelessReport }

/** Lifecycle of the destructive detach, including its confirmation step. */
type DetachState =
  | { status: 'idle' }
  | { status: 'confirm' }
  | { status: 'running' }
  | { status: 'error'; message: string }
  | { status: 'done'; undo: NamelessUndoFile }

/** Lifecycle of the undo (replaying an undo file). */
type RestoreState =
  | { status: 'idle' }
  | { status: 'running' }
  | { status: 'error'; message: string }
  | { status: 'done'; queued: number }

/** Renders a subject's creation timestamp as a plain date, or an em dash. */
function createdOn(value: string): string {
  const at = new Date(value)
  return Number.isNaN(at.getTime()) ? '—' : at.toLocaleDateString()
}

/** The found subjects as a table: which one, how big, and when it appeared. */
function NamelessTable({ subjects }: { subjects: NamelessSubject[] }) {
  const { t } = useTranslation()
  const columns: RecordColumn<NamelessSubject>[] = [
    {
      key: 'uid',
      header: t('maintenance.nameless.columns.subject'),
      cell: (subject) => (
        <>
          <span className="font-monospace text-break">{subject.uid}</span>
          <div className="fw-normal text-secondary small">
            {t('maintenance.nameless.emptyName', { slug: subject.slug })}
          </div>
        </>
      ),
    },
    {
      key: 'markers',
      header: t('maintenance.nameless.columns.markers'),
      cell: (subject) => subject.marker_count.toLocaleString(),
    },
    {
      key: 'faces',
      header: t('maintenance.nameless.columns.faces'),
      cell: (subject) => subject.face_count.toLocaleString(),
    },
    {
      key: 'created',
      header: t('maintenance.nameless.columns.created'),
      cell: (subject) => createdOn(subject.created_at),
    },
  ]
  return <RecordTable records={subjects} columns={columns} rowKey={(s) => s.uid} size="sm" />
}

/**
 * Maps a failed detach to the i18n suffix of the message shown: the repair being
 * gone (503) or the library having become clean under the maintainer (409) are
 * both worth saying plainly rather than as a generic failure.
 */
function detachErrorKey(err: unknown): 'clean' | 'unavailable' | 'detachError' {
  if (!(err instanceof ApiError)) {
    return 'detachError'
  }
  if (err.status === 409) {
    return 'clean'
  }
  return err.status === 503 ? 'unavailable' : 'detachError'
}

/**
 * The nameless catch-all subject repair, the one class of data problem that used
 * to be reachable only over SSH.
 *
 * A subject whose name identifies nobody cannot be created deliberately, so one
 * in the catalogue is an importer artefact that every nameless face after it was
 * assigned to — in production it owns 96 % of the library's faces and sits first
 * in /people. The card reports it (read-only, safe to click), detaches it behind
 * a confirmation that hands the undo file to the browser as a download *before*
 * anything is scheduled, and takes that file back for the undo. Both destructive
 * directions run in the background job queue, so the counts they report are what
 * was scheduled.
 */
export function NamelessSubjectsCard() {
  const { t } = useTranslation()
  const [report, setReport] = useState<ReportState>({ status: 'idle' })
  const [detach, setDetach] = useState<DetachState>({ status: 'idle' })
  const [restore, setRestore] = useState<RestoreState>({ status: 'idle' })
  const fileInput = useRef<HTMLInputElement>(null)

  const runReport = useCallback(async () => {
    setReport({ status: 'loading' })
    try {
      setReport({ status: 'ready', report: await fetchNamelessSubjects() })
    } catch {
      setReport({ status: 'error' })
    }
  }, [])

  const runDetach = useCallback(async () => {
    setDetach({ status: 'running' })
    try {
      const undo = await detachNamelessSubjects()
      setDetach({ status: 'done', undo })
      await runReport()
    } catch (err) {
      setDetach({ status: 'error', message: t(`maintenance.nameless.${detachErrorKey(err)}`) })
    }
  }, [runReport, t])

  const runRestore = useCallback(
    async (file: File) => {
      setRestore({ status: 'running' })
      try {
        const result = await restoreNamelessSubjects(file)
        setRestore({ status: 'done', queued: result.queued })
        await runReport()
      } catch (err) {
        const invalid = err instanceof ApiError && err.status === 400
        setRestore({
          status: 'error',
          message: invalid
            ? t('maintenance.nameless.restoreInvalid')
            : t('maintenance.nameless.restoreError'),
        })
      } finally {
        // Clear the picker, so re-uploading the same file after a failed attempt
        // still fires a change event.
        if (fileInput.current) {
          fileInput.current.value = ''
        }
      }
    },
    [runReport, t],
  )

  const found = report.status === 'ready' ? report.report.subjects.length > 0 : false
  const busy = detach.status === 'running' || restore.status === 'running'

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('maintenance.nameless.title')}</h2>
        <p className="text-secondary small">{t('maintenance.nameless.hint')}</p>

        <Button
          variant="outline-primary"
          disabled={report.status === 'loading'}
          onClick={() => {
            void runReport()
          }}
        >
          {report.status === 'loading' && (
            <Spinner animation="border" size="sm" role="status" className="me-2" />
          )}
          {report.status === 'loading'
            ? t('maintenance.nameless.checking')
            : t('maintenance.nameless.check')}
        </Button>

        {report.status === 'idle' && (
          <p className="text-secondary small mt-3 mb-0">{t('maintenance.nameless.empty')}</p>
        )}
        {report.status === 'error' && (
          <Alert variant="danger" className="mt-3 mb-0">
            {t('maintenance.nameless.reportError')}
          </Alert>
        )}
        {report.status === 'ready' && !found && (
          <Alert variant="success" className="mt-3 mb-0">
            {t('maintenance.nameless.clean')}
          </Alert>
        )}
        {report.status === 'ready' && found && (
          <div className="mt-3">
            <p className="text-secondary mb-2">
              {t('maintenance.nameless.summary', {
                subjects: report.report.subjects.length,
                markers: report.report.marker_total,
                faces: report.report.face_total,
              })}
            </p>
            <NamelessTable subjects={report.report.subjects} />
            {detach.status !== 'confirm' && (
              <Button
                variant="danger"
                disabled={busy}
                onClick={() => {
                  setDetach({ status: 'confirm' })
                }}
              >
                {detach.status === 'running' && (
                  <Spinner animation="border" size="sm" role="status" className="me-2" />
                )}
                {detach.status === 'running'
                  ? t('maintenance.nameless.detaching')
                  : t('maintenance.nameless.detach')}
              </Button>
            )}
            {detach.status === 'confirm' && (
              <Alert variant="warning" className="mb-0">
                <p className="mb-2">
                  {t('maintenance.nameless.confirm', {
                    markers: report.report.marker_total,
                    faces: report.report.face_total,
                  })}
                </p>
                <p className="mb-2 small">{t('maintenance.nameless.confirmUndo')}</p>
                <div className="d-flex gap-2">
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => {
                      void runDetach()
                    }}
                  >
                    {t('maintenance.nameless.confirmRun')}
                  </Button>
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    onClick={() => {
                      setDetach({ status: 'idle' })
                    }}
                  >
                    {t('maintenance.nameless.cancel')}
                  </Button>
                </div>
              </Alert>
            )}
          </div>
        )}

        {detach.status === 'error' && (
          <Alert variant="danger" className="mt-3 mb-0">
            {detach.message}
          </Alert>
        )}
        {detach.status === 'done' && (
          <Alert variant="success" className="mt-3 mb-0">
            {t('maintenance.nameless.detachResult', {
              filename: detach.undo.filename,
              markers: detach.undo.markers,
              faces: detach.undo.faces,
            })}
          </Alert>
        )}

        <hr />
        <h3 className="h6 mb-1">{t('maintenance.nameless.restoreTitle')}</h3>
        <p className="text-secondary small">{t('maintenance.nameless.restoreHint')}</p>
        <Form.Group controlId="nameless-undo-file">
          <Form.Label className="visually-hidden">
            {t('maintenance.nameless.restoreLabel')}
          </Form.Label>
          <Form.Control
            ref={fileInput}
            type="file"
            accept="application/json,.json"
            disabled={busy}
            onChange={(e) => {
              const file = (e.target as HTMLInputElement).files?.[0]
              if (file) {
                void runRestore(file)
              }
            }}
          />
        </Form.Group>
        {restore.status === 'running' && (
          <div className="text-secondary small mt-2">
            <Spinner animation="border" size="sm" role="status" className="me-2" />
            {t('maintenance.nameless.restoring')}
          </div>
        )}
        {restore.status === 'error' && (
          <Alert variant="danger" className="mt-3 mb-0">
            {restore.message}
          </Alert>
        )}
        {restore.status === 'done' && (
          <Alert variant="success" className="mt-3 mb-0">
            {t('maintenance.nameless.restoreResult', { queued: restore.queued })}
          </Alert>
        )}
      </Card.Body>
    </Card>
  )
}
