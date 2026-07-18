import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import Placeholder from 'react-bootstrap/Placeholder'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { formatDate, formatDateTime } from '../lib/format'
import { ApiError, MIN_PASSWORD_LENGTH, type Role } from '../services/auth'
import {
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
  | { kind: 'toggle'; user: AdminUser }

/** The form fields an API validation error can be attributed to. */
type FormField = 'username' | 'password' | 'role' | 'note'

/** The i18n keys for the validation messages the backend can produce. */
type ErrorKey =
  | 'users.errors.usernameTaken'
  | 'users.errors.passwordTooShort'
  | 'users.errors.invalidRole'
  | 'users.errors.noteTooLong'
  | 'users.errors.generic'

/**
 * A failed submission: `field` names the input to flag inline, or is null when
 * the failure belongs to no single field and has to surface as a form-level alert.
 */
interface FormError {
  field: FormField | null
  messageKey: ErrorKey
}

/**
 * Attributes a failed create/update to the field the backend rejected.
 *
 * The admin user handlers answer with a plain `{"error": "..."}` envelope rather
 * than a per-field structure, so the status plus a keyword from the message is
 * all there is to go on: 409 is only ever a duplicate username, and the three
 * possible 400s each name their own field (`internal/auth/handlers_admin.go`).
 * Anything unrecognised degrades to a form-level message.
 */
function fieldErrorFor(error: unknown): FormError {
  if (error instanceof ApiError) {
    if (error.status === 409) {
      return { field: 'username', messageKey: 'users.errors.usernameTaken' }
    }
    if (error.status === 400) {
      const message = error.message.toLowerCase()
      if (message.includes('password')) {
        return { field: 'password', messageKey: 'users.errors.passwordTooShort' }
      }
      if (message.includes('role')) {
        return { field: 'role', messageKey: 'users.errors.invalidRole' }
      }
      if (message.includes('note')) {
        return { field: 'note', messageKey: 'users.errors.noteTooLong' }
      }
    }
  }
  return { field: null, messageKey: 'users.errors.generic' }
}

/** The skeleton's three placeholder rows. */
const SKELETON_ROWS = ['a', 'b', 'c']

/** One placeholder bar per table column, roughly as wide as the real content. */
const SKELETON_CELLS: { column: string; width: number }[] = [
  { column: 'username', width: 9 },
  { column: 'displayName', width: 7 },
  { column: 'role', width: 4 },
  { column: 'state', width: 4 },
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
  const [role, setRole] = useState<Role>(user?.role ?? 'viewer')
  const [note, setNote] = useState(user?.note ?? '')
  const [validated, setValidated] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<FormError | null>(null)

  const usernameMissing = username.trim() === ''
  const passwordTooShort = password.length < MIN_PASSWORD_LENGTH

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (creating && (usernameMissing || passwordTooShort)) {
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
            email: '',
            role,
            note,
          })
        : await updateUser(user.uid, {
            display_name: displayName,
            // The update replaces the whole profile, so the fields this dialog
            // does not offer are echoed back unchanged.
            email: user.email,
            role,
            disabled: user.disabled,
            note,
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
    <Modal show onHide={onHide} centered>
      <Form
        noValidate
        validated={validated}
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
    <Modal show onHide={onHide} centered>
      <Form
        noValidate
        validated={validated}
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
 */
function ToggleModal({ user, onHide, onConfirm, busy }: ToggleModalProps) {
  const { t } = useTranslation()
  const enabling = user.disabled
  return (
    <Modal show onHide={onHide} centered>
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

/** Props for one row of the user table. */
interface UserRowProps {
  user: AdminUser
  /** True when the row is the signed-in administrator's own account. */
  self: boolean
  /**
   * True when the signed-in actor may manage this account. A non-maintainer
   * cannot touch a maintainer's account (edit, reset password or disable), so its
   * three actions are disabled — mirroring the backend `guardMaintainerBoundary`.
   */
  canManage: boolean
  locale: string
  onEdit: () => void
  onPassword: () => void
  onToggle: () => void
}

/** One user: profile columns, then the three per-row actions. */
function UserRow({ user, self, canManage, locale, onEdit, onPassword, onToggle }: UserRowProps) {
  const { t } = useTranslation()
  return (
    <tr>
      <td className="fw-semibold text-break">{user.username}</td>
      <td className="text-break">{user.display_name || '—'}</td>
      <td>
        <Badge bg="secondary">{t(`roles.${user.role}`)}</Badge>
      </td>
      <td>
        <Badge bg={user.disabled ? 'danger' : 'success'}>
          {user.disabled ? t('users.state.disabled') : t('users.state.enabled')}
        </Badge>
      </td>
      <td className="text-secondary small text-break" style={{ maxWidth: '18rem' }}>
        {user.note || '—'}
      </td>
      <td className="text-nowrap">
        {user.last_login_at === undefined
          ? t('users.never')
          : formatDateTime(user.last_login_at, locale)}
      </td>
      <td className="text-nowrap">{formatDate(user.created_at, locale)}</td>
      <td>
        <div className="d-flex gap-1 flex-wrap">
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={!canManage}
            title={canManage ? undefined : t('users.maintainerManageHint')}
            onClick={onEdit}
          >
            {t('users.edit')}
          </Button>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={!canManage}
            title={canManage ? undefined : t('users.maintainerManageHint')}
            onClick={onPassword}
          >
            {t('users.changePassword')}
          </Button>
          <Button
            variant={user.disabled ? 'outline-success' : 'outline-danger'}
            size="sm"
            disabled={self || !canManage}
            title={
              self
                ? t('users.selfDisableHint')
                : canManage
                  ? undefined
                  : t('users.maintainerManageHint')
            }
            onClick={onToggle}
          >
            {user.disabled ? t('users.enable') : t('users.disable')}
          </Button>
        </div>
        {self && <div className="text-secondary small mt-1">{t('users.selfDisableHint')}</div>}
        {!self && !canManage && (
          <div className="text-secondary small mt-1">{t('users.maintainerManageHint')}</div>
        )}
      </td>
    </tr>
  )
}

/**
 * Admin-only user administration: the roster of local accounts with the four
 * things an administrator does to them — create one, edit its role/name/note,
 * reset its password, and retire it by disabling.
 *
 * Accounts are never deleted. Photos, albums, ratings and audit entries all
 * point at a user; deleting one would either orphan that history or erase it, so
 * disabling is the supported way to retire an account. An administrator cannot
 * disable their own account either, which would lock the instance's last admin
 * out of it.
 */
export function UsersPage() {
  const { t, i18n } = useTranslation()
  const { isAdmin, isMaintainer, user: me } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [dialog, setDialog] = useState<Dialog>({ kind: 'none' })
  const [toggling, setToggling] = useState(false)
  const [actionError, setActionError] = useState(false)
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
      const known = prev.users.some((u) => u.uid === saved.uid)
      const users = known
        ? prev.users.map((u) => (u.uid === saved.uid ? saved : u))
        : [...prev.users, saved].sort((a, b) => a.username.localeCompare(b.username))
      return { status: 'ready', users }
    })
  }, [])

  async function confirmToggle(user: AdminUser) {
    setActionError(false)
    setToggling(true)
    try {
      upsert(await setUserDisabled(user, !user.disabled))
    } catch {
      setActionError(true)
    } finally {
      setToggling(false)
      // Either way the dialog closes: a modal backdrop would hide the error alert.
      close()
    }
  }

  if (!isAdmin) {
    return <Alert variant="danger">{t('users.adminOnly')}</Alert>
  }

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

      {actionError && (
        <Alert
          variant="danger"
          role="alert"
          dismissible
          onClose={() => {
            setActionError(false)
          }}
        >
          {t('users.errors.generic')}
        </Alert>
      )}
      {notice === 'passwordChanged' && (
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
            <Table striped hover responsive className="mb-0 align-middle">
              <thead>
                <tr>
                  <th>{t('users.columns.username')}</th>
                  <th>{t('users.columns.displayName')}</th>
                  <th>{t('users.columns.role')}</th>
                  <th>{t('users.columns.state')}</th>
                  <th>{t('users.columns.note')}</th>
                  <th>{t('users.columns.lastLogin')}</th>
                  <th>{t('users.columns.created')}</th>
                  <th>{t('users.columns.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {state.users.map((user) => (
                  <UserRow
                    key={user.uid}
                    user={user}
                    self={user.uid === me?.uid}
                    canManage={isMaintainer || user.role !== 'maintainer'}
                    locale={i18n.language}
                    onEdit={() => {
                      setDialog({ kind: 'edit', user })
                    }}
                    onPassword={() => {
                      setDialog({ kind: 'password', user })
                    }}
                    onToggle={() => {
                      setDialog({ kind: 'toggle', user })
                    }}
                  />
                ))}
              </tbody>
            </Table>
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
