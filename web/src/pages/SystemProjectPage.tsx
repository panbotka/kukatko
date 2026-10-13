import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { ConfirmModal } from '../components/ConfirmModal'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { NarrowPage } from '../components/NarrowPage'
import { SYSTEM_PROJECTS_PATH } from '../components/navItems'
import { LimitField } from '../components/system/LimitField'
import { StorageMoveCard } from '../components/system/StorageMoveCard'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { type LimitDraft, limitDraft, parseLimit } from '../lib/byteLimit'
import { formatBytes, formatCount } from '../lib/format'
import { projectPathFor } from '../project/paths'
import {
  type AdminProject,
  addProjectMember,
  fetchAdminProjects,
  updateProject,
  type UpdateProjectBody,
} from '../services/projects'

/** Fetch lifecycle of the project behind the page. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; project: AdminProject }

/** The editable fields of the project. */
interface Draft {
  name: string
  description: string
  maxFile: LimitDraft
  maxTotal: LimitDraft
}

/** A transient outcome the page reports above the form. */
type Notice = { variant: 'success' | 'danger'; text: string } | null

/** The draft a stored project is edited from. */
function toDraft(project: AdminProject): Draft {
  return {
    name: project.name,
    description: project.description,
    maxFile: limitDraft(project.max_file_bytes),
    maxTotal: limitDraft(project.max_total_bytes),
  }
}

/** True when two limit drafts say the same thing as typed. */
function sameLimit(a: LimitDraft, b: LimitDraft): boolean {
  return a.amount.trim() === b.amount.trim() && (a.amount.trim() === '' || a.unit === b.unit)
}

/**
 * `/system/projects/:slug` — one project as the system administrator manages
 * it: its name, description and limits; whether it is active (switched behind
 * a confirmation, since deactivating locks every member out); adding oneself as
 * its admin, for a project one does not belong to; the door to its members page
 * (`/p/<slug>/members`); its usage; and where its files live in the object store.
 *
 * The page is addressed by slug, as the project is everywhere else, while the
 * API addresses it by uid — so it reads the project list and picks the row. The
 * „Úložiště" section is {@link StorageMoveCard}: the prefix and, for a project
 * still in the root of the store, its storage move with live progress.
 *
 * Saving sends only what changed: a limit left as it was is not re-sent, so a
 * value set to the byte elsewhere (`kukatko ctl`) is never rounded by this form.
 */
export function SystemProjectPage() {
  const { t, i18n } = useTranslation()
  const { slug = '' } = useParams()
  const { user, projects: memberships, refresh } = useAuth()

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [draft, setDraft] = useState<Draft | null>(null)
  const [invalid, setInvalid] = useState({ name: false, maxFile: false, maxTotal: false })
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState<Notice>(null)
  // The state the switch asks for; kept while the dialog fades out so its words do not flip.
  const [confirm, setConfirm] = useState({ show: false, active: false })
  const [statusBusy, setStatusBusy] = useState(false)
  const [adding, setAdding] = useState(false)
  const [joined, setJoined] = useState(false)

  const project = load.status === 'ready' ? load.project : null
  useDocumentTitle(project?.name ?? t('systemProjects.title'))

  const reload = useCallback(
    async (signal?: AbortSignal) => {
      setLoad({ status: 'loading' })
      try {
        const all = await fetchAdminProjects(signal)
        if (signal?.aborted === true) {
          return
        }
        const found = all.find((p) => p.slug === slug)
        if (found === undefined) {
          setLoad({ status: 'missing' })
          return
        }
        setLoad({ status: 'ready', project: found })
        setDraft(toDraft(found))
      } catch (error: unknown) {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setLoad({ status: 'error' })
        }
      }
    },
    [slug],
  )

  useEffect(() => {
    const controller = new AbortController()
    void reload(controller.signal)
    return () => {
      controller.abort()
    }
  }, [reload])

  /** Replaces the shown project with what the server now holds. */
  function adopt(next: AdminProject) {
    setLoad({ status: 'ready', project: next })
    setDraft(toDraft(next))
  }

  if (load.status === 'loading' || (load.status === 'ready' && draft === null)) {
    return (
      <NarrowPage>
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('systemProjects.loading')}</span>
          </Spinner>
        </div>
      </NarrowPage>
    )
  }

  if (load.status === 'error' || load.status === 'missing') {
    return (
      <NarrowPage>
        <BackLink to={SYSTEM_PROJECTS_PATH} label={t('systemProject.back')} />
        <ErrorState
          title={
            load.status === 'missing'
              ? t('systemProject.notFound', { slug })
              : t('systemProject.loadFailed')
          }
          onRetry={() => {
            void reload()
          }}
        />
      </NarrowPage>
    )
  }

  // Narrowed by the two returns above; restated for the type checker.
  if (project === null || draft === null) {
    return null
  }
  const current = project
  const form = draft
  const saved = toDraft(current)
  const dirty =
    form.name !== saved.name ||
    form.description !== saved.description ||
    !sameLimit(form.maxFile, saved.maxFile) ||
    !sameLimit(form.maxTotal, saved.maxTotal)
  const isMember = joined || memberships.some((m) => m.uid === current.uid)
  const locale = i18n.language

  async function handleSave(event: SyntheticEvent) {
    event.preventDefault()
    if (saving) {
      return
    }
    const file = parseLimit(form.maxFile)
    const total = parseLimit(form.maxTotal)
    const nameMissing = form.name.trim() === ''
    setInvalid({ name: nameMissing, maxFile: !file.ok, maxTotal: !total.ok })
    if (nameMissing || !file.ok || !total.ok) {
      return
    }
    const body: UpdateProjectBody = {}
    if (form.name !== saved.name) {
      body.name = form.name.trim()
    }
    if (form.description !== saved.description) {
      body.description = form.description.trim()
    }
    if (!sameLimit(form.maxFile, saved.maxFile)) {
      body.max_file_bytes = file.bytes
    }
    if (!sameLimit(form.maxTotal, saved.maxTotal)) {
      body.max_total_bytes = total.bytes
    }
    setSaving(true)
    setNotice(null)
    try {
      adopt(await updateProject(current.uid, body))
      setNotice({ variant: 'success', text: t('systemProject.saved') })
    } catch {
      setNotice({ variant: 'danger', text: t('systemProject.saveFailed') })
    } finally {
      setSaving(false)
    }
  }

  async function handleActive(active: boolean) {
    setStatusBusy(true)
    setNotice(null)
    try {
      const next = await updateProject(current.uid, { active })
      // Only the flag: whatever is being typed into the form stays.
      setLoad({ status: 'ready', project: next })
    } catch {
      setNotice({ variant: 'danger', text: t('systemProject.statusFailed') })
    } finally {
      setStatusBusy(false)
      setConfirm((prev) => ({ ...prev, show: false }))
    }
  }

  async function handleAddMe() {
    if (user === null || adding) {
      return
    }
    setAdding(true)
    setNotice(null)
    try {
      await addProjectMember(current.uid, user.uid, 'admin')
      setJoined(true)
      setNotice({ variant: 'success', text: t('systemProject.alreadyMember') })
      // The switcher and the chooser read the memberships from the session.
      await refresh()
    } catch {
      setNotice({ variant: 'danger', text: t('systemProject.addMeFailed') })
    } finally {
      setAdding(false)
    }
  }

  return (
    <NarrowPage>
      <BackLink to={SYSTEM_PROJECTS_PATH} label={t('systemProject.back')} />
      <h1 className="kk-page-title mb-1">{current.name}</h1>
      <p className="text-secondary font-monospace">/p/{current.slug}</p>

      {notice !== null && (
        <Alert
          variant={notice.variant}
          role="alert"
          dismissible
          onClose={() => {
            setNotice(null)
          }}
        >
          {notice.text}
        </Alert>
      )}

      <Card className="mb-4">
        <Card.Body>
          <h2 className="kk-section-title mb-3">{t('systemProject.detailsTitle')}</h2>
          <Form noValidate onSubmit={(event) => void handleSave(event)}>
            <Form.Group className="mb-3" controlId="project-name">
              <Form.Label>{t('systemProjects.form.nameLabel')}</Form.Label>
              <Form.Control
                value={form.name}
                isInvalid={invalid.name}
                onChange={(event) => {
                  const value = event.currentTarget.value
                  setDraft({ ...form, name: value })
                  setInvalid((prev) => ({ ...prev, name: false }))
                }}
              />
              {invalid.name && (
                <Form.Control.Feedback type="invalid">
                  {t('systemProjects.errors.nameRequired')}
                </Form.Control.Feedback>
              )}
            </Form.Group>
            <Form.Group className="mb-3" controlId="project-description">
              <Form.Label>{t('systemProjects.form.descriptionLabel')}</Form.Label>
              <Form.Control
                as="textarea"
                rows={3}
                value={form.description}
                onChange={(event) => {
                  const value = event.currentTarget.value
                  setDraft({ ...form, description: value })
                }}
              />
            </Form.Group>
            <LimitField
              id="project-max-file"
              label={t('systemProjects.form.maxFileLabel')}
              value={form.maxFile}
              invalid={invalid.maxFile}
              onChange={(value) => {
                setDraft({ ...form, maxFile: value })
                setInvalid((prev) => ({ ...prev, maxFile: false }))
              }}
            />
            <LimitField
              id="project-max-total"
              label={t('systemProjects.form.maxTotalLabel')}
              value={form.maxTotal}
              invalid={invalid.maxTotal}
              onChange={(value) => {
                setDraft({ ...form, maxTotal: value })
                setInvalid((prev) => ({ ...prev, maxTotal: false }))
              }}
            />
            <Button type="submit" variant="primary" disabled={!dirty || saving}>
              {saving && <Spinner as="span" animation="border" size="sm" className="me-2" />}
              {t('systemProject.save')}
            </Button>
          </Form>
        </Card.Body>
      </Card>

      <Card className="mb-4">
        <Card.Body>
          <h2 className="kk-section-title mb-3">{t('systemProject.statusTitle')}</h2>
          <Form.Check
            type="switch"
            id="project-active"
            label={t('systemProject.activeLabel')}
            checked={current.active}
            disabled={statusBusy}
            onChange={(event) => {
              setConfirm({ show: true, active: event.currentTarget.checked })
            }}
          />
          <Form.Text>{t('systemProject.activeHelp')}</Form.Text>
        </Card.Body>
      </Card>

      <Card className="mb-4">
        <Card.Body>
          <h2 className="kk-section-title mb-3">{t('systemProject.membersTitle')}</h2>
          <p className="mb-3">
            {t('systemProject.membersCount', { count: current.members_count })}
          </p>
          <div className="d-flex flex-wrap align-items-center gap-2">
            <Link to={projectPathFor(current.slug, '/members')} className="btn btn-outline-primary">
              <Icon name="people" className="me-1" />
              {t('systemProject.membersLink')}
            </Link>
            {!isMember && (
              <Button
                variant="outline-secondary"
                disabled={adding}
                title={t('systemProject.addMeHelp')}
                onClick={() => {
                  void handleAddMe()
                }}
              >
                <Icon name="person-plus" className="me-1" />
                {t('systemProject.addMe')}
              </Button>
            )}
          </div>
        </Card.Body>
      </Card>

      <Card className="mb-4">
        <Card.Body>
          <h2 className="kk-section-title mb-3">{t('systemProject.usageTitle')}</h2>
          <dl className="row mb-0">
            <dt className="col-6 fw-normal text-secondary">{t('systemProject.photos')}</dt>
            <dd className="col-6">{formatCount(current.photos_count, locale)}</dd>
            <dt className="col-6 fw-normal text-secondary">{t('systemProject.originals')}</dt>
            <dd className="col-6 mb-0">{formatBytes(current.originals_bytes, locale)}</dd>
          </dl>
        </Card.Body>
      </Card>

      <StorageMoveCard key={current.uid} project={current} />

      <ConfirmModal
        show={confirm.show}
        title={
          confirm.active
            ? t('systemProject.reactivateTitle', { name: current.name })
            : t('systemProject.deactivateTitle', { name: current.name })
        }
        confirmLabel={
          confirm.active ? t('systemProject.reactivate') : t('systemProject.deactivate')
        }
        variant={confirm.active ? 'primary' : 'danger'}
        busy={statusBusy}
        onConfirm={() => {
          void handleActive(confirm.active)
        }}
        onCancel={() => {
          setConfirm((prev) => ({ ...prev, show: false }))
        }}
      >
        {confirm.active ? t('systemProject.reactivateBody') : t('systemProject.deactivateBody')}
      </ConfirmModal>
    </NarrowPage>
  )
}
