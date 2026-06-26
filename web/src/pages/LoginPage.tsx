import { type SyntheticEvent, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { ApiError } from '../services/auth'

/** Shape of the history state set by the route guard on redirect to login. */
interface LocationState {
  from?: { pathname?: string }
}

type LoginErrorKey = 'login.errorInvalid' | 'login.errorRateLimited' | 'login.errorGeneric'

type SubmitState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'error'; messageKey: LoginErrorKey }

/** Maps a failed login to the i18n key of the message to show the user. */
function errorKeyFor(error: unknown): LoginErrorKey {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return 'login.errorInvalid'
    }
    if (error.status === 429) {
      return 'login.errorRateLimited'
    }
  }
  return 'login.errorGeneric'
}

/**
 * Login page: a Superhero-styled card with username + password. Validates that
 * both fields are filled, surfaces invalid-credentials and rate-limit errors,
 * and on success redirects to the originally requested route (or home).
 */
export function LoginPage() {
  const { t } = useTranslation()
  const { status: authStatus, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [validated, setValidated] = useState(false)
  const [submit, setSubmit] = useState<SubmitState>({ status: 'idle' })

  const from = (location.state as LocationState | null)?.from?.pathname ?? '/'

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

  return (
    <Row className="justify-content-center">
      <Col xs={12} sm={10} md={6} lg={5} xl={4}>
        <Card bg="dark" text="light" border="secondary" className="mt-4 mt-md-5">
          <Card.Body>
            <Card.Title as="h1" className="h3 mb-4 text-center">
              {t('login.title')}
            </Card.Title>

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
                  autoFocus
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
          </Card.Body>
        </Card>
      </Col>
    </Row>
  )
}
