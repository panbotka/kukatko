import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type LimitDraft, limitDraft, parseLimit } from '../../lib/byteLimit'
import {
  type AdminProject,
  createProject,
  ProjectApiError,
  type SlugProblem,
  slugProblem,
} from '../../services/projects'
import Modal from '../Modal'

import { LimitField } from './LimitField'

/** Props of {@link NewProjectModal}. */
export interface NewProjectModalProps {
  show: boolean
  onHide: () => void
  /** Called with the project the server created; the caller closes the dialog. */
  onCreated: (project: AdminProject) => void
}

/** The per-field refusals the dialog can show. */
interface FieldErrors {
  slug: string | null
  name: string | null
  maxFile: boolean
  maxTotal: boolean
}

const NO_ERRORS: FieldErrors = { slug: null, name: null, maxFile: false, maxTotal: false }

/**
 * The "Nový projekt" dialog of `/system/projects`: slug, name, description and
 * the two limits.
 *
 * The slug is checked **as it is typed**, against the same rule the backend
 * applies (`slugProblem`), because it is the one field that can never be
 * corrected afterwards — it is the project's address. The server still has the
 * last word: its refusals come back as codes and land on the field they are
 * about (`slug_taken` → „Tento slug už existuje"), anything else above the
 * buttons.
 */
export function NewProjectModal({ show, onHide, onCreated }: NewProjectModalProps) {
  const { t } = useTranslation()
  const [slug, setSlug] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [maxFile, setMaxFile] = useState<LimitDraft>(limitDraft(null))
  const [maxTotal, setMaxTotal] = useState<LimitDraft>(limitDraft(null))
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [failure, setFailure] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

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
    }
    setErrors(found)
    setFailure(null)
    if (found.slug !== null || found.name !== null || !file.ok || !total.ok) {
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
      })
      reset()
      onCreated(created)
    } catch (error: unknown) {
      const code = error instanceof ProjectApiError ? error.code : ''
      if (code === 'slug_taken') {
        setErrors((prev) => ({ ...prev, slug: t('systemProjects.errors.slugTaken') }))
      } else if (code === 'invalid_slug') {
        setErrors((prev) => ({ ...prev, slug: t('systemProjects.errors.slugFormat') }))
      } else if (code === 'invalid_name') {
        setErrors((prev) => ({ ...prev, name: t('systemProjects.errors.nameRequired') }))
      } else if (code === 'invalid_limit') {
        setErrors((prev) => ({
          ...prev,
          maxFile: file.bytes !== null,
          maxTotal: total.bytes !== null,
        }))
      } else {
        setFailure(t('systemProjects.errors.createFailed'))
      }
    } finally {
      setBusy(false)
    }
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
