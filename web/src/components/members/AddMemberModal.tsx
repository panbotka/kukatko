import { type SyntheticEvent, useId, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import type { ProjectRole } from '../../services/auth'
import { addMember, MEMBER_ROLES } from '../../services/members'
import Modal from '../Modal'

/** Props for {@link AddMemberModal}. */
export interface AddMemberModalProps {
  show: boolean
  /** Closes the dialog. */
  onClose: () => void
  /** Called after every answered request, so the page can refetch its list. */
  onSubmitted: () => void
}

/** Where the dialog is: filling the form, or showing the one neutral answer. */
type Phase = 'form' | 'done'

/**
 * „Přidat člena" — adds an existing account to the current project by its
 * **exact** username or e-mail address.
 *
 * Deliberately blind: there is no autocomplete, and whatever the backend did —
 * added somebody, found nobody, found a member already — the dialog says the
 * same neutral sentence („Pokud takový účet existuje, byl přidán."). A project
 * admin must not be able to probe which accounts exist on the instance; the
 * audit trail records what really happened. Only a request that never got an
 * answer (no network) is reported as a failure, since nothing was asked.
 */
export function AddMemberModal({ show, onClose, onSubmitted }: AddMemberModalProps) {
  const { t } = useTranslation()
  const titleId = useId()
  const [identifier, setIdentifier] = useState('')
  const [role, setRole] = useState<ProjectRole>('viewer')
  const [missing, setMissing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const [phase, setPhase] = useState<Phase>('form')

  /** Puts the dialog back to an empty form for the next time it opens. */
  function reset() {
    setIdentifier('')
    setRole('viewer')
    setMissing(false)
    setFailed(false)
    setPhase('form')
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (busy) {
      return
    }
    const value = identifier.trim()
    if (value === '') {
      setMissing(true)
      return
    }
    setBusy(true)
    setFailed(false)
    try {
      await addMember(value, role)
      setPhase('done')
      onSubmitted()
    } catch {
      setFailed(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      show={show}
      onHide={busy ? undefined : onClose}
      onExited={reset}
      centered
      aria-labelledby={titleId}
    >
      <Modal.Header closeButton={!busy}>
        <Modal.Title id={titleId}>{t('members.add.title')}</Modal.Title>
      </Modal.Header>
      {phase === 'done' ? (
        <>
          <Modal.Body>
            <Alert variant="info" className="mb-0" role="status">
              {t('members.add.done')}
            </Alert>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="primary" onClick={onClose}>
              {t('members.add.close')}
            </Button>
          </Modal.Footer>
        </>
      ) : (
        <Form noValidate onSubmit={(event) => void handleSubmit(event)}>
          <Modal.Body>
            {failed && (
              <Alert variant="danger" role="alert">
                {t('members.add.failed')}
              </Alert>
            )}
            <Form.Group className="mb-3" controlId="member-identifier">
              <Form.Label>{t('members.add.identifierLabel')}</Form.Label>
              <Form.Control
                type="text"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                value={identifier}
                isInvalid={missing}
                disabled={busy}
                onChange={(event) => {
                  setIdentifier(event.currentTarget.value)
                  setMissing(false)
                }}
              />
              {missing ? (
                <Form.Control.Feedback type="invalid">
                  {t('members.add.identifierRequired')}
                </Form.Control.Feedback>
              ) : (
                <Form.Text>{t('members.add.identifierHelp')}</Form.Text>
              )}
            </Form.Group>
            <Form.Group controlId="member-role">
              <Form.Label>{t('members.add.roleLabel')}</Form.Label>
              <Form.Select
                value={role}
                disabled={busy}
                onChange={(event) => {
                  setRole(event.currentTarget.value as ProjectRole)
                }}
              >
                {MEMBER_ROLES.map((value) => (
                  <option key={value} value={value}>
                    {t(`roles.${value}`)}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={onClose} disabled={busy}>
              {t('members.add.cancel')}
            </Button>
            <Button type="submit" variant="primary" disabled={busy}>
              {busy && <Spinner as="span" animation="border" size="sm" className="me-2" />}
              {t('members.add.submit')}
            </Button>
          </Modal.Footer>
        </Form>
      )}
    </Modal>
  )
}
