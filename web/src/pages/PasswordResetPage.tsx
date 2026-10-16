import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { AuthFrame } from '../components/AuthFrame'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import {
  ApiError,
  fetchPasswordResetStatus,
  MIN_PASSWORD_LENGTH,
  NetworkError,
  resetPassword,
} from '../services/auth'
import { slugProblem } from '../services/projects'

/**
 * What is known about the link this page was opened with.
 *
 * `unusable` is the one answer to an unknown, spent, expired or blocked link —
 * the backend refuses to tell those four apart, and neither does this screen, so
 * nothing here can be read as "there is (not) an account with that address".
 * `unreachable` is deliberately **not** the same state: the question could not be
 * asked at all, and telling somebody their link is dead because the server was
 * down would send them to ask for a replacement they do not need.
 */
type LinkState =
  | { status: 'checking' }
  | { status: 'usable'; displayName: string; firstPassword: boolean }
  | { status: 'unusable' }
  | { status: 'unreachable' }

/** The inputs of the form, so a rejection can name the one that caused it. */
type FieldName = 'password' | 'passwordRepeat'

/** The sentences a refused password change can produce. */
type ResetErrorKey =
  | 'passwordReset.errorPasswordShort'
  | 'passwordReset.errorPasswordMismatch'
  | 'passwordReset.errorRateLimited'
  | 'passwordReset.errorOffline'
  | 'passwordReset.errorGeneric'

/**
 * A refused attempt and where the sentence belongs: under one input when
 * retyping that input is what fixes it, above the form when the refusal is about
 * the attempt as a whole.
 */
interface Rejection {
  field: FieldName | null
  messageKey: ResetErrorKey
}

type SubmitState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'error'; rejection: Rejection }
  | { status: 'done' }

/**
 * Maps a failed `POST /auth/password-reset/{token}` onto the sentence to print
 * and the field to print it under.
 *
 * A **404 is absent on purpose**: it means the link died between opening this
 * page and submitting it, which is not a rejection of anything typed here — the
 * caller turns it back into the expired-link explanation instead, so the reader
 * gets the one honest answer rather than a form insisting on a better password.
 */
function rejectionFor(error: unknown): Rejection {
  if (error instanceof NetworkError) {
    return { field: null, messageKey: 'passwordReset.errorOffline' }
  }
  if (error instanceof ApiError) {
    if (error.status === 400) {
      return { field: 'password', messageKey: 'passwordReset.errorPasswordShort' }
    }
    if (error.status === 429) {
      return { field: null, messageKey: 'passwordReset.errorRateLimited' }
    }
  }
  return { field: null, messageKey: 'passwordReset.errorGeneric' }
}

/**
 * The sign-in address the "done" screen sends its reader to: the plain sign-in,
 * or — when the link named a project (`?project=<slug>`, which a project set up
 * for somebody carries) — the sign-in that lands in that project. A `project`
 * that is no slug is ignored rather than trusted.
 */
function signInPath(project: string | null): string {
  if (project === null || slugProblem(project) !== null) {
    return '/login'
  }
  return `/login?returnTo=${encodeURIComponent(`/p/${project}`)}`
}

/**
 * Password-reset landing page (route `/password-reset/:token`, public — the
 * reader is by definition locked out), laid out as the same one-column Superhero
 * card as sign-in and registration so the three read as one flow.
 *
 * The same page sets the **first** password of an account set up for somebody
 * (a project's first admin): the link's status says so (`first_password`), and
 * the wording follows — „Nastavte si heslo", not „Nové heslo" — with no word
 * about other sessions, which such an account never had. When the link carries
 * `?project=<slug>` the way to sign in afterwards lands in that project.
 *
 * The link is checked **before** the form exists: a dead link gets a plain
 * explanation and the way to sign-in, never a form that every submit would
 * refuse. A successful change does **not** sign anybody in — the call ends every
 * session of that account, including one signed in at that moment, so the honest
 * next step is to sign in with the new password.
 */
export function PasswordResetPage() {
  const { t } = useTranslation()
  const { token } = useParams<{ token: string }>()
  const [searchParams] = useSearchParams()
  const loginPath = signInPath(searchParams.get('project'))

  const [link, setLink] = useState<LinkState>({ status: 'checking' })
  const first = link.status === 'usable' && link.firstPassword
  const title = first ? t('passwordReset.firstTitle') : t('passwordReset.title')
  useDocumentTitle(title)
  // Bumped by the retry button; re-running the check is the whole effect, so the
  // attempt counter is the dependency that re-runs it.
  const [attempt, setAttempt] = useState(0)
  const [password, setPassword] = useState('')
  const [passwordRepeat, setPasswordRepeat] = useState('')
  const [validated, setValidated] = useState(false)
  const [submit, setSubmit] = useState<SubmitState>({ status: 'idle' })

  useEffect(() => {
    if (token === undefined || token === '') {
      setLink({ status: 'unusable' })
      return
    }
    const controller = new AbortController()
    setLink({ status: 'checking' })
    void fetchPasswordResetStatus(token, controller.signal)
      .then((status) => {
        if (controller.signal.aborted) {
          return
        }
        setLink(
          status.valid
            ? {
                status: 'usable',
                displayName: status.display_name ?? '',
                firstPassword: status.first_password === true,
              }
            : { status: 'unusable' },
        )
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setLink({ status: 'unreachable' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [token, attempt])

  const rejection = submit.status === 'error' ? submit.rejection : null
  // Every rejection is rendered through here so the sentence naming a number —
  // the password minimum — reads the same constant the input enforces.
  const rejectionMessage =
    rejection === null ? '' : t(rejection.messageKey, { min: MIN_PASSWORD_LENGTH })

  /**
   * Wraps a field's setter so that typing clears a standing rejection: leaving
   * „the passwords do not match" under input the reader has since corrected
   * would make the form argue with something it has not seen.
   */
  function change(setter: (value: string) => void) {
    return (value: string) => {
      setter(value)
      setSubmit((current) => (current.status === 'error' ? { status: 'idle' } : current))
    }
  }

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (token === undefined || password.length < MIN_PASSWORD_LENGTH || passwordRepeat === '') {
      setValidated(true)
      return
    }
    // Checked here rather than by the server, which never sees the second
    // password: a typo in it is the one rejection this form can word itself.
    if (password !== passwordRepeat) {
      setSubmit({
        status: 'error',
        rejection: {
          field: 'passwordRepeat',
          messageKey: 'passwordReset.errorPasswordMismatch',
        },
      })
      return
    }
    setSubmit({ status: 'submitting' })
    try {
      await resetPassword(token, password)
      setSubmit({ status: 'done' })
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 404) {
        // The link expired (or was spent elsewhere) while this page was open:
        // the same dead-link explanation as on opening, and the form goes.
        setLink({ status: 'unusable' })
        setSubmit({ status: 'idle' })
        return
      }
      setSubmit({ status: 'error', rejection: rejectionFor(error) })
    }
  }

  const retry = useCallback(() => {
    setAttempt((current) => current + 1)
  }, [])

  const submitting = submit.status === 'submitting'
  const tooShort = validated && password.length < MIN_PASSWORD_LENGTH

  return (
    <AuthFrame title={title}>
      {link.status === 'checking' && (
        <div className="text-center py-3" data-testid="password-reset-checking">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('passwordReset.checking')}</span>
          </Spinner>
        </div>
      )}

      {link.status === 'unusable' && (
        <div data-testid="password-reset-invalid">
          <Alert variant="secondary" role="status">
            <Alert.Heading as="h2" className="h5">
              <Icon name="slash-circle" className="me-2" />
              {t('passwordReset.invalidTitle')}
            </Alert.Heading>
            <p className="mb-0">{t('passwordReset.invalidBody')}</p>
          </Alert>
          <div className="text-center">
            <Link to="/login">{t('passwordReset.backToLogin')}</Link>
          </div>
        </div>
      )}

      {link.status === 'unreachable' && (
        <div data-testid="password-reset-unreachable">
          <Alert variant="warning" role="status">
            <Alert.Heading as="h2" className="h5">
              <Icon name="wifi-off" className="me-2" />
              {t('passwordReset.checkFailedTitle')}
            </Alert.Heading>
            <p className="mb-0">{t('passwordReset.checkFailedBody')}</p>
          </Alert>
          <div className="d-grid">
            <Button variant="primary" onClick={retry}>
              {t('passwordReset.retry')}
            </Button>
          </div>
          <div className="text-center mt-3">
            <Link to="/login">{t('passwordReset.backToLogin')}</Link>
          </div>
        </div>
      )}

      {link.status === 'usable' && submit.status === 'done' && (
        <div data-testid="password-reset-done">
          <Alert variant="success" role="status">
            <Alert.Heading as="h2" className="h5">
              <Icon name="shield-lock" className="me-2" />
              {first ? t('passwordReset.firstDoneTitle') : t('passwordReset.doneTitle')}
            </Alert.Heading>
            {!first && <p className="mb-0">{t('passwordReset.doneSessions')}</p>}
            <p className="mb-0">
              {loginPath === '/login'
                ? t('passwordReset.doneSignIn')
                : t('passwordReset.doneSignInProject')}
            </p>
          </Alert>
          <div className="d-grid">
            <Link to={loginPath} className="btn btn-primary">
              {loginPath === '/login'
                ? t('passwordReset.backToLogin')
                : t('passwordReset.signInToProject')}
            </Link>
          </div>
        </div>
      )}

      {link.status === 'usable' && submit.status !== 'done' && (
        <>
          <p className="mb-1">{t('passwordReset.greeting', { name: link.displayName })}</p>
          <p className="text-secondary">
            {first ? t('passwordReset.firstIntro') : t('passwordReset.intro')}
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
            <Form.Group className="mb-3" controlId="password-reset-password">
              <Form.Label>
                {first ? t('passwordReset.firstPassword') : t('passwordReset.password')}
              </Form.Label>
              <Form.Control
                type="password"
                autoComplete="new-password"
                autoFocus
                required
                minLength={MIN_PASSWORD_LENGTH}
                isInvalid={tooShort || rejection?.field === 'password'}
                // react-bootstrap's `isInvalid` only paints the input red;
                // a screen reader learns nothing from a colour.
                aria-invalid={tooShort || rejection?.field === 'password' ? true : undefined}
                aria-describedby="password-reset-password-help"
                value={password}
                onChange={(event) => {
                  change(setPassword)(event.target.value)
                }}
                disabled={submitting}
              />
              <Form.Text id="password-reset-password-help" className="text-secondary d-block">
                {t('passwordReset.passwordHint', { min: MIN_PASSWORD_LENGTH })}
              </Form.Text>
              <Form.Control.Feedback type="invalid">
                {rejection?.field === 'password'
                  ? rejectionMessage
                  : t('passwordReset.passwordRequired', { min: MIN_PASSWORD_LENGTH })}
              </Form.Control.Feedback>
            </Form.Group>

            <Form.Group className="mb-3" controlId="password-reset-repeat">
              <Form.Label>{t('passwordReset.passwordRepeat')}</Form.Label>
              <Form.Control
                type="password"
                autoComplete="new-password"
                required
                isInvalid={rejection?.field === 'passwordRepeat'}
                aria-invalid={rejection?.field === 'passwordRepeat' ? true : undefined}
                value={passwordRepeat}
                onChange={(event) => {
                  change(setPasswordRepeat)(event.target.value)
                }}
                disabled={submitting}
              />
              <Form.Control.Feedback type="invalid">
                {rejection?.field === 'passwordRepeat'
                  ? rejectionMessage
                  : t('passwordReset.passwordRepeatRequired')}
              </Form.Control.Feedback>
            </Form.Group>

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
                {first ? t('passwordReset.firstSubmit') : t('passwordReset.submit')}
              </Button>
            </div>
          </Form>

          <div className="text-center mt-3">
            <Link to="/login">{t('passwordReset.backToLogin')}</Link>
          </div>
        </>
      )}
    </AuthFrame>
  )
}
