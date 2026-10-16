import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { type LimitDraft, limitDraft, parseLimit } from '../../lib/byteLimit'
import { projectPathFor } from '../../project/paths'
import {
  type AdminProject,
  type CreatedProject,
  createProject,
  type FirstAdmin,
  type FirstAdminBody,
  ProjectApiError,
  type SlugProblem,
  slugProblem,
} from '../../services/projects'
import Modal from '../Modal'
import { systemProjectPath } from '../navItems'

import { FirstAdminResult } from './FirstAdminResult'
import { LimitField } from './LimitField'

/** Props of {@link NewProjectModal}. */
export interface NewProjectModalProps {
  show: boolean
  onHide: () => void
  /**
   * Called with a project the server created with the caller as its admin; the
   * caller closes the dialog.
   */
  onCreated: (project: AdminProject) => void
  /**
   * Called with a project set up for somebody else, while the dialog stays open
   * on its result screen (the set-password link may be on it); the caller
   * refreshes whatever lists projects.
   */
  onSetUp?: (project: CreatedProject) => void
}

/** The per-field refusals the dialog can show. */
interface FieldErrors {
  slug: string | null
  name: string | null
  maxFile: boolean
  maxTotal: boolean
  adminEmail: string | null
  adminUsername: string | null
}

const NO_ERRORS: FieldErrors = {
  slug: null,
  name: null,
  maxFile: false,
  maxTotal: false,
  adminEmail: null,
  adminUsername: null,
}

/** A loose shape check of an address before the round trip; the server decides. */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/** The first admin's fields as typed. */
interface AdminDraft {
  email: string
  displayName: string
  username: string
}

const EMPTY_ADMIN: AdminDraft = { email: '', displayName: '', username: '' }

/** The body's `admin` object of a draft: only what was filled in. */
function adminBody(draft: AdminDraft): FirstAdminBody {
  const displayName = draft.displayName.trim()
  const username = draft.username.trim()
  return {
    email: draft.email.trim(),
    ...(displayName === '' ? {} : { display_name: displayName }),
    ...(username === '' ? {} : { username }),
  }
}

/**
 * The "Nový projekt" dialog of `/system/projects`: slug, name, description and
 * the two limits — and, behind the switch „Zakládám projekt pro někoho
 * jiného", the section „První správce projektu" (e-mail, required there;
 * display name and username, optional). With it the project is set up for that
 * person in one request: they become its only admin, a new account gets a
 * set-password e-mail, and the administrator does not become a member. The
 * dialog then stays open on a result screen ({@link FirstAdminResult}): the
 * project, the admin's username, „E-mail odeslán" or the copyable link, and
 * the link's expiry.
 *
 * The slug is checked **as it is typed**, against the same rule the backend
 * applies (`slugProblem`), because it is the one field that can never be
 * corrected afterwards — it is the project's address. The server still has the
 * last word: its refusals come back as codes and land on the field they are
 * about (`slug_taken` → „Tento slug už existuje"), anything else above the
 * buttons.
 */
export function NewProjectModal({ show, onHide, onCreated, onSetUp }: NewProjectModalProps) {
  const { t } = useTranslation()
  const [slug, setSlug] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [maxFile, setMaxFile] = useState<LimitDraft>(limitDraft(null))
  const [maxTotal, setMaxTotal] = useState<LimitDraft>(limitDraft(null))
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [failure, setFailure] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [forOther, setForOther] = useState(false)
  const [admin, setAdmin] = useState<AdminDraft>(EMPTY_ADMIN)
  const [result, setResult] = useState<{ project: CreatedProject; admin: FirstAdmin } | null>(null)

  /** The translated refusal of a slug problem. */
  function slugMessage(problem: SlugProblem, value: string): string {
    switch (problem) {
      case 'empty':
        return t('systemProjects.errors.slugEmpty')
      case 'format':
        return t('systemProjects.errors.slugFormat')
      case 'reserved':
        return t('systemProjects.errors.slugReserved', { slug: value })
    }
  }

  function reset() {
    setSlug('')
    setName('')
    setDescription('')
    setMaxFile(limitDraft(null))
    setMaxTotal(limitDraft(null))
    setErrors(NO_ERRORS)
    setFailure(null)
    setForOther(false)
    setAdmin(EMPTY_ADMIN)
    setResult(null)
  }

  /** Closes the dialog, forgetting a shown result (and its link) for good. */
  function close() {
    if (result !== null) {
      reset()
    }
    onHide()
  }

  /** The address problem of the first admin's e-mail, or null. */
  function adminEmailProblem(): string | null {
    if (!forOther) {
      return null
    }
    return EMAIL_PATTERN.test(admin.email.trim()) ? null : t('systemProjects.errors.emailInvalid')
  }

  function changeSlug(value: string) {
    setSlug(value)
    // Live: an empty field is not scolded until the submit, everything else is.
    const problem = slugProblem(value)
    setErrors((prev) => ({
      ...prev,
      slug: problem === null || problem === 'empty' ? null : slugMessage(problem, value),
    }))
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (busy) {
      return
    }
    const problem = slugProblem(slug)
    const file = parseLimit(maxFile)
    const total = parseLimit(maxTotal)
    const found: FieldErrors = {
      slug: problem === null ? null : slugMessage(problem, slug),
      name: name.trim() === '' ? t('systemProjects.errors.nameRequired') : null,
      maxFile: !file.ok,
      maxTotal: !total.ok,
      adminEmail: adminEmailProblem(),
      adminUsername: null,
    }
    setErrors(found)
    setFailure(null)
    if (
      found.slug !== null ||
      found.name !== null ||
      !file.ok ||
      !total.ok ||
      found.adminEmail !== null
    ) {
      return
    }
    setBusy(true)
    try {
      const created = await createProject({
        slug,
        name: name.trim(),
        description: description.trim(),
        ...(file.bytes === null ? {} : { max_file_bytes: file.bytes }),
        ...(total.bytes === null ? {} : { max_total_bytes: total.bytes }),
        ...(forOther ? { admin: adminBody(admin) } : {}),
      })
      if (created.first_admin !== undefined) {
        setResult({ project: created, admin: created.first_admin })
        onSetUp?.(created)
        return
      }
      reset()
      onCreated(created)
    } catch (error: unknown) {
      const code = error instanceof ProjectApiError ? error.code : ''
      if (!applyAdminRefusal(code)) {
        applyRefusal(code, file.bytes, total.bytes)
      }
    } finally {
      setBusy(false)
    }
  }

  /**
   * Puts a refusal about the first admin on its field; false when `code` is
   * not one of those.
   */
  function applyAdminRefusal(code: string): boolean {
    const messages: Record<string, Partial<FieldErrors>> = {
      invalid_email: { adminEmail: t('systemProjects.errors.emailInvalid') },
      email_ambiguous: { adminEmail: t('systemProjects.errors.emailAmbiguous') },
      user_disabled: { adminEmail: t('systemProjects.errors.userDisabled') },
      username_taken: { adminUsername: t('systemProjects.errors.usernameTaken') },
      invalid_username: { adminUsername: t('systemProjects.errors.usernameInvalid') },
    }
    if (!Object.hasOwn(messages, code)) {
      return false
    }
    setErrors((prev) => ({ ...prev, ...messages[code] }))
    return true
  }

  /** Puts a refusal about the project on its field, or above the buttons. */
  function applyRefusal(code: string, fileBytes: number | null, totalBytes: number | null) {
    switch (code) {
      case 'slug_taken':
        setErrors((prev) => ({ ...prev, slug: t('systemProjects.errors.slugTaken') }))
        break
      case 'invalid_slug':
        setErrors((prev) => ({ ...prev, slug: t('systemProjects.errors.slugFormat') }))
        break
      case 'invalid_name':
        setErrors((prev) => ({ ...prev, name: t('systemProjects.errors.nameRequired') }))
        break
      case 'invalid_limit':
        setErrors((prev) => ({
          ...prev,
          maxFile: fileBytes !== null,
          maxTotal: totalBytes !== null,
        }))
        break
      default:
        setFailure(t('systemProjects.errors.createFailed'))
    }
  }

  if (result !== null) {
    return (
      <Modal show={show} onHide={close} centered scrollable fullscreen="sm-down">
        <Modal.Header closeButton>
          <Modal.Title>
            {t('systemProjects.firstAdmin.resultTitle', { name: result.project.name })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <dl className="row mb-0">
            <dt className="col-4 fw-normal text-secondary">
              {t('systemProjects.firstAdmin.projectLabel')}
            </dt>
            <dd className="col-8">
              <Link to={projectPathFor(result.project.slug, '/')} className="font-monospace">
                /p/{result.project.slug}
              </Link>
            </dd>
          </dl>
          <FirstAdminResult admin={result.admin} idPrefix="new-project-admin" />
        </Modal.Body>
        <Modal.Footer>
          <Link
            to={systemProjectPath(result.project.slug)}
            className="btn btn-outline-primary"
            onClick={close}
          >
            {t('systemProjects.firstAdmin.openProject')}
          </Link>
          <Button variant="primary" onClick={close}>
            {t('systemProjects.firstAdmin.close')}
          </Button>
        </Modal.Footer>
      </Modal>
    )
  }

  return (
    <Modal
      show={show}
      onHide={() => {
        if (!busy) {
          onHide()
        }
      }}
      centered
      scrollable
      fullscreen="sm-down"
    >
      <Form noValidate onSubmit={(event) => void handleSubmit(event)}>
        <Modal.Header closeButton={!busy}>
          <Modal.Title>{t('systemProjects.new')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form.Group className="mb-3" controlId="new-project-slug">
            <Form.Label>{t('systemProjects.form.slugLabel')}</Form.Label>
            <Form.Control
              className="font-monospace"
              value={slug}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              maxLength={40}
              isInvalid={errors.slug !== null}
              aria-describedby="new-project-slug-hint"
              onChange={(event) => {
                changeSlug(event.currentTarget.value)
              }}
            />
            <Form.Control.Feedback type="invalid">{errors.slug}</Form.Control.Feedback>
            <Form.Text id="new-project-slug-hint">{t('systemProjects.form.slugHint')}</Form.Text>
          </Form.Group>
          <Form.Group className="mb-3" controlId="new-project-name">
            <Form.Label>{t('systemProjects.form.nameLabel')}</Form.Label>
            <Form.Control
              value={name}
              isInvalid={errors.name !== null}
              onChange={(event) => {
                setName(event.currentTarget.value)
                setErrors((prev) => ({ ...prev, name: null }))
              }}
            />
            <Form.Control.Feedback type="invalid">{errors.name}</Form.Control.Feedback>
          </Form.Group>
          <Form.Group className="mb-3" controlId="new-project-description">
            <Form.Label>{t('systemProjects.form.descriptionLabel')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={description}
              onChange={(event) => {
                setDescription(event.currentTarget.value)
              }}
            />
          </Form.Group>
          <LimitField
            id="new-project-max-file"
            label={t('systemProjects.form.maxFileLabel')}
            value={maxFile}
            invalid={errors.maxFile}
            onChange={(value) => {
              setMaxFile(value)
              setErrors((prev) => ({ ...prev, maxFile: false }))
            }}
          />
          <LimitField
            id="new-project-max-total"
            label={t('systemProjects.form.maxTotalLabel')}
            value={maxTotal}
            invalid={errors.maxTotal}
            onChange={(value) => {
              setMaxTotal(value)
              setErrors((prev) => ({ ...prev, maxTotal: false }))
            }}
          />
          <Form.Check
            type="switch"
            id="new-project-for-other"
            className="mb-1"
            label={t('systemProjects.firstAdmin.toggle')}
            checked={forOther}
            aria-describedby="new-project-for-other-help"
            onChange={(event) => {
              setForOther(event.currentTarget.checked)
              setErrors((prev) => ({ ...prev, adminEmail: null, adminUsername: null }))
            }}
          />
          <Form.Text id="new-project-for-other-help" className="d-block mb-3">
            {t('systemProjects.firstAdmin.toggleHelp')}
          </Form.Text>
          {forOther && (
            <fieldset className="mb-3" data-testid="first-admin-section">
              <legend className="h6">{t('systemProjects.firstAdmin.title')}</legend>
              <Form.Group className="mb-3" controlId="new-project-admin-email">
                <Form.Label>{t('systemProjects.firstAdmin.emailLabel')}</Form.Label>
                <Form.Control
                  type="email"
                  autoComplete="off"
                  value={admin.email}
                  isInvalid={errors.adminEmail !== null}
                  aria-describedby="new-project-admin-email-hint"
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setAdmin((prev) => ({ ...prev, email: value }))
                    setErrors((prev) => ({ ...prev, adminEmail: null }))
                  }}
                />
                <Form.Control.Feedback type="invalid">{errors.adminEmail}</Form.Control.Feedback>
                <Form.Text id="new-project-admin-email-hint">
                  {t('systemProjects.firstAdmin.emailHint')}
                </Form.Text>
              </Form.Group>
              <Form.Group className="mb-3" controlId="new-project-admin-name">
                <Form.Label>{t('systemProjects.firstAdmin.displayNameLabel')}</Form.Label>
                <Form.Control
                  value={admin.displayName}
                  autoComplete="off"
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setAdmin((prev) => ({ ...prev, displayName: value }))
                  }}
                />
              </Form.Group>
              <Form.Group controlId="new-project-admin-username">
                <Form.Label>{t('systemProjects.firstAdmin.usernameLabel')}</Form.Label>
                <Form.Control
                  value={admin.username}
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  maxLength={64}
                  isInvalid={errors.adminUsername !== null}
                  aria-describedby="new-project-admin-username-hint"
                  onChange={(event) => {
                    const value = event.currentTarget.value
                    setAdmin((prev) => ({ ...prev, username: value }))
                    setErrors((prev) => ({ ...prev, adminUsername: null }))
                  }}
                />
                <Form.Control.Feedback type="invalid">{errors.adminUsername}</Form.Control.Feedback>
                <Form.Text id="new-project-admin-username-hint">
                  {t('systemProjects.firstAdmin.usernameHint')}
                </Form.Text>
              </Form.Group>
            </fieldset>
          )}
          {failure !== null && (
            <Alert variant="danger" className="mb-0">
              {failure}
            </Alert>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onHide} disabled={busy}>
            {t('systemProjects.form.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy && <Spinner as="span" animation="border" size="sm" className="me-2" />}
            {t('systemProjects.form.create')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}
