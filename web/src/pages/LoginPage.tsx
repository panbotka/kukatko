import { type SyntheticEvent, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useRegistrationOpen } from '../hooks/useRegistrationOpen'
import { ApiError, NetworkError } from '../services/auth'

/** Shape of the history state set by the route guard on redirect to login. */
interface LocationState {
  from?: { pathname?: string; search?: string }
}

/**
 * Rebuilds the address the guard bounced the visitor off, query string and all.
 *
 * The search matters: it is where the app keeps view state (filters, sorting,
 * paging), and it is how a share from the phone's share sheet names the files
 * waiting in the cache — `/share-target?share=<id>`. Dropping it used to mean a
 * shared batch of photos was lost the moment the sharer turned out to be signed
 * out. Falls back to the library when there is nothing stashed.
 */
function returnTo(state: LocationState | null): string {
  const pathname = state?.from?.pathname
  if (pathname === undefined || pathname === '') {
    return '/'
  }
  return `${pathname}${state?.from?.search ?? ''}`
}

type LoginErrorKey =
  | 'login.errorInvalid'
  | 'login.errorPendingApproval'
  | 'login.errorRateLimited'
  | 'login.errorOffline'
  | 'login.errorGeneric'

type SubmitState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'error'; messageKey: LoginErrorKey }

/**
 * Maps a failed login to the i18n key of the message to show the user.
 *
 * The network branch comes first and matters most. A {@link NetworkError} means
 * the credentials never left the device, so nothing judged them; falling through
 * to the generic "sign in failed, try again" was how an offline phone told its
 * owner to retype a password that was perfectly correct.
 *
 * A 403 is the sign-in's own outcome for an account that registered and is
 * waiting for an administrator: the password was right, so telling them it was
 * not would send them round in circles retyping it.
 */
function errorKeyFor(error: unknown): LoginErrorKey {
  if (error instanceof NetworkError) {
    return 'login.errorOffline'
  }
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return 'login.errorInvalid'
    }
    if (error.status === 403) {
      return 'login.errorPendingApproval'
    }
    if (error.status === 429) {
      return 'login.errorRateLimited'
    }
  }
  return 'login.errorGeneric'
}

/**
 * Login page: a Superhero-styled card with username + password. Validates that
 * both fields are filled, surfaces invalid-credentials, rate-limit and
 * unreachable-backend errors, and on success redirects to the originally
 * requested route (or home).
 *
 * A guarded route with no backend now shows the offline page instead of sending
 * anyone here, but this address is still reachable on its own — a bookmark, or
 * the installed app reopening on the screen it was last left on. So when the
 * session probe has already found the server unreachable, the form says so up
 * front and keeps its hands off the keyboard: `autoFocus` raising the phone's
 * keyboard for a form that cannot succeed is an invitation to type.
 *
 * Under the form sits the way to {@link RegisterPage} — but only on an instance
 * that reports registration open. A closed one (and one that could not be asked)
 * shows nothing at all, because a link to a form that refuses every submit costs
 * the reader the whole form before they learn it.
 */
export function LoginPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('login.title'))
  const { status: authStatus, login } = useAuth()
  // Only an instance that says registration is open gets the invitation below.
  // A closed one — and one that could not be asked at all — shows nothing: a
  // link to a form that answers every submit with "not open" is worse than no
  // link, because the reader fills it in before finding out.
  const registration = useRegistrationOpen()
  const navigate = useNavigate()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [validated, setValidated] = useState(false)
  const [submit, setSubmit] = useState<SubmitState>({ status: 'idle' })

  const from = returnTo(location.state as LocationState | null)

  // If an already-authenticated user lands on /login, bounce them onward.
  useEffect(() => {
    if (authStatus === 'authenticated') {
      void navigate(from, { replace: true })
    }
  }, [authStatus, from, navigate])

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (username.trim() === '' || password === '') {
      setValidated(true)
      return
    }
    setSubmit({ status: 'submitting' })
    try {
      await login(username.trim(), password)
      void navigate(from, { replace: true })
    } catch (error: unknown) {
      setSubmit({ status: 'error', messageKey: errorKeyFor(error) })
    }
  }

  const submitting = submit.status === 'submitting'
  // The session probe already tried and failed to reach the backend, so this
  // form has nothing to talk to — until it does.
  const unreachable = authStatus === 'unreachable'

  return (
    <Row className="justify-content-center">
      <Col xs={12} sm={10} md={6} lg={5} xl={4}>
        <Card text="light" className="mt-4 mt-md-5">
          <Card.Body>
            <Card.Title as="h1" className="kk-page-title mb-4 text-center">
              {t('login.title')}
            </Card.Title>

            {/* The warning yields to a submit error: once they have pressed the
                button, the sentence about that attempt is the more useful one,
                and both say the same thing anyway. */}
            {unreachable && submit.status !== 'error' && (
              <Alert variant="warning" role="status" data-testid="login-offline-notice">
                <Icon name="wifi-off" className="me-2" />
                {t('login.offlineNotice')}
              </Alert>
            )}

            {submit.status === 'error' && (
              <Alert variant="danger" role="alert">
                {t(submit.messageKey)}
              </Alert>
            )}

            <Form
              noValidate
              validated={validated}
              onSubmit={(event) => {
                void handleSubmit(event)
              }}
            >
              <Form.Group className="mb-3" controlId="login-username">
                <Form.Label>{t('login.username')}</Form.Label>
                <Form.Control
                  type="text"
                  name="username"
                  autoComplete="username"
                  autoFocus={!unreachable}
                  required
                  value={username}
                  onChange={(event) => {
                    setUsername(event.target.value)
                  }}
                  disabled={submitting}
                />
                <Form.Control.Feedback type="invalid">
                  {t('login.usernameRequired')}
                </Form.Control.Feedback>
              </Form.Group>

              <Form.Group className="mb-4" controlId="login-password">
                <Form.Label>{t('login.password')}</Form.Label>
                <Form.Control
                  type="password"
                  name="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(event) => {
                    setPassword(event.target.value)
                  }}
                  disabled={submitting}
                />
                <Form.Control.Feedback type="invalid">
                  {t('login.passwordRequired')}
                </Form.Control.Feedback>
              </Form.Group>

              <div className="d-grid">
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
                  {t('login.submit')}
                </Button>
              </div>
            </Form>

            {registration === 'open' && (
              <div className="text-center mt-3" data-testid="login-register-link">
                <span className="text-secondary me-1">{t('login.registerPrompt')}</span>
                <Link to="/register">{t('login.registerLink')}</Link>
              </div>
            )}
          </Card.Body>
        </Card>
      </Col>
    </Row>
  )
}
