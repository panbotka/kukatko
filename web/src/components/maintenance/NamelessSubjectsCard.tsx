import { useCallback, useEffect, useRef, useState } from 'react'
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
  NamelessRestoreError,
} from '../../services/maintenance'
import { fetchAdminProjects, type AdminProject } from '../../services/projects'
import { RecordTable, type RecordColumn } from '../RecordTable'
import { TechnicalDetail } from '../TechnicalDetail'

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

/** Lifecycle of the list of projects the picker offers. */
type ProjectsState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; projects: AdminProject[] }

/**
 * Loads every project of the instance (`GET /projects`, maintainer-only) for the
 * picker — the instance list, not the caller's memberships, so a maintainer who
 * belongs to no project can still repair one.
 */
function useAllProjects(): ProjectsState {
  const [state, setState] = useState<ProjectsState>({ status: 'loading' })
  useEffect(() => {
    const controller = new AbortController()
    fetchAdminProjects(controller.signal).then(
      (projects) => {
        setState({ status: 'ready', projects })
      },
      () => {
        if (!controller.signal.aborted) {
          setState({ status: 'error' })
        }
      },
    )
    return () => {
      controller.abort()
    }
  }, [])
  return state
}

/** The picker of the project the repair acts on. */
function ProjectPicker({
  projects,
  value,
  disabled,
  onChange,
}: {
  projects: ProjectsState
  value: string
  disabled: boolean
  onChange: (slug: string) => void
}) {
  const { t } = useTranslation()
  if (projects.status === 'loading') {
    return (
      <p className="text-secondary small">
        <Spinner animation="border" size="sm" role="status" className="me-2" />
        {t('maintenance.nameless.projectsLoading')}
      </p>
    )
  }
  if (projects.status === 'error') {
    return <Alert variant="danger">{t('maintenance.nameless.projectsError')}</Alert>
  }
  if (projects.projects.length === 0) {
    return <p className="text-secondary small">{t('maintenance.nameless.projectsEmpty')}</p>
  }
  return (
    <Form.Group controlId="nameless-project" className="mb-3">
      <Form.Label>{t('maintenance.nameless.projectLabel')}</Form.Label>
      <Form.Select
        value={value}
        disabled={disabled}
        onChange={(e) => {
          onChange(e.target.value)
        }}
      >
        <option value="" disabled>
          {t('maintenance.nameless.projectPlaceholder')}
        </option>
        {projects.projects.map((project) => (
          <option key={project.uid} value={project.slug}>
            {project.active
              ? project.name
              : t('maintenance.nameless.projectInactive', { name: project.name })}
          </option>
        ))}
      </Form.Select>
    </Form.Group>
  )
}

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
      // The row says what this is; the two identifiers that tell one catch-all
      // from another are behind the disclosure, because that is the only thing
      // here nobody but a maintainer can read.
      cell: (subject) => (
        <>
          <span>{t('maintenance.nameless.emptyName')}</span>
          <TechnicalDetail id={`nameless-subject-${subject.uid}`}>
            <dl className="small mb-0">
              <dt className="fw-normal text-secondary">{t('maintenance.nameless.uidLabel')}</dt>
              <dd className="font-monospace text-break">{subject.uid}</dd>
              <dt className="fw-normal text-secondary">{t('maintenance.nameless.slugLabel')}</dt>
              <dd className="font-monospace text-break mb-0">{subject.slug}</dd>
            </dl>
          </TechnicalDetail>
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
 * The sentence for a refused restore. A valid undo file picked while another
 * project is chosen says so — it is not lost, it belongs elsewhere — and is
 * told apart from a file that is no undo file at all, or one with nothing in it.
 */
function restoreFailureKey(err: unknown) {
  if (err instanceof NamelessRestoreError && err.status === 400) {
    switch (err.code) {
      case 'undo_wrong_project':
        return 'maintenance.nameless.restoreWrongProject' as const
      case 'undo_empty':
        return 'maintenance.nameless.restoreEmpty' as const
      default:
        return 'maintenance.nameless.restoreInvalid' as const
    }
  }
  if (err instanceof ApiError && err.status === 400) {
    return 'maintenance.nameless.restoreInvalid' as const
  }
  return 'maintenance.nameless.restoreError' as const
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
 *
 * The card sits on the instance-level `/maintenance` page, which belongs to no
 * project, so it asks which one: a picker over every project of the instance
 * (not the maintainer's memberships), and every request names the picked slug.
 */
export function NamelessSubjectsCard() {
  const { t } = useTranslation()
  const projects = useAllProjects()
  // The picked project's slug, or "" while none is picked. With a single
  // project there is nothing to choose, so it is picked for the maintainer;
  // with more the choice is always explicit.
  const [picked, setPicked] = useState('')
  const only =
    projects.status === 'ready' && projects.projects.length === 1 ? projects.projects[0] : null
  const project = picked !== '' ? picked : (only?.slug ?? '')
  const projectName =
    projects.status === 'ready'
      ? (projects.projects.find((p) => p.slug === project)?.name ?? project)
      : project
  const [report, setReport] = useState<ReportState>({ status: 'idle' })
  const [detach, setDetach] = useState<DetachState>({ status: 'idle' })
  const [restore, setRestore] = useState<RestoreState>({ status: 'idle' })
  const fileInput = useRef<HTMLInputElement>(null)

  const runReport = useCallback(async () => {
    setReport({ status: 'loading' })
    try {
      setReport({ status: 'ready', report: await fetchNamelessSubjects(project) })
    } catch {
      setReport({ status: 'error' })
    }
  }, [project])

  const pickProject = useCallback((slug: string) => {
    // What was found, detached or restored belongs to the previous project.
    setPicked(slug)
    setReport({ status: 'idle' })
    setDetach({ status: 'idle' })
    setRestore({ status: 'idle' })
  }, [])

  const runDetach = useCallback(async () => {
    setDetach({ status: 'running' })
    try {
      const undo = await detachNamelessSubjects(project)
      setDetach({ status: 'done', undo })
      await runReport()
    } catch (err) {
      setDetach({ status: 'error', message: t(`maintenance.nameless.${detachErrorKey(err)}`) })
    }
  }, [project, runReport, t])

  const runRestore = useCallback(
    async (file: File) => {
      setRestore({ status: 'running' })
      try {
        const result = await restoreNamelessSubjects(project, file)
        setRestore({ status: 'done', queued: result.queued })
        await runReport()
      } catch (err) {
        setRestore({ status: 'error', message: t(restoreFailureKey(err)) })
      } finally {
        // Clear the picker, so re-uploading the same file after a failed attempt
        // still fires a change event.
        if (fileInput.current) {
          fileInput.current.value = ''
        }
      }
    },
    [project, runReport, t],
  )

  const found = report.status === 'ready' ? report.report.subjects.length > 0 : false
  const busy = detach.status === 'running' || restore.status === 'running'
  const noProject = project === ''

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{t('maintenance.nameless.title')}</h2>
        <p className="text-secondary small">{t('maintenance.nameless.hint')}</p>

        <ProjectPicker
          projects={projects}
          value={project}
          disabled={busy || report.status === 'loading'}
          onChange={pickProject}
        />

        <Button
          variant="outline-primary"
          disabled={noProject || report.status === 'loading'}
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
          <p className="text-secondary small mt-3 mb-0">
            {noProject ? t('maintenance.nameless.pickProject') : t('maintenance.nameless.empty')}
          </p>
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
                <p className="mb-2 fw-bold">
                  {t('maintenance.nameless.confirmProject', { name: projectName })}
                </p>
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
            disabled={busy || noProject}
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
