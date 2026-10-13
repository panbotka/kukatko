import { type SyntheticEvent, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { Trans, useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'

import { AuthFrame } from '../components/AuthFrame'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { mailEnabledFrom } from '../hooks/useMailEnabled'
import { usePublicSettings } from '../hooks/usePublicSettings'
import { registrationOpenFrom } from '../hooks/useRegistrationOpen'
import { ApiError, MIN_PASSWORD_LENGTH, NetworkError, register } from '../services/auth'
import { fetchPublicInvite, InviteApiError } from '../services/invites'

/** The inputs of the form, so a rejection can name the one that caused it. */
type FieldName = 'username' | 'displayName' | 'email' | 'password' | 'passwordRepeat' | 'secret'

/** The sentences a refused registration can produce. */
type RegisterErrorKey =
  | 'register.errorUsernameTaken'
  | 'register.errorUsernameTooLong'
  | 'register.errorEmailInvalid'
  | 'register.errorPasswordShort'
  | 'register.errorPasswordMismatch'
  | 'register.errorSecret'
  | 'register.errorClosed'
  | 'register.errorLink'
  | 'register.errorInvite'
  | 'register.errorRateLimited'
  | 'register.errorOffline'
  | 'register.errorGeneric'

/**
 * A refusal to register, and where it belongs on screen. A `field` puts the
 * sentence under that input — which is the only place it is actionable, since
 * every one of these is fixed by retyping one thing — while `null` means the
 * refusal is about the attempt as a whole and goes above the form.
 */
interface Rejection {
  field: FieldName | null
  messageKey: RegisterErrorKey
}

type SubmitState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'error'; rejection: Rejection }
  | { status: 'done'; username: string; pendingApproval: boolean }

/**
 * What the invitation in the address (`/register?invite=<code>`) turned out to
 * be: still being asked about, live (with the project it leads into), unusable,
 * or unknown because the question failed — then the form is shown anyway and
 * the server has the final word.
 */
type InviteLookup =
  | { status: 'none' }
  | { status: 'loading' }
  | { status: 'ready'; projectName: string }
  | { status: 'invalid' }
  | { status: 'unknown' }

/**
 * Maps a failed `POST /auth/register` onto the sentence to print and the field
 * to print it under.
 *
 * The backend answers a wrong shared secret with a 403 whose message names the
 * secret, and a closed instance with a 403 whose message does not — the two are
 * the same status on purpose (neither discloses anything about the secret), so
 * the message is what tells them apart. Getting that wrong is the failure this
 * function exists to prevent: "that is not the right word" must never reach the
 * reader as a server error, because the word is the one thing they can fix.
 *
 * A {@link NetworkError} comes first: a request that never left the device had
 * nothing judge it, so blaming the username or the secret would send somebody
 * off editing input that was fine.
 */
function rejectionFor(error: unknown): Rejection {
  if (error instanceof NetworkError) {
    return { field: null, messageKey: 'register.errorOffline' }
  }
  if (error instanceof ApiError) {
    if (error.status === 409) {
      return { field: 'username', messageKey: 'register.errorUsernameTaken' }
    }
    if (error.status === 403) {
      if (error.message.includes('upload link')) {
        return { field: null, messageKey: 'register.errorLink' }
      }
      if (error.message.includes('invitation')) {
        return { field: null, messageKey: 'register.errorInvite' }
      }
      return error.message.includes('secret')
        ? { field: 'secret', messageKey: 'register.errorSecret' }
        : { field: null, messageKey: 'register.errorClosed' }
    }
    if (error.status === 429) {
      return { field: null, messageKey: 'register.errorRateLimited' }
    }
    if (error.status === 400) {
      // The account store words its refusals as "auth: <field> must be …", so
      // the field it will not take is in the message and nowhere else.
      if (error.message.includes('email')) {
        return { field: 'email', messageKey: 'register.errorEmailInvalid' }
      }
      if (error.message.includes('password')) {
        return { field: 'password', messageKey: 'register.errorPasswordShort' }
      }
      if (error.message.includes('username')) {
        return { field: 'username', messageKey: 'register.errorUsernameTooLong' }
      }
    }
  }
  return { field: null, messageKey: 'register.errorGeneric' }
}

/**
 * Asks the backend about the invitation `code` (`GET /invite/<code>`), for the
 * project name the form greets with. An empty code is no invitation at all.
 */
function useInviteLookup(code: string): InviteLookup {
  // The answer is kept with the code it belongs to, so a changed code reads as
  // "loading" until its own answer arrives — without resetting state in the effect.
  const [answer, setAnswer] = useState<{ code: string; lookup: InviteLookup } | null>(null)
  useEffect(() => {
    if (code === '') {
      return
    }
    const controller = new AbortController()
    fetchPublicInvite(code, controller.signal)
      .then((invite) => {
        setAnswer({ code, lookup: { status: 'ready', projectName: invite.project_name } })
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        const lookup: InviteLookup =
          error instanceof InviteApiError && error.status === 404
            ? { status: 'invalid' }
            : { status: 'unknown' }
        setAnswer({ code, lookup })
      })
    return () => {
      controller.abort()
    }
  }, [code])
  if (code === '') {
    return { status: 'none' }
  }
  return answer?.code === code ? answer.lookup : { status: 'loading' }
}

/** One labelled input of the registration form, with its own error slot. */
interface FieldProps {
  /** The `controlId` of the group, which becomes the input's `id`. */
  id: string
  label: string
  type: 'text' | 'email' | 'password'
  autoComplete: string
  value: string
  onChange: (value: string) => void
  disabled: boolean
  /**
   * The sentence shown when the field is invalid: the server's rejection when
   * there is one for this field, otherwise the "fill this in" prompt browser
   * validation triggers.
   */
  feedback: string
  /** True when the server (or the password check) refused *this* field. */
  invalid: boolean
  /** An explanatory line under the input, for a field that needs one. */
  hint?: string
  autoFocus?: boolean
  minLength?: number
}

/**
 * Renders one form row. The six inputs differ only in their label, type and
 * error slot, so they share this instead of six near-identical blocks — and the
 * shared shape is what keeps every rejection anchored to its own field.
 */
function RegisterField({
  id,
  label,
  type,
  autoComplete,
  value,
  onChange,
  disabled,
  feedback,
  invalid,
  hint,
  autoFocus,
  minLength,
}: FieldProps) {
  const hintId = `${id}-help`
  return (
    <Form.Group className="mb-3" controlId={id}>
      <Form.Label>{label}</Form.Label>
      <Form.Control
        type={type}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        required
        minLength={minLength}
        isInvalid={invalid}
        // react-bootstrap's `isInvalid` only paints the input red; a screen
        // reader learns nothing from a colour, so the state is stated too.
        aria-invalid={invalid ? true : undefined}
        aria-describedby={hint === undefined ? undefined : hintId}
        value={value}
        onChange={(event) => {
          onChange(event.target.value)
        }}
        disabled={disabled}
      />
      {hint !== undefined && (
        <Form.Text id={hintId} className="text-secondary d-block">
          {hint}
        </Form.Text>
      )}
      <Form.Control.Feedback type="invalid">{feedback}</Form.Control.Feedback>
    </Form.Group>
  )
}

/**
 * Registration page: the self-service way into the library for somebody the
 * community told about it, laid out like the sign-in card next to it so the two
 * read as one flow.
 *
 * The instance decides whether this screen has a form at all. While
 * `GET /settings/public` is being asked it shows a spinner, and a closed
 * instance gets a plain explanation and the way back to sign-in — a form that
 * cannot succeed is worse than no form, since every field would be typed only
 * to be refused. When the question could not be asked at all the form is shown
 * anyway: the server has the final word, and it words a closed door better than
 * a failed probe could.
 *
 * A successful registration replaces the form with the confirmation, and
 * deliberately does **not** sign anybody in: the account exists but is waiting
 * for an administrator, so there is no session to hand out and there is nothing
 * to browse yet.
 *
 * Arriving through an invitation (`/register?invite=<code>`, from the
 * `/invite/:code` page) the form asks `GET /invite/<code>` for the project's
 * name, greets with it, leaves out the registration word and sends the code as
 * `invite_code`. The instance's registration switch does not apply — an invite
 * is a project admin's decision about one person — and the account is created
 * approved, so the confirmation says it may sign in right away. An unusable
 * invite shows one plain sentence instead of the form.
 *
 * The same public response says whether this instance sends mail, and both the
 * e-mail field's hint and the confirmation say only what will actually happen:
 * an instance with `mail.enabled` off wires the no-op sender, so a promised
 * confirmation would never arrive.
 */
export function RegisterPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('register.title'))
  // One fetch answers both questions this screen asks of the instance: whether
  // there is any point showing the form, and whether it may promise an e-mail.
  const publicSettings = usePublicSettings()
  const registration = registrationOpenFrom(publicSettings)
  const mailEnabled = mailEnabledFrom(publicSettings)
  // Arriving from an upload link (/register?link=<code>): the link stands in for
  // the shared secret, so the secret field is not asked for at all.
  const [searchParams] = useSearchParams()
  const uploadLink = searchParams.get('link')?.trim() ?? ''
  // Arriving from an invitation (/register?invite=<code>): the invite stands in
  // for the secret too, wins over a link, and works while registration is closed
  // — it is a project admin's decision about one person. The account it makes
  // is approved at once.
  const inviteCode = searchParams.get('invite')?.trim() ?? ''
  const viaInvite = inviteCode !== ''
  const viaLink = !viaInvite && uploadLink !== ''
  const invite = useInviteLookup(inviteCode)

  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [passwordRepeat, setPasswordRepeat] = useState('')
  const [secret, setSecret] = useState('')
  const [validated, setValidated] = useState(false)
  const [submit, setSubmit] = useState<SubmitState>({ status: 'idle' })

  const rejection = submit.status === 'error' ? submit.rejection : null
  // Every rejection is rendered through here so the one sentence that names a
  // number — the password minimum — reads the same constant the input enforces.
  const rejectionMessage =
    rejection === null ? '' : t(rejection.messageKey, { min: MIN_PASSWORD_LENGTH })

  /**
   * Wraps a field's setter so that typing clears a standing rejection. Leaving
   * „that username is taken" under a name the reader has since changed would
   * make the form argue with input it has not seen.
   */
  function change(setter: (value: string) => void) {
    return (value: string) => {
      setter(value)
      setSubmit((current) => (current.status === 'error' ? { status: 'idle' } : current))
    }
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (
      username.trim() === '' ||
      displayName.trim() === '' ||
      email.trim() === '' ||
      password === '' ||
      passwordRepeat === '' ||
      (!viaLink && !viaInvite && secret.trim() === '')
    ) {
      setValidated(true)
      return
    }
    // Checked here rather than by the server, which never sees the second
    // password: a typo in it is the one rejection this form can word itself.
    if (password !== passwordRepeat) {
      setSubmit({
        status: 'error',
        rejection: { field: 'passwordRepeat', messageKey: 'register.errorPasswordMismatch' },
      })
      return
    }
    setSubmit({ status: 'submitting' })
    try {
      const account = await register({
        username: username.trim(),
        display_name: displayName.trim(),
        email: email.trim(),
        password,
        secret: viaLink || viaInvite ? '' : secret.trim(),
        ...(viaLink ? { upload_link: uploadLink } : {}),
        ...(viaInvite ? { invite_code: inviteCode } : {}),
      })
      setSubmit({
        status: 'done',
        username: account.username,
        pendingApproval: account.pending_approval,
      })
    } catch (error: unknown) {
      setSubmit({ status: 'error', rejection: rejectionFor(error) })
    }
  }

  const submitting = submit.status === 'submitting'
  // An invitation decides on its own whether the form is shown: the instance's
  // registration switch does not apply to it.
  const gate = viaInvite
    ? invite.status === 'loading'
      ? 'loading'
      : invite.status === 'invalid'
        ? 'invalid'
        : 'open'
    : registration

  return (
    <AuthFrame title={t('register.title')}>
      {gate === 'loading' && (
        <div className="text-center py-3">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('register.loading')}</span>
          </Spinner>
        </div>
      )}

      {gate === 'invalid' && (
        <div data-testid="register-invite-invalid">
          <Alert variant="secondary" role="status">
            <Icon name="slash-circle" className="me-2" />
            {t('register.errorInvite')}
          </Alert>
          <div className="text-center">
            <Link to="/login">{t('register.backToLogin')}</Link>
          </div>
        </div>
      )}

      {gate === 'closed' && (
        <div data-testid="register-closed">
          <Alert variant="secondary" role="status">
            <Icon name="slash-circle" className="me-2" />
            {t('register.closed')}
          </Alert>
          <div className="text-center">
            <Link to="/login">{t('register.backToLogin')}</Link>
          </div>
        </div>
      )}

      {gate !== 'loading' && gate !== 'closed' && gate !== 'invalid' && (
        <>
          {submit.status === 'done' ? (
            <div data-testid="register-done">
              <Alert variant="success" role="status">
                <Alert.Heading as="h2" className="h5">
                  <Icon name="person-check" className="me-2" />
                  {t('register.doneTitle', { username: submit.username })}
                </Alert.Heading>
                {submit.pendingApproval ? (
                  <>
                    <p className="mb-0">{t('register.donePending')}</p>
                    <p className="mb-0">
                      {mailEnabled ? t('register.doneMail') : t('register.doneNoMail')}
                    </p>
                  </>
                ) : (
                  <p className="mb-0">{t('register.doneApproved')}</p>
                )}
              </Alert>
              <div className="text-center">
                <Link to="/login">{t('register.backToLogin')}</Link>
              </div>
            </div>
          ) : (
            <>
              <p className="text-secondary" data-testid="register-intro">
                {viaInvite ? (
                  invite.status === 'ready' ? (
                    <Trans
                      i18nKey="register.introInvite"
                      values={{ project: invite.projectName }}
                      components={{ strong: <strong /> }}
                    />
                  ) : (
                    t('register.introInviteNoName')
                  )
                ) : viaLink ? (
                  t('register.introLink')
                ) : (
                  t('register.intro')
                )}
              </p>

              {rejection !== null && rejection.field === null && (
                <Alert variant="danger" role="alert">
                  {rejectionMessage}
                </Alert>
              )}

              <Form
                noValidate
                validated={validated}
                onSubmit={(event) => {
                  void handleSubmit(event)
                }}
              >
                <RegisterField
                  id="register-username"
                  label={t('register.username')}
                  type="text"
                  autoComplete="username"
                  value={username}
                  onChange={change(setUsername)}
                  disabled={submitting}
                  invalid={rejection?.field === 'username'}
                  feedback={
                    rejection?.field === 'username'
                      ? rejectionMessage
                      : t('register.usernameRequired')
                  }
                  hint={t('register.usernameHint')}
                  autoFocus
                />
                <RegisterField
                  id="register-display-name"
                  label={t('register.displayName')}
                  type="text"
                  autoComplete="name"
                  value={displayName}
                  onChange={change(setDisplayName)}
                  disabled={submitting}
                  invalid={rejection?.field === 'displayName'}
                  feedback={
                    rejection?.field === 'displayName'
                      ? rejectionMessage
                      : t('register.displayNameRequired')
                  }
                />
                <RegisterField
                  id="register-email"
                  label={t('register.email')}
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={change(setEmail)}
                  disabled={submitting}
                  invalid={rejection?.field === 'email'}
                  feedback={
                    rejection?.field === 'email' ? rejectionMessage : t('register.emailRequired')
                  }
                  hint={t(mailEnabled ? 'register.emailHint' : 'register.emailHintNoMail')}
                />
                <RegisterField
                  id="register-password"
                  label={t('register.password')}
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={change(setPassword)}
                  disabled={submitting}
                  minLength={MIN_PASSWORD_LENGTH}
                  invalid={rejection?.field === 'password'}
                  feedback={
                    rejection?.field === 'password'
                      ? rejectionMessage
                      : t('register.passwordRequired', { min: MIN_PASSWORD_LENGTH })
                  }
                />
                <RegisterField
                  id="register-password-repeat"
                  label={t('register.passwordRepeat')}
                  type="password"
                  autoComplete="new-password"
                  value={passwordRepeat}
                  onChange={change(setPasswordRepeat)}
                  disabled={submitting}
                  invalid={rejection?.field === 'passwordRepeat'}
                  feedback={
                    rejection?.field === 'passwordRepeat'
                      ? rejectionMessage
                      : t('register.passwordRepeatRequired')
                  }
                />
                {!viaLink && !viaInvite && (
                  <RegisterField
                    id="register-secret"
                    label={t('register.secret')}
                    type="text"
                    autoComplete="off"
                    value={secret}
                    onChange={change(setSecret)}
                    disabled={submitting}
                    invalid={rejection?.field === 'secret'}
                    feedback={
                      rejection?.field === 'secret'
                        ? rejectionMessage
                        : t('register.secretRequired')
                    }
                    hint={t('register.secretHint')}
                  />
                )}

                <div className="d-grid mt-4">
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
                    {t('register.submit')}
                  </Button>
                </div>
              </Form>

              <div className="text-center mt-3">
                <Link to="/login">{t('register.backToLogin')}</Link>
              </div>
            </>
          )}
        </>
      )}
    </AuthFrame>
  )
}
