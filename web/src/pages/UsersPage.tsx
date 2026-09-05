import { type SyntheticEvent, useCallback, useEffect, useId, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Placeholder from 'react-bootstrap/Placeholder'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import Modal from '../components/Modal'
import { ReasonedButton } from '../components/ReasonedButton'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { AddAutocomplete } from '../components/photo/AddAutocomplete'
import { PendingFilter } from '../components/users/PendingFilter'
import { ResetLinkModal } from '../components/users/ResetLinkModal'
import { UserEmail } from '../components/users/UserEmail'
import { UserStateBadges } from '../components/users/UserStateBadges'
import { isWaitingForApproval } from '../components/users/account'
import {
  actionErrorFor,
  fieldErrorFor,
  type ErrorKey,
  type FormError,
  type FormField,
} from '../components/users/errors'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useSubjects } from '../hooks/useSubjects'
import { formatDate, formatDateTime } from '../lib/format'
import { MIN_PASSWORD_LENGTH, type Role } from '../services/auth'
import {
  approveUser,
  createUser,
  fetchUsers,
  MAX_NOTE_LENGTH,
  resetUserPassword,
  ROLES,
  setUserDisabled,
  updateUser,
  type AdminUser,
} from '../services/users'

/** Fetch lifecycle of the user list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; users: AdminUser[] }

/** Which dialog is open, and over which row. */
type Dialog =
  | { kind: 'none' }
  | { kind: 'create' }
  | { kind: 'edit'; user: AdminUser }
  | { kind: 'password'; user: AdminUser }
  | { kind: 'resetLink'; user: AdminUser }
  | { kind: 'approve'; user: AdminUser }
  | { kind: 'toggle'; user: AdminUser }

/** The skeleton's three placeholder rows. */
const SKELETON_ROWS = ['a', 'b', 'c']

/** One placeholder bar per table column, roughly as wide as the real content. */
const SKELETON_CELLS: { column: string; width: number }[] = [
  { column: 'username', width: 9 },
  { column: 'displayName', width: 7 },
  { column: 'email', width: 8 },
  { column: 'role', width: 4 },
  { column: 'state', width: 4 },
  { column: 'subject', width: 6 },
  { column: 'note', width: 8 },
  { column: 'lastLogin', width: 6 },
  { column: 'created', width: 6 },
  { column: 'actions', width: 5 },
]

/** The table-shaped loading skeleton shown while the first fetch is in flight. */
function UsersSkeleton() {
  const { t } = useTranslation()
  return (
    <div role="status" aria-live="polite">
      <span className="visually-hidden">{t('users.loading')}</span>
      <Table responsive className="mb-0">
        <tbody>
          {SKELETON_ROWS.map((row) => (
            <tr key={row}>
              {SKELETON_CELLS.map((cell) => (
                <td key={cell.column}>
                  <Placeholder animation="glow">
                    <Placeholder xs={cell.width} />
                  </Placeholder>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  )
}

/**
 * The classes a `<Form>` needs when it wraps a whole modal — header, body and
 * footer — inside a `scrollable` / `fullscreen="sm-down"` dialog.
 *
 * Bootstrap pins the footer by making `.modal-content` a height-capped flex
 * column whose `.modal-body` is the one part allowed to scroll. A `<form>`
 * between the two breaks that chain: it would size to its content and push the
 * footer past the bottom of the screen (or under the keyboard). Making the form
 * a flex column that may shrink — `overflow-hidden` turns it into a scroll
 * container, whose automatic minimum size is zero — hands the constraint
 * straight through to the body again.
 */
const MODAL_FORM_CLASS = 'd-flex flex-column overflow-hidden'

/**
 * The account's link to a person of the library, as one form field: the app's
 * ordinary subject typeahead when nothing is chosen, and the chosen name with a
 * "clear" beside it once something is.
 *
 * It cannot create a person. An account belongs to somebody the library already
 * knows, and minting an empty subject from a user dialog would leave a record
 * with no photographs on it.
 *
 * The warning under it is not decoration: linking publishes that person's cover
 * photo beside everything the account writes, and an administrator setting it
 * for somebody else is the one who needs to know that.
 */
function SubjectField({
  value,
  disabled,
  onChange,
}: {
  value: string | null
  disabled: boolean
  onChange: (uid: string | null) => void
}) {
  const { t } = useTranslation()
  const { subjects, loading } = useSubjects()
  const chosen = subjects.find((candidate) => candidate.uid === value)

  return (
    <Form.Group className="mb-3" controlId="user-subject">
      <Form.Label>{t('users.form.subject')}</Form.Label>
      {value === null ? (
        <AddAutocomplete
          id="user-subject-picker"
          label={t('users.form.subjectPick')}
          disabled={loading || disabled}
          options={subjects.map((candidate) => ({
            uid: candidate.uid,
            label: candidate.name,
            hint: String(candidate.photo_count),
          }))}
          onAdd={onChange}
        />
      ) : (
        <div className="d-flex align-items-center gap-2 flex-wrap mb-1">
          <span>{chosen === undefined ? t('users.form.subjectUnknown') : chosen.name}</span>
          <Button
            type="button"
            variant="link"
            size="sm"
            disabled={disabled}
            onClick={() => {
              onChange(null)
            }}
          >
            {t('users.form.subjectClear')}
          </Button>
        </div>
      )}
      <Form.Text className="text-secondary">{t('users.form.subjectHint')}</Form.Text>
    </Form.Group>
  )
}

/**
 * Renders one account's linked person for the roster: the person's name, an em
 * dash when there is no link, and the bare UID in the moment between the roster
 * arriving and the people list arriving (or if that list failed) — a raw id is
 * ugly, but claiming "nobody" would be wrong.
 *
 * A link whose subject has been deleted cannot happen: the database clears it.
 * It is a plain function of the already-loaded name map rather than a component
 * with a hook, so a roster of thirty accounts still costs exactly one request
 * for the people.
 */
function linkedPersonLabel(subjectUid: string | null | undefined, names: Map<string, string>) {
  if (subjectUid === null || subjectUid === undefined || subjectUid === '') {
    return '—'
  }
  return names.get(subjectUid) ?? subjectUid
}

/** Props shared by the create and edit dialogs. */
interface UserFormModalProps {
  /** The row being edited, or null to create a new user. */
  user: AdminUser | null
  /**
   * Whether the signed-in actor is a maintainer. Only a maintainer may grant the
   * `maintainer` role, so a non-maintainer's role selector omits that option.
   */
  isMaintainer: boolean
  onHide: () => void
  onSaved: (user: AdminUser) => void
}

/**
 * The create/edit dialog. Creating asks for a username and password on top of
 * the shared profile fields; editing renders the username read-only, because the
 * backend has no way to change it and pretending otherwise would be a lie.
 *
 * Validation errors from the API land next to the input that caused them rather
 * than in a banner, so the reader does not have to guess which field to fix.
 *
 * On a phone the dialog is a full-screen sheet and only its body scrolls, so the
 * six fields get the whole small screen and the Save/Cancel pair stays pinned
 * above the on-screen keyboard rather than under it; on a wider screen it is the
 * same centred card as before. Its wrapping form carries {@link MODAL_FORM_CLASS}.
 */
function UserFormModal({ user, isMaintainer, onHide, onSaved }: UserFormModalProps) {
  const { t } = useTranslation()
  const creating = user === null

  // Granting the top-of-ladder maintainer role is a maintainer-only power
  // (mirrors the backend `authorizeUserManagement`); everyone else is offered
  // viewer/editor/admin. Editing a maintainer's account is blocked upstream, so
  // this filtered list never has to represent a role the select cannot show.
  const availableRoles = isMaintainer ? ROLES : ROLES.filter((value) => value !== 'maintainer')

  const [username, setUsername] = useState(user?.username ?? '')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState(user?.display_name ?? '')
  const [email, setEmail] = useState(user?.email ?? '')
  const [role, setRole] = useState<Role>(user?.role ?? 'viewer')
  const [note, setNote] = useState(user?.note ?? '')
  const [subjectUid, setSubjectUid] = useState<string | null>(user?.subject_uid ?? null)
  const [validated, setValidated] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<FormError | null>(null)

  const usernameMissing = username.trim() === ''
  const passwordTooShort = password.length < MIN_PASSWORD_LENGTH
  // Every account receives mail — approval, password reset — so the backend
  // requires an address on create *and* on update; an edit may not clear it.
  const emailMissing = email.trim() === ''

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (emailMissing || (creating && (usernameMissing || passwordTooShort))) {
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
            role,
            note,
            subject_uid: subjectUid,
          })
        : await updateUser(user.uid, {
            display_name: displayName,
            email: email.trim(),
            // The update replaces the whole profile, so the fields this dialog
            // does not offer are echoed back unchanged.
            role,
            disabled: user.disabled,
            note,
            subject_uid: subjectUid,
          })
      onSaved(saved)
    } catch (err) {
      setError(fieldErrorFor(err))
      setSubmitting(false)
    }
  }

  /** Renders the inline message for `field`, or the client-side fallback. */
  function feedbackFor(field: FormField, fallback: string) {
    if (error?.field === field) {
      return t(error.messageKey, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })
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
            {creating ? t('users.form.createTitle') : t('users.form.editTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error?.field === null && (
            <Alert variant="danger" role="alert">
              {t(error.messageKey, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
            </Alert>
          )}

          <Form.Group className="mb-3" controlId="user-username">
            <Form.Label>{t('users.form.username')}</Form.Label>
            <Form.Control
              type="text"
              autoComplete="off"
              required
              readOnly={!creating}
              plaintext={!creating}
              isInvalid={error?.field === 'username' || (validated && usernameMissing)}
              value={username}
              onChange={(event) => {
                setUsername(event.target.value)
              }}
              disabled={submitting}
            />
            {!creating && (
              <Form.Text className="text-secondary">{t('users.form.usernameImmutable')}</Form.Text>
            )}
            <Form.Control.Feedback type="invalid">
              {feedbackFor('username', t('users.form.usernameRequired'))}
            </Form.Control.Feedback>
          </Form.Group>

          {creating && (
            <Form.Group className="mb-3" controlId="user-password">
              <Form.Label>{t('users.form.password')}</Form.Label>
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
                {t('users.form.passwordHint', { min: MIN_PASSWORD_LENGTH })}
              </Form.Text>
              <Form.Control.Feedback type="invalid">
                {feedbackFor(
                  'password',
                  t('users.errors.passwordTooShort', { min: MIN_PASSWORD_LENGTH }),
                )}
              </Form.Control.Feedback>
            </Form.Group>
          )}

          {/* Required in both modes: the backend refuses an account without a
              usable address, and an edit that cleared it would be refused too. */}
          <Form.Group className="mb-3" controlId="user-email">
            <Form.Label>{t('users.form.email')}</Form.Label>
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
            <Form.Text className="text-secondary">{t('users.form.emailHint')}</Form.Text>
            <Form.Control.Feedback type="invalid">
              {feedbackFor('email', t('users.form.emailRequired'))}
            </Form.Control.Feedback>
          </Form.Group>

          <Form.Group className="mb-3" controlId="user-role">
            <Form.Label>{t('users.form.role')}</Form.Label>
            <Form.Select
              value={role}
              isInvalid={error?.field === 'role'}
              onChange={(event) => {
                setRole(event.target.value as Role)
              }}
              disabled={submitting}
            >
              {availableRoles.map((value) => (
                <option key={value} value={value}>
                  {t(`roles.${value}`)}
                </option>
              ))}
            </Form.Select>
            <Form.Control.Feedback type="invalid">
              {feedbackFor('role', t('users.errors.invalidRole'))}
            </Form.Control.Feedback>
          </Form.Group>

          <Form.Group className="mb-3" controlId="user-display-name">
            <Form.Label>{t('users.form.displayName')}</Form.Label>
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

          {/* Which person of the library this account is. An administrator sets
              it here for somebody else; the user sets their own on the account
              page. Both publish that person's face beside the account's
              comments, which is why the field says so. */}
          <SubjectField value={subjectUid} disabled={submitting} onChange={setSubjectUid} />

          <Form.Group controlId="user-note">
            <Form.Label>{t('users.form.note')}</Form.Label>
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
            <Form.Text className="text-secondary">{t('users.form.noteHint')}</Form.Text>
            <Form.Control.Feedback type="invalid">
              {feedbackFor('note', t('users.errors.noteTooLong', { max: MAX_NOTE_LENGTH }))}
            </Form.Control.Feedback>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={submitting}>
            {t('users.form.cancel')}
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
            {creating ? t('users.form.submitCreate') : t('users.form.submitSave')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

/** Props for the password-reset dialog. */
interface PasswordModalProps {
  user: AdminUser
  onHide: () => void
  onDone: () => void
}

/**
 * Sets another user's password. It never shows the current one — the backend
 * only ever stores a bcrypt hash and never serialises it — and the reset signs
 * the target out of every session.
 *
 * Shaped like {@link UserFormModal}: a full-screen sheet with a scrolling body on
 * a phone, so the two password fields never push the submit button under the
 * on-screen keyboard.
 */
function PasswordModal({ user, onHide, onDone }: PasswordModalProps) {
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
      await resetUserPassword(user.uid, password)
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
            {t('users.password.title', { username: user.username })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error?.field === null && (
            <Alert variant="danger" role="alert">
              {t(error.messageKey, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
            </Alert>
          )}
          <p className="text-secondary small">{t('users.password.hint')}</p>

          <Form.Group className="mb-3" controlId="user-new-password">
            <Form.Label>{t('users.password.newPassword')}</Form.Label>
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
              {t('users.errors.passwordTooShort', { min: MIN_PASSWORD_LENGTH })}
            </Form.Control.Feedback>
          </Form.Group>

          <Form.Group controlId="user-confirm-password">
            <Form.Label>{t('users.password.confirmPassword')}</Form.Label>
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
              {t('users.password.mismatch')}
            </Form.Control.Feedback>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={submitting}>
            {t('users.form.cancel')}
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
            {t('users.password.submit')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}

/** Props for the enable/disable confirmation dialog. */
interface ToggleModalProps {
  user: AdminUser
  onHide: () => void
  onConfirm: () => void
  busy: boolean
}

/**
 * The confirmation step in front of enabling or disabling an account. Disabling
 * signs the user out everywhere, so it is never one stray click away.
 *
 * A question with no inputs, so it follows `ConfirmModal` rather than the form
 * dialogs above: `scrollable` to keep the two buttons pinned, but a centred card
 * on every screen — a phone-wide sheet for one sentence reads as a page.
 */
function ToggleModal({ user, onHide, onConfirm, busy }: ToggleModalProps) {
  const { t } = useTranslation()
  const enabling = user.disabled
  return (
    <Modal show onHide={onHide} centered scrollable>
      <Modal.Header closeButton>
        <Modal.Title as="h2" className="kk-section-title mb-0">
          {enabling ? t('users.confirm.enableTitle') : t('users.confirm.disableTitle')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {enabling
          ? t('users.confirm.enableBody', { username: user.username })
          : t('users.confirm.disableBody', { username: user.username })}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={busy}>
          {t('users.form.cancel')}
        </Button>
        <Button variant={enabling ? 'success' : 'danger'} onClick={onConfirm} disabled={busy}>
          {busy && (
            <Spinner
              animation="border"
              size="sm"
              role="status"
              aria-hidden="true"
              className="me-2"
            />
          )}
          {enabling ? t('users.enable') : t('users.disable')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}

/** Props for the per-user action cluster. */
interface UserActionsProps {
  user: AdminUser
  /** True when the row is the signed-in administrator's own account. */
  self: boolean
  /**
   * True when the signed-in actor may manage this account. A non-maintainer
   * cannot touch a maintainer's account (edit, reset the password of, approve or
   * disable), so its actions are disabled — mirroring the backend
   * `guardMaintainerBoundary`.
   */
  canManage: boolean
  /**
   * True on a phone card, where the actions are the whole point of the
   * reflow: they become one full-width button per line at the finger-friendly
   * height instead of a `size="sm"` cluster the reader would have to scroll to.
   */
  stacked: boolean
  onApprove: () => void
  onEdit: () => void
  onPassword: () => void
  onResetLink: () => void
  onToggle: () => void
}

/**
 * The things an administrator does to an account, plus the one-line reason when a
 * control is disabled. Shared by both layouts so the table cell and the card's
 * action row can never offer different powers.
 *
 * **Approve appears only on a waiting row.** It is not a power an administrator
 * has over every account but the answer to a question one particular account is
 * asking, so on an account that was already let in there is nothing to press and
 * nothing to grey out.
 */
function UserActions({
  user,
  self,
  canManage,
  stacked,
  onApprove,
  onEdit,
  onPassword,
  onResetLink,
  onToggle,
}: UserActionsProps) {
  const { t } = useTranslation()
  const size = stacked ? undefined : 'sm'
  const hintId = useId()
  const approveHintId = useId()
  // Not a role gate but a per-row boundary: this administrator may manage users,
  // just not *this* one. That is why the buttons stay on the row instead of
  // vanishing — and why, per the app-wide rule, they have to say why they are
  // off. `ReasonedButton` is what makes that sentence actually reachable: a
  // natively disabled Bootstrap button takes no focus and shows no `title`.
  const outOfReach = canManage ? undefined : t('users.maintainerManageHint')
  // Why a control is dead, in one line under it: the reason belongs next to the
  // button, not only in a `title` a touch device never shows. Every reason a
  // button in this cluster can have is the one printed there, so they all
  // describe themselves by that line rather than each carrying a hidden copy of
  // the same sentence for a screen reader to repeat.
  const hint = self
    ? t('users.selfDisableHint')
    : canManage
      ? null
      : t('users.maintainerManageHint')
  const waiting = isWaitingForApproval(user)
  // Approving a blocked account is refused by the backend (409) and would be a
  // half-measure anyway: the person still could not sign in. The row says so
  // instead of offering a button that fails — on its own hint line, because it
  // is a different sentence from the one the rest of the cluster shares.
  const blocked = user.disabled ? t('users.approve.blockedHint') : undefined
  const approveOff = outOfReach ?? blocked
  const buttons = (
    <>
      {waiting && (
        <ReasonedButton
          variant="outline-success"
          size={size}
          disabledReason={approveOff}
          reasonId={outOfReach === undefined ? approveHintId : hintId}
          onClick={onApprove}
        >
          {t('users.approve.action')}
        </ReasonedButton>
      )}
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={outOfReach}
        reasonId={hintId}
        onClick={onEdit}
      >
        {t('users.edit')}
      </ReasonedButton>
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={outOfReach}
        reasonId={hintId}
        onClick={onPassword}
      >
        {t('users.changePassword')}
      </ReasonedButton>
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={outOfReach}
        reasonId={hintId}
        onClick={onResetLink}
      >
        {t('users.resetLink.action')}
      </ReasonedButton>
      <ReasonedButton
        variant={user.disabled ? 'outline-success' : 'outline-danger'}
        size={size}
        disabledReason={self ? t('users.selfDisableHint') : outOfReach}
        reasonId={hintId}
        onClick={onToggle}
      >
        {user.disabled ? t('users.enable') : t('users.disable')}
      </ReasonedButton>
    </>
  )
  return (
    <>
      {/* On a card the buttons are the grid items of the card's own full-width
          action row, so they must not be boxed in a second wrapper; in a table
          cell they need their own inline cluster. */}
      {stacked ? buttons : <div className="d-flex gap-1 flex-wrap">{buttons}</div>}
      {hint !== null && (
        <div id={hintId} className="text-secondary small mt-1">
          {hint}
        </div>
      )}
      {/* Only when Approve is the one control that is off: the shared line above
          already covers the case where nothing on the row may be pressed. */}
      {waiting && outOfReach === undefined && blocked !== undefined && (
        <div id={approveHintId} className="text-secondary small mt-1">
          {blocked}
        </div>
      )}
    </>
  )
}

/**
 * Admin-only user administration: the roster of local accounts and the things an
 * administrator does to them — create one, edit its role/name/note, let a waiting
 * one in, set its password or hand out a link so its owner can set their own, and
 * retire it by disabling.
 *
 * Accounts are never deleted. Photos, albums, ratings and audit entries all
 * point at a user; deleting one would either orphan that history or erase it, so
 * disabling is the supported way to retire an account. An administrator cannot
 * disable their own account either, which would lock the instance's last admin
 * out of it.
 */
export function UsersPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('users.title'))
  const { isAdmin, isMaintainer, user: me } = useAuth()
  // The people, once for the whole roster, so the "linked person" column can
  // print a name instead of a UID without costing a request per row.
  const { subjects } = useSubjects()
  const subjectNames = useMemo(
    () => new Map(subjects.map((subject) => [subject.uid, subject.name])),
    [subjects],
  )
  const [state, setState] = useState<State>({ status: 'loading' })
  const [dialog, setDialog] = useState<Dialog>({ kind: 'none' })
  const [toggling, setToggling] = useState(false)
  const [approving, setApproving] = useState(false)
  // Narrows the roster to the accounts waiting to be let in. It is view state of
  // the already-loaded list, not a query: switching it re-renders and never
  // re-fetches, so the filter cannot fail and cannot empty the page.
  const [pendingOnly, setPendingOnly] = useState(false)
  // The enable/disable action has no form to hang a field error on, so it keeps
  // just the message key. It is a real message rather than a boolean because the
  // backend refuses disabling the last maintainer, and "the action could not be
  // completed" would leave the reader with no idea what to do about it.
  const [actionError, setActionError] = useState<ErrorKey | null>(null)
  const [notice, setNotice] = useState<'passwordChanged' | 'approved' | null>(null)

  const load = useCallback((signal?: AbortSignal) => {
    setState({ status: 'loading' })
    fetchUsers(signal)
      .then((users) => {
        setState({ status: 'ready', users })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
  }, [])

  useEffect(() => {
    if (!isAdmin) {
      return undefined
    }
    const controller = new AbortController()
    load(controller.signal)
    return () => {
      controller.abort()
    }
  }, [isAdmin, load])

  const close = useCallback(() => {
    setDialog({ kind: 'none' })
  }, [])

  /** Merges a created or updated user into the list, keeping username order. */
  const upsert = useCallback((saved: AdminUser) => {
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      const known = prev.users.some((u) => u.uid === saved.uid)
      const users = known
        ? prev.users.map((u) => (u.uid === saved.uid ? saved : u))
        : [...prev.users, saved].sort((a, b) => a.username.localeCompare(b.username))
      return { status: 'ready', users }
    })
  }, [])

  /** Lets a waiting account in, replacing its row with the approved one. */
  async function confirmApprove(user: AdminUser) {
    setActionError(null)
    setApproving(true)
    try {
      upsert(await approveUser(user.uid))
      setNotice('approved')
    } catch (err) {
      // The roster is untouched: the row still reads "waiting", so the same
      // button is still there once the reader has dealt with the reason.
      setActionError(actionErrorFor(err))
    } finally {
      setApproving(false)
      // Either way the dialog closes: a modal backdrop would hide the alert.
      close()
    }
  }

  async function confirmToggle(user: AdminUser) {
    setActionError(null)
    setToggling(true)
    try {
      upsert(await setUserDisabled(user, !user.disabled))
    } catch (err) {
      setActionError(fieldErrorFor(err).messageKey)
    } finally {
      setToggling(false)
      // Either way the dialog closes: a modal backdrop would hide the error alert.
      close()
    }
  }

  if (!isAdmin) {
    return <Alert variant="danger">{t('users.adminOnly')}</Alert>
  }

  /** The action-cluster props for one account — the same on a row and on a card. */
  function actionsFor(user: AdminUser) {
    return {
      user,
      self: user.uid === me?.uid,
      canManage: isMaintainer || user.role !== 'maintainer',
      onApprove: () => {
        setDialog({ kind: 'approve', user })
      },
      onEdit: () => {
        setDialog({ kind: 'edit', user })
      },
      onPassword: () => {
        setDialog({ kind: 'password', user })
      },
      onResetLink: () => {
        setDialog({ kind: 'resetLink', user })
      },
      onToggle: () => {
        setDialog({ kind: 'toggle', user })
      },
    }
  }

  // The whole roster, what the filter leaves of it, and how many accounts are
  // waiting — the count is of everybody, never of what is on screen, so turning
  // the filter on cannot make the reminder disappear.
  const all = state.status === 'ready' ? state.users : []
  const pendingCount = all.filter(isWaitingForApproval).length
  const visible = pendingOnly ? all.filter(isWaitingForApproval) : all

  // One definition drives both layouts: the table columns on a tablet or
  // desktop, and the same fields as "label: value" lines on a phone card.
  const columns: RecordColumn<AdminUser>[] = [
    {
      key: 'username',
      header: t('users.columns.username'),
      cellClassName: 'fw-semibold text-break',
      cell: (user) => user.username,
    },
    {
      key: 'displayName',
      header: t('users.columns.displayName'),
      cellClassName: 'text-break',
      cell: (user) => user.display_name || '—',
    },
    {
      // Every message the app sends goes here, so it belongs on the roster and
      // not only inside the edit dialog: an account nobody can be reached at is
      // something the administrator has to be able to *see*.
      key: 'email',
      header: t('users.columns.email'),
      cellClassName: 'text-break',
      cell: (user) => <UserEmail email={user.email} />,
    },
    {
      key: 'role',
      header: t('users.columns.role'),
      cell: (user) => <Badge bg="secondary">{t(`roles.${user.role}`)}</Badge>,
    },
    {
      key: 'state',
      header: t('users.columns.state'),
      cell: (user) => <UserStateBadges user={user} />,
    },
    {
      key: 'subject',
      header: t('users.columns.subject'),
      cellClassName: 'text-break',
      cell: (user) => linkedPersonLabel(user.subject_uid, subjectNames),
    },
    {
      key: 'note',
      header: t('users.columns.note'),
      cellClassName: 'text-secondary small text-break',
      // The note is written in a `<textarea>`, so it comes back with the line
      // breaks its author made; without this they collapse into spaces.
      multiline: true,
      // A long note must not push the actions off the far end of the table; on a
      // card it has the full width and needs no cap.
      cellStyle: { maxWidth: '18rem' },
      cell: (user) => user.note || '—',
    },
    {
      key: 'lastLogin',
      header: t('users.columns.lastLogin'),
      cellClassName: 'text-nowrap',
      cell: (user) =>
        user.last_login_at === undefined
          ? t('users.never')
          : formatDateTime(user.last_login_at, i18n.language),
    },
    {
      key: 'created',
      header: t('users.columns.created'),
      cellClassName: 'text-nowrap',
      cell: (user) => formatDate(user.created_at, i18n.language),
    },
    {
      key: 'actions',
      header: t('users.columns.actions'),
      // On a card the same cluster is the full-width action row instead, so it is
      // not squeezed into the label/value grid.
      cardHidden: true,
      cell: (user) => <UserActions {...actionsFor(user)} stacked={false} />,
    },
  ]

  return (
    <>
      <div className="d-flex justify-content-between align-items-start gap-3 mb-1">
        <h1 className="kk-page-title mb-0">{t('users.title')}</h1>
        <Button
          variant="primary"
          onClick={() => {
            setDialog({ kind: 'create' })
          }}
        >
          {t('users.create')}
        </Button>
      </div>
      <p className="text-secondary">{t('users.subtitle')}</p>

      {actionError !== null && (
        <Alert
          variant="danger"
          role="alert"
          dismissible
          onClose={() => {
            setActionError(null)
          }}
        >
          {t(actionError, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
        </Alert>
      )}
      {notice !== null && (
        <Alert
          variant="success"
          role="alert"
          dismissible
          onClose={() => {
            setNotice(null)
          }}
        >
          {notice === 'passwordChanged' ? t('users.password.success') : t('users.approve.success')}
        </Alert>
      )}

      <Card>
        <Card.Body>
          {state.status === 'loading' && <UsersSkeleton />}

          {state.status === 'error' && (
            <ErrorState
              title={t('users.error')}
              onRetry={() => {
                load()
              }}
            />
          )}

          {/* Unreachable in practice — the bootstrap admin always exists — but a
              backend that returns [] must render a page, not a crash. */}
          {state.status === 'ready' && state.users.length === 0 && (
            <EmptyState title={t('users.empty.title')} hint={t('users.empty.hint')} />
          )}

          {state.status === 'ready' && state.users.length > 0 && (
            <>
              <PendingFilter
                pendingOnly={pendingOnly}
                pendingCount={pendingCount}
                onChange={setPendingOnly}
              />
              {visible.length === 0 ? (
                <EmptyState
                  title={t('users.empty.noPendingTitle')}
                  hint={t('users.empty.noPendingHint')}
                />
              ) : (
                <RecordTable
                  records={visible}
                  columns={columns}
                  rowKey={(user) => user.uid}
                  cardActions={(user) => <UserActions {...actionsFor(user)} stacked />}
                  className="mb-0 align-middle"
                />
              )}
            </>
          )}
        </Card.Body>
      </Card>

      {(dialog.kind === 'create' || dialog.kind === 'edit') && (
        <UserFormModal
          user={dialog.kind === 'edit' ? dialog.user : null}
          isMaintainer={isMaintainer}
          onHide={close}
          onSaved={(saved) => {
            upsert(saved)
            close()
          }}
        />
      )}

      {dialog.kind === 'password' && (
        <PasswordModal
          user={dialog.user}
          onHide={close}
          onDone={() => {
            setNotice('passwordChanged')
            close()
          }}
        />
      )}

      {dialog.kind === 'resetLink' && <ResetLinkModal user={dialog.user} onHide={close} />}

      {dialog.kind === 'approve' && (
        <ConfirmModal
          show
          variant="primary"
          busy={approving}
          title={t('users.approve.title')}
          confirmLabel={t('users.approve.action')}
          onCancel={close}
          onConfirm={() => {
            void confirmApprove(dialog.user)
          }}
        >
          {t('users.approve.body', { username: dialog.user.username })}
        </ConfirmModal>
      )}

      {dialog.kind === 'toggle' && (
        <ToggleModal
          user={dialog.user}
          busy={toggling}
          onHide={close}
          onConfirm={() => {
            void confirmToggle(dialog.user)
          }}
        />
      )}
    </>
  )
}
