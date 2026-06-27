import { type SyntheticEvent, useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { createLabel, type Label, type LabelInput, updateLabel } from '../../services/organize'

/** Props for {@link LabelEditModal}. */
export interface LabelEditModalProps {
  /** The label being edited; omit (or pass `null`) to create a new label. */
  label?: Label | null
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the created/updated label after a successful save. */
  onSaved: (label: Label) => void
}

/**
 * A modal form for creating or renaming a label and setting its priority
 * (higher floats it up in lists). A validation or save error is surfaced inline.
 */
export function LabelEditModal({ label, show, onHide, onSaved }: LabelEditModalProps) {
  const { t } = useTranslation()
  const editing = label != null
  const [name, setName] = useState('')
  const [priority, setPriority] = useState('0')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  // Reset the form to the label's values (or blanks) whenever it opens.
  useEffect(() => {
    if (show) {
      setName(label?.name ?? '')
      setPriority(String(label?.priority ?? 0))
      setError(false)
    }
  }, [show, label])

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed === '') {
      setError(true)
      return
    }
    const parsed = Number.parseInt(priority, 10)
    const input: LabelInput = {
      name: trimmed,
      priority: Number.isNaN(parsed) ? 0 : parsed,
    }
    setBusy(true)
    setError(false)
    try {
      const saved = editing ? await updateLabel(label.uid, input) : await createLabel(input)
      onSaved(saved)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered>
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {editing ? t('labels.edit.titleEdit') : t('labels.edit.titleNew')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && <p className="text-danger small">{t('labels.edit.error')}</p>}
          <Form.Group className="mb-3" controlId="label-name">
            <Form.Label>{t('labels.edit.name')}</Form.Label>
            <Form.Control
              type="text"
              value={name}
              autoFocus
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Group controlId="label-priority">
            <Form.Label>{t('labels.edit.priority')}</Form.Label>
            <Form.Control
              type="number"
              value={priority}
              disabled={busy}
              onChange={(event) => {
                setPriority(event.target.value)
              }}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={busy}>
            {t('labels.edit.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {t('labels.edit.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}
