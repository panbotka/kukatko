import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { formatDateTimeMinutes } from '../../lib/format'
import {
  issuePasswordReset,
  PASSWORD_RESET_TTL_DAYS,
  type AdminUser,
  type IssuedPasswordReset,
} from '../../services/users'
import { Icon } from '../Icon'

import Modal from '../Modal'
import { isPlaceholderEmail } from './account'
import { actionErrorFor, type ErrorKey } from './errors'

/** What the dialog is doing: asking, working, or showing the issued link. */
type Stage =
  | { kind: 'confirm' }
  | { kind: 'issuing' }
  | { kind: 'issued'; reset: IssuedPasswordReset }

/** Props for {@link ResetLinkModal}. */
export interface ResetLinkModalProps {
  /** The account the link is for. */
  user: AdminUser
  /** Closes the dialog — which is also what discards the link. */
  onHide: () => void
}

/**
 * The sibling of "set password": instead of choosing a password for somebody and
 * having to tell them what it is, issue a one-time link and let them choose
 * their own.
 *
 * It asks before it acts, because issuing kills the account's earlier unused
 * link — an administrator who clicks twice while the first mail is still in
 * flight would otherwise silently break the link the person is about to follow.
 *
 * Afterwards it shows the link itself in a read-only field, selectable and
 * copyable in one press, exactly as the account page discloses a fresh API
 * token: the backend mails the link, but an instance with mail switched off, or
 * an account whose address is a `.invalid` placeholder, sends nothing — and then
 * the administrator's own copy is the only one there is. The dialog says which
 * of the two happened rather than promising a mail nobody will get.
 *
 * The link lives in this component's state only, so closing the dialog takes it
 * off the screen for good; it is a bearer credential for one account's password
 * and has no business staying on a roster somebody may walk away from.
 */
export function ResetLinkModal({ user, onHide }: ResetLinkModalProps) {
  const { t, i18n } = useTranslation()
  const [stage, setStage] = useState<Stage>({ kind: 'confirm' })
  const [error, setError] = useState<ErrorKey | null>(null)
  const [copied, setCopied] = useState(false)

  async function issue() {
    setError(null)
    setStage({ kind: 'issuing' })
    try {
      setStage({ kind: 'issued', reset: await issuePasswordReset(user.uid) })
    } catch (err) {
      // Back to the question: nothing was issued, so the same button is still
      // the right next step once the reader has read why it failed.
      setError(actionErrorFor(err))
      setStage({ kind: 'confirm' })
    }
  }

  async function copy(url: string) {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      // The clipboard can be denied (an insecure context, a withheld
      // permission). The link is in the field either way, so there is nothing
      // to report and nothing to undo.
    }
  }

  const busy = stage.kind === 'issuing'

  return (
    <Modal show onHide={onHide} centered scrollable>
      <Modal.Header closeButton={!busy}>
        <Modal.Title as="h2" className="kk-section-title mb-0">
          {t('users.resetLink.title', { username: user.username })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {error !== null && (
          <Alert variant="danger" role="alert">
            {t(error)}
          </Alert>
        )}

        {stage.kind === 'issued' ? (
          <>
            <p>
              {isPlaceholderEmail(stage.reset.email)
                ? t('users.resetLink.notMailed', { days: PASSWORD_RESET_TTL_DAYS })
                : t('users.resetLink.mailed', {
                    email: stage.reset.email,
                    days: PASSWORD_RESET_TTL_DAYS,
                  })}{' '}
              {t('users.resetLink.expires', {
                date: formatDateTimeMinutes(stage.reset.expires_at, i18n.language),
              })}
            </p>
            <InputGroup>
              <Form.Control
                readOnly
                value={stage.reset.reset_url}
                aria-label={t('users.resetLink.linkLabel')}
                onFocus={(event) => {
                  event.target.select()
                }}
              />
              <Button
                variant="outline-secondary"
                className="d-inline-flex align-items-center gap-2"
                onClick={() => {
                  void copy(stage.reset.reset_url)
                }}
              >
                <Icon name={copied ? 'check-lg' : 'clipboard'} />
                {copied ? t('users.resetLink.copied') : t('users.resetLink.copy')}
              </Button>
            </InputGroup>
          </>
        ) : (
          <p className="mb-0">{t('users.resetLink.body', { username: user.username })}</p>
        )}
      </Modal.Body>
      <Modal.Footer>
        {stage.kind === 'issued' ? (
          <Button variant="secondary" onClick={onHide}>
            {t('users.resetLink.close')}
          </Button>
        ) : (
          <>
            <Button variant="secondary" onClick={onHide} disabled={busy}>
              {t('users.form.cancel')}
            </Button>
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                void issue()
              }}
            >
              {busy && (
                <Spinner
                  animation="border"
                  size="sm"
                  role="status"
                  aria-hidden="true"
                  className="me-2"
                />
              )}
              {t('users.resetLink.submit')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </Modal>
  )
}
