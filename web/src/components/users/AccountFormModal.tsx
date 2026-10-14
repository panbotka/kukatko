import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type InstanceRole, MIN_PASSWORD_LENGTH } from '../../services/auth'
import {
  type AccountProfile,
  type AdminUser,
  createUser,
  MAX_NOTE_LENGTH,
  MAX_USERNAME_LENGTH,
  renameUser,
  resetUserPassword,
  updateUser,
} from '../../services/users'
import Modal from '../Modal'

import { editPatch } from './account'
import { type FormError, type FormField, fieldErrorFor } from './errors'

/**
 * Lets a form sit between `.modal-content` and its header/body/footer inside a
 * `scrollable` / `fullscreen="sm-down"` dialog without breaking the pinned
 * footer: Bootstrap pins it by making `.modal-content` a height-capped flex
 * column whose body is the one part that scrolls, and a plain `<form>` would size
 * to its content and push the footer under the keyboard. A flex column that may
 * shrink (`overflow-hidden` gives it a zero automatic minimum) hands the
 * constraint straight through to the body again.
 */
const MODAL_FORM_CLASS = 'd-flex flex-column overflow-hidden'

/** The account being edited: its record as the account list holds it. */
export type EditedAccount = AccountProfile & { username: string; disabled: boolean }

/** Props of {@link AccountFormModal}. */
export interface AccountFormModalProps {
  /** The account being edited, or null to create a new one. */
  account: EditedAccount | null
  onHide: () => void
  /** Called with the saved record once everything the dialog sent was saved. */
  onSaved: (user: AdminUser) => void
  /**
   * Called when a rename went through but the profile update after it did not,
   * so the list can show the name the account really has while the dialog stays
   * open on the error.
   */
  onRenamed: (user: AdminUser) => void
}

/**
 * The system administrator's create/edit dialog for one account record
 * (`POST`/`PATCH /admin/users`, the rename through `PUT …/username`).
 *
 * It edits the account and nothing project-shaped: creating asks for the
 * username, password, e-mail, instance role, name and note, and makes an account
 * with **no membership** — the dialog says where it joins a project. Editing
 * offers the username, e-mail, name and note; the instance role is changed by
 * the list's own promote/demote button behind its confirmation and the block by
 * its own action. The update is a patch of only the fields changed here (see
 * {@link editPatch}), so a dialog opened before another administrator blocked
 * or promoted the account cannot undo that. A changed username
 * goes to its own endpoint before the profile update, so a taken name stops the
 * save with the message under the field and nothing else changes.
 *
 * Validation errors from the API land next to the input that caused them. On a
 * phone the dialog is a full-screen sheet whose body alone scrolls, so the
 * Save/Cancel pair stays pinned above the on-screen keyboard.
 */
export function AccountFormModal({ account, onHide, onSaved, onRenamed }: AccountFormModalProps) {
  const { t } = useTranslation()
  const creating = account === null

  const [username, setUsername] = useState(account?.username ?? '')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState(account?.display_name ?? '')
  const [email, setEmail] = useState(account?.email ?? '')
  const [instanceRole, setInstanceRole] = useState<InstanceRole>(account?.instance_role ?? 'user')
  const [note, setNote] = useState(account?.note ?? '')
  const [validated, setValidated] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<FormError | null>(null)
  // The name the account holds right now. It starts as the row's and moves when
  // a rename went through but the profile update after it failed, so a second
  // Save does not send the rename again.
  const [storedUsername, setStoredUsername] = useState(account?.username ?? '')

  const usernameMissing = username.trim() === ''
  const passwordTooShort = password.length < MIN_PASSWORD_LENGTH
  // Every account receives mail — approval, password reset — so the backend
  // requires an address on create *and* on update; an edit may not clear it.
  const emailMissing = email.trim() === ''

  /** Sends the rename (when the name changed) and the profile update of `edited`. */
  async function saveEdit(edited: EditedAccount): Promise<AdminUser> {
    // The backend lower-cases and trims the name; comparing in that form keeps
    // "Jan " from being sent as a rename of "jan". The untouched-field check
    // comes first so a legacy mixed-case name nobody edited is never quietly
    // lower-cased by an unrelated save.
    const renaming = username !== storedUsername && username.trim().toLowerCase() !== storedUsername
    if (renaming) {
      const renamed = await renameUser(edited.uid, username)
      setStoredUsername(renamed.username)
      onRenamed(renamed)
    }
    // Only what was changed here: an empty patch writes nothing and answers
    // the account as it is now, which also refreshes the row after a rename.
    return updateUser(
      edited.uid,
      editPatch(edited, { display_name: displayName, email: email.trim(), note }),
    )
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (emailMissing || usernameMissing || (creating && passwordTooShort)) {
      setValidated(true)
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      const saved = creating
        ? await createUser({
            username: username.trim(),
            password,
            display_name: displayName,
            email: email.trim(),
            instance_role: instanceRole,
            note,
          })
        : await saveEdit(account)
      onSaved(saved)
    } catch (err) {
      setError(fieldErrorFor(err))
      setSubmitting(false)
    }
  }

  /** Renders the inline message for `field`, or the client-side fallback. */
  function feedbackFor(field: FormField, fallback: string) {
    if (error?.field === field) {
      return t(error.messageKey, {
        min: MIN_PASSWORD_LENGTH,
        max: field === 'username' ? MAX_USERNAME_LENGTH : MAX_NOTE_LENGTH,
      })
    }
    return fallback
  }

  return (
    <Modal show onHide={onHide} centered scrollable fullscreen="sm-down">
      <Form
        noValidate
        validated={validated}
        className={MODAL_FORM_CLASS}
        onSubmit={(event) => {
          void handleSubmit(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h2" className="kk-section-title mb-0">
            {creating
              ? t('systemUsers.form.createTitle')
              : t('systemUsers.form.editTitle', { username: account.username })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error?.field === null && (
            <Alert variant="danger" role="alert">
              {t(error.messageKey, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
            </Alert>
          )}

          <Form.Group className="mb-3" controlId="account-username">
            <Form.Label>{t('systemUsers.form.username')}</Form.Label>
            <Form.Control
              type="text"
              autoComplete="off"
              required
              maxLength={MAX_USERNAME_LENGTH}
              isInvalid={error?.field === 'username' || (validated && usernameMissing)}
              value={username}
              onChange={(event) => {
                setUsername(event.target.value)
              }}
              disabled={submitting}
            />
            {!creating && (
              <Form.Text className="text-secondary">
                {t('systemUsers.form.usernameRenameHint')}
              </Form.Text>
            )}
            <Form.Control.Feedback type="invalid">
              {feedbackFor('username', t('systemUsers.form.usernameRequired'))}
            </Form.Control.Feedback>
          </Form.Group>

          {creating && (
            <Form.Group className="mb-3" controlId="account-password">
              <Form.Label>{t('systemUsers.form.password')}</Form.Label>
              <Form.Control
                type="password"
                autoComplete="new-password"
                required
                minLength={MIN_PASSWORD_LENGTH}
                isInvalid={error?.field === 'password' || (validated && passwordTooShort)}
                value={password}
                onChange={(event) => {
                  setPassword(event.target.value)
                }}
                disabled={submitting}
              />
              <Form.Text className="text-secondary">
                {t('systemUsers.form.passwordHint', { min: MIN_PASSWORD_LENGTH })}
              </Form.Text>
              <Form.Control.Feedback type="invalid">
                {feedbackFor(
                  'password',
                  t('systemUsers.errors.passwordTooShort', { min: MIN_PASSWORD_LENGTH }),
                )}
              </Form.Control.Feedback>
            </Form.Group>
          )}

          <Form.Group className="mb-3" controlId="account-email">
            <Form.Label>{t('systemUsers.form.email')}</Form.Label>
            <Form.Control
              type="email"
              autoComplete="off"
              required
              isInvalid={error?.field === 'email' || (validated && emailMissing)}
              value={email}
              onChange={(event) => {
                setEmail(event.target.value)
              }}
              disabled={submitting}
            />
            <Form.Text className="text-secondary">{t('systemUsers.form.emailHint')}</Form.Text>
            <Form.Control.Feedback type="invalid">
              {feedbackFor('email', t('systemUsers.form.emailRequired'))}
            </Form.Control.Feedback>
          </Form.Group>

          {creating && (
            <Form.Group className="mb-3" controlId="account-instance-role">
              <Form.Label>{t('systemUsers.form.instanceRole')}</Form.Label>
              <Form.Select
                value={instanceRole}
                isInvalid={error?.field === 'instanceRole'}
                onChange={(event) => {
                  setInstanceRole(event.target.value === 'maintainer' ? 'maintainer' : 'user')
                }}
                disabled={submitting}
              >
                <option value="user">{t('systemUsers.instanceRoles.user')}</option>
                <option value="maintainer">{t('systemUsers.instanceRoles.maintainer')}</option>
              </Form.Select>
              <Form.Text className="text-secondary">
                {t('systemUsers.form.instanceRoleHint')}
              </Form.Text>
              <Form.Control.Feedback type="invalid">
                {feedbackFor('instanceRole', t('systemUsers.errors.invalidRole'))}
              </Form.Control.Feedback>
            </Form.Group>
          )}

          <Form.Group className="mb-3" controlId="account-display-name">
            <Form.Label>{t('systemUsers.form.displayName')}</Form.Label>
            <Form.Control
              type="text"
              autoComplete="off"
              value={displayName}
              onChange={(event) => {
                setDisplayName(event.target.value)
              }}
              disabled={submitting}
            />
          </Form.Group>

          <Form.Group controlId="account-note">
            <Form.Label>{t('systemUsers.form.note')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={3}
              maxLength={MAX_NOTE_LENGTH}
              isInvalid={error?.field === 'note'}
              value={note}
              onChange={(event) => {
                setNote(event.target.value)
              }}
              disabled={submitting}
            />
            <Form.Text className="text-secondary">{t('systemUsers.form.noteHint')}</Form.Text>
            <Form.Control.Feedback type="invalid">
              {feedbackFor('note', t('systemUsers.errors.noteTooLong', { max: MAX_NOTE_LENGTH }))}
            </Form.Control.Feedback>
          </Form.Group>

          {creating && (
            <p className="text-secondary small mt-3 mb-0">{t('systemUsers.form.noProjectHint')}</p>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onHide} disabled={submitting}>
            {t('systemUsers.form.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting && (
              <Spinner
                animation="border"
                size="sm"
                role="status"
                aria-hidden="true"
                className="me-2"
              />
            )}
            {creating ? t('systemUsers.form.submitCreate') : t('systemUsers.form.submitSave')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

/** Props of {@link PasswordModal}. */
export interface PasswordModalProps {
  /** The account whose password is set: its uid and the name the title shows. */
  account: { uid: string; username: string }
  onHide: () => void
  /** Called once the new password is stored. */
  onDone: () => void
}

/**
 * Sets another account's password (`POST /admin/users/{uid}/password`). It
 * never shows the current one — the backend only ever stores a bcrypt hash and
 * never serialises it — and the change signs the account out of every session.
 * The reset link is the gentler sibling: the person chooses their own password
 * and the administrator never learns it.
 *
 * Shaped like {@link AccountFormModal}: a full-screen sheet with a scrolling body
 * on a phone, so the two password fields never push the submit button under the
 * on-screen keyboard.
 */
export function PasswordModal({ account, onHide, onDone }: PasswordModalProps) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [validated, setValidated] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<FormError | null>(null)

  const tooShort = password.length < MIN_PASSWORD_LENGTH
  const mismatch = confirm !== password

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (tooShort || mismatch) {
      setValidated(true)
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      await resetUserPassword(account.uid, password)
      onDone()
    } catch (err) {
      setError(fieldErrorFor(err))
      setSubmitting(false)
    }
  }

  return (
    <Modal show onHide={onHide} centered scrollable fullscreen="sm-down">
      <Form
        noValidate
        validated={validated}
        className={MODAL_FORM_CLASS}
        onSubmit={(event) => {
          void handleSubmit(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h2" className="kk-section-title mb-0">
            {t('systemUsers.password.title', { username: account.username })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error?.field === null && (
            <Alert variant="danger" role="alert">
              {t(error.messageKey, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
            </Alert>
          )}
          <p className="text-secondary small">{t('systemUsers.password.hint')}</p>

          <Form.Group className="mb-3" controlId="account-new-password">
            <Form.Label>{t('systemUsers.password.newPassword')}</Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              required
              minLength={MIN_PASSWORD_LENGTH}
              isInvalid={error?.field === 'password' || (validated && tooShort)}
              value={password}
              onChange={(event) => {
                setPassword(event.target.value)
              }}
              disabled={submitting}
            />
            <Form.Control.Feedback type="invalid">
              {t('systemUsers.errors.passwordTooShort', { min: MIN_PASSWORD_LENGTH })}
            </Form.Control.Feedback>
          </Form.Group>

          <Form.Group controlId="account-confirm-password">
            <Form.Label>{t('systemUsers.password.confirmPassword')}</Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              required
              isInvalid={validated && mismatch}
              value={confirm}
              onChange={(event) => {
                setConfirm(event.target.value)
              }}
              disabled={submitting}
            />
            <Form.Control.Feedback type="invalid">
              {t('systemUsers.password.mismatch')}
            </Form.Control.Feedback>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={onHide} disabled={submitting}>
            {t('systemUsers.form.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting && (
              <Spinner
                animation="border"
                size="sm"
                role="status"
                aria-hidden="true"
                className="me-2"
              />
            )}
            {t('systemUsers.password.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}
