import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useRegistrationOpen } from '../hooks/useRegistrationOpen'
import { ApiError, MIN_PASSWORD_LENGTH, NetworkError, register } from '../services/auth'

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
  | { status: 'done'; username: string }

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
 * for an administrator, so there is no session to hand out and the next step is
 * to read the e-mail, not to browse.
 */
export function RegisterPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('register.title'))
  const registration = useRegistrationOpen()

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
      secret.trim() === ''
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
        secret: secret.trim(),
      })
      setSubmit({ status: 'done', username: account.username })
    } catch (error: unknown) {
      setSubmit({ status: 'error', rejection: rejectionFor(error) })
    }
  }

  const submitting = submit.status === 'submitting'

  return (
    <Row className="justify-content-center">
      <Col xs={12} sm={10} md={7} lg={6} xl={5}>
        <Card text="light" className="mt-4 mt-md-5">
          <Card.Body>
            <Card.Title as="h1" className="kk-page-title mb-4 text-center">
              {t('register.title')}
            </Card.Title>

            {registration === 'loading' && (
              <div className="text-center py-3">
                <Spinner animation="border" role="status">
                  <span className="visually-hidden">{t('register.loading')}</span>
                </Spinner>
              </div>
            )}

            {registration === 'closed' && (
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

            {registration !== 'loading' && registration !== 'closed' && (
              <>
                {submit.status === 'done' ? (
                  <div data-testid="register-done">
                    <Alert variant="success" role="status">
                      <Alert.Heading as="h2" className="h5">
                        <Icon name="person-check" className="me-2" />
                        {t('register.doneTitle', { username: submit.username })}
                      </Alert.Heading>
                      <p className="mb-0">{t('register.donePending')}</p>
                      <p className="mb-0">{t('register.doneMail')}</p>
                    </Alert>
                    <div className="text-center">
                      <Link to="/login">{t('register.backToLogin')}</Link>
                    </div>
                  </div>
                ) : (
                  <>
                    <p className="text-secondary">{t('register.intro')}</p>

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
                          rejection?.field === 'email'
                            ? rejectionMessage
                            : t('register.emailRequired')
                        }
                        hint={t('register.emailHint')}
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
          </Card.Body>
        </Card>
      </Col>
    </Row>
  )
}
