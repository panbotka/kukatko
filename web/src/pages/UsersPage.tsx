import { type SyntheticEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Placeholder from 'react-bootstrap/Placeholder'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import Modal from '../components/Modal'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { AddAutocomplete } from '../components/photo/AddAutocomplete'
import { PendingFilter } from '../components/users/PendingFilter'
import { ResetLinkModal } from '../components/users/ResetLinkModal'
import { UserActions, type UserActionsProps } from '../components/users/UserActions'
import { UserCard } from '../components/users/UserCard'
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
import { useToast } from '../components/toast/ToastContext'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { useMailEnabled } from '../hooks/useMailEnabled'
import { useSubjects } from '../hooks/useSubjects'
import { formatDate, formatDateTime } from '../lib/format'
import { MIN_PASSWORD_LENGTH, type Role } from '../services/auth'
import {
  approveUser,
  createUser,
  fetchUsers,
  MAX_NOTE_LENGTH,
  MAX_USERNAME_LENGTH,
  renameUser,
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
 * Names one account's linked person for the roster: the person's name, null
 * when there is no link (the table prints an em dash, a card leaves the line
 * out), and the bare UID in the moment between the roster
 * arriving and the people list arriving (or if that list failed) — a raw id is
 * ugly, but claiming "nobody" would be wrong.
 *
 * A link whose subject has been deleted cannot happen: the database clears it.
 * It is a plain function of the already-loaded name map rather than a component
 * with a hook, so a roster of thirty accounts still costs exactly one request
 * for the people.
 */
function linkedPersonName(
  subjectUid: string | null | undefined,
  names: Map<string, string>,
): string | null {
  if (subjectUid === null || subjectUid === undefined || subjectUid === '') {
    return null
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
  /**
   * Whether the actor may manage the edited account at all — false only for an
   * admin facing a maintainer's account (the backend's maintainer boundary).
   * Where it is false the username stays read-only; it is ignored on create.
   */
  canManage: boolean
  onHide: () => void
  /** Called with the final row once everything the dialog sent was saved. */
  onSaved: (user: AdminUser) => void
  /**
   * Called with the renamed row when a rename succeeded but the profile update
   * after it did not, so the roster shows the name the account really has while
   * the dialog stays open on the error.
   */
  onRenamed: (user: AdminUser) => void
}

/**
 * The create/edit dialog. Creating asks for a username and password on top of
 * the shared profile fields. Editing offers the username too, for an account the
 * actor may manage: a changed name goes to its own endpoint
 * (`PUT /admin/users/{uid}/username`) before the profile update, so a taken name
 * stops the save with the message under the field and nothing else changes.
 * Where the actor may not manage the account the name is shown read-only, with
 * the reason under it.
 *
 * Validation errors from the API land next to the input that caused them rather
 * than in a banner, so the reader does not have to guess which field to fix.
 *
 * On a phone the dialog is a full-screen sheet and only its body scrolls, so the
 * six fields get the whole small screen and the Save/Cancel pair stays pinned
 * above the on-screen keyboard rather than under it; on a wider screen it is the
 * same centred card as before. Its wrapping form carries {@link MODAL_FORM_CLASS}.
 */
export function UserFormModal({
  user,
  isMaintainer,
  canManage,
  onHide,
  onSaved,
  onRenamed,
}: UserFormModalProps) {
  const { t } = useTranslation()
  const creating = user === null
  const usernameEditable = creating || canManage

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
  // The name the account holds right now. It starts as the row's and moves when
  // a rename went through but the profile update after it failed, so a second
  // Save does not send the rename again.
  const [storedUsername, setStoredUsername] = useState(user?.username ?? '')

  const usernameMissing = username.trim() === ''
  const passwordTooShort = password.length < MIN_PASSWORD_LENGTH
  // Every account receives mail — approval, password reset — so the backend
  // requires an address on create *and* on update; an edit may not clear it.
  const emailMissing = email.trim() === ''

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (emailMissing || (usernameEditable && usernameMissing) || (creating && passwordTooShort)) {
      setValidated(true)
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      // The backend lower-cases and trims the name; comparing in that form
      // keeps "Jan " from being sent as a rename of "jan". The untouched-field
      // check comes first so a legacy mixed-case name nobody edited is never
      // quietly lower-cased by an unrelated save.
      const renaming =
        username !== storedUsername && username.trim().toLowerCase() !== storedUsername
      if (!creating && usernameEditable && renaming) {
        const renamed = await renameUser(user.uid, username)
        setStoredUsername(renamed.username)
        onRenamed(renamed)
      }
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
              maxLength={MAX_USERNAME_LENGTH}
              readOnly={!usernameEditable}
              plaintext={!usernameEditable}
              isInvalid={error?.field === 'username' || (validated && usernameMissing)}
              value={username}
              onChange={(event) => {
                setUsername(event.target.value)
              }}
              disabled={submitting}
            />
            {!creating && (
              <Form.Text className="text-secondary">
                {usernameEditable
                  ? t('users.form.usernameRenameHint')
                  : t('users.form.usernameLocked')}
              </Form.Text>
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
  // Whether an approval is followed by an e-mail at all. On an instance with
  // mail off nothing is sent, and the approval's confirmation says so rather than
  // telling an administrator the account will hear from Kukátko by itself.
  const mailEnabled = useMailEnabled()
  const toast = useToast()
  // A phone gets the approval-shaped cards instead of the ten-column table.
  const narrow = useIsNarrowViewport()
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
  // The accounts whose approval is in flight, and why the last approval of an
  // account was refused. Per account rather than one flag for the page, because
  // approving is one tap and nothing stops the next card being tapped while the
  // first request still runs. The ref is the synchronous guard against a double
  // tap landing before React has re-rendered the button as disabled.
  const [approving, setApproving] = useState<ReadonlySet<string>>(() => new Set())
  const approvingRef = useRef(new Set<string>())
  const [approveErrors, setApproveErrors] = useState<Readonly<Record<string, ErrorKey>>>({})
  // Narrows the roster to the accounts waiting to be let in — on by default,
  // because letting newly registered people in is what an administrator usually
  // opens this page for. It lives in the URL ("back always works"): no parameter
  // is the waiting list, `?all=1` is everybody, and each switch is a history
  // entry. It is view state of the already-loaded list, not a query: switching
  // it re-renders and never re-fetches, so the filter cannot fail.
  const [searchParams, setSearchParams] = useSearchParams()
  const pendingOnly = searchParams.get('all') !== '1'
  const setPendingOnly = useCallback(
    (next: boolean) => {
      setSearchParams((prev) => {
        const params = new URLSearchParams(prev)
        if (next) {
          params.delete('all')
        } else {
          params.set('all', '1')
        }
        return params
      })
    },
    [setSearchParams],
  )
  // The enable/disable action has no form to hang a field error on, so it keeps
  // just the message key. It is a real message rather than a boolean because the
  // backend refuses disabling the last maintainer, and "the action could not be
  // completed" would leave the reader with no idea what to do about it.
  const [actionError, setActionError] = useState<ErrorKey | null>(null)
  const [notice, setNotice] = useState<'passwordChanged' | null>(null)

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
      const previous = prev.users.find((u) => u.uid === saved.uid)
      const merged =
        previous === undefined
          ? [...prev.users, saved]
          : prev.users.map((u) => (u.uid === saved.uid ? saved : u))
      // Re-sorted only when the order can have changed — a new row or a renamed
      // one — so an ordinary edit never reshuffles the roster under the reader.
      const users =
        previous?.username === saved.username
          ? merged
          : merged.sort((a, b) => a.username.localeCompare(b.username))
      return { status: 'ready', users }
    })
  }, [])

  /** Marks `uid`'s approval as running (or no longer running). */
  const markApproving = useCallback((uid: string, running: boolean) => {
    if (running) {
      approvingRef.current.add(uid)
    } else {
      approvingRef.current.delete(uid)
    }
    setApproving(new Set(approvingRef.current))
  }, [])

  /**
   * Lets a waiting account in on one press, replacing its row with the approved
   * one — which, under the default waiting-only filter, takes it off the list.
   *
   * There is no confirmation step, on a phone or on the table: approving is the
   * errand this page exists for, and a wrong approval is cheap to undo by
   * blocking the account. What the dialog used to say — whether the person will
   * get an e-mail — moves into the success toast.
   */
  async function approve(user: AdminUser) {
    if (approvingRef.current.has(user.uid)) {
      return
    }
    markApproving(user.uid, true)
    setApproveErrors(({ [user.uid]: _dropped, ...rest }) => rest)
    try {
      upsert(await approveUser(user.uid))
      toast.show({
        variant: 'success',
        message: t(mailEnabled ? 'users.approve.success' : 'users.approve.successNoMail', {
          username: user.display_name || user.username,
        }),
      })
    } catch (err) {
      // The roster is untouched: the account still reads "waiting", so the same
      // button is still there once the reader has dealt with the reason printed
      // under it.
      setApproveErrors((prev) => ({ ...prev, [user.uid]: actionErrorFor(err) }))
    } finally {
      markApproving(user.uid, false)
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

  /** The action props for one account — the same on a row and on a card. */
  function actionsFor(user: AdminUser): UserActionsProps {
    return {
      user,
      self: user.uid === me?.uid,
      canManage: isMaintainer || user.role !== 'maintainer',
      approving: approving.has(user.uid),
      approveError: approveErrors[user.uid] ?? null,
      onApprove: () => {
        void approve(user)
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
      cell: (user) => linkedPersonName(user.subject_uid, subjectNames) ?? '—',
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
      cell: (user) => <UserActions {...actionsFor(user)} />,
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
          {t('users.password.success')}
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
                // Only the waiting-only filter can leave nothing (the roster
                // itself is not empty here). It is the default view, so it must
                // read as "all done", not as a broken page — with the way to
                // everybody else one press away.
                <EmptyState
                  title={t('users.empty.noPendingTitle')}
                  hint={t('users.empty.noPendingHint')}
                  action={
                    <Button
                      variant="outline-primary"
                      onClick={() => {
                        setPendingOnly(false)
                      }}
                    >
                      {t('users.empty.showAll')}
                    </Button>
                  }
                />
              ) : narrow ? (
                <ul className="list-unstyled d-grid gap-3 mb-0">
                  {visible.map((user) => (
                    <li key={user.uid}>
                      <UserCard
                        {...actionsFor(user)}
                        person={linkedPersonName(user.subject_uid, subjectNames)}
                      />
                    </li>
                  ))}
                </ul>
              ) : (
                <RecordTable
                  records={visible}
                  columns={columns}
                  rowKey={(user) => user.uid}
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
          canManage={dialog.kind !== 'edit' || isMaintainer || dialog.user.role !== 'maintainer'}
          onHide={close}
          onSaved={(saved) => {
            upsert(saved)
            close()
          }}
          onRenamed={upsert}
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

      {dialog.kind === 'resetLink' && (
        <ResetLinkModal user={dialog.user} mailEnabled={mailEnabled} onHide={close} />
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
