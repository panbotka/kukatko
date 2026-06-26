import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ApiError, changePassword, MIN_PASSWORD_LENGTH } from '../services/auth'

type AccountErrorKey =
  | 'account.errorCurrentWrong'
  | 'account.errorTooShort'
  | 'account.errorGeneric'

type FormState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'success' }
  | { status: 'error'; messageKey: AccountErrorKey }

/** Maps a failed password change to the i18n key of the message to show. */
function errorKeyFor(error: unknown): AccountErrorKey {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return 'account.errorCurrentWrong'
    }
    if (error.status === 400) {
      return 'account.errorTooShort'
    }
  }
  return 'account.errorGeneric'
}

/**
 * Account page: shows the signed-in identity and role, and lets the user change
 * their own password via `POST /auth/password`. Validates that the new password
 * meets the minimum length and matches its confirmation before submitting.
 */
export function AccountPage() {
  const { t } = useTranslation()
  const { user, role } = useAuth()

  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [validated, setValidated] = useState(false)
  const [state, setState] = useState<FormState>({ status: 'idle' })

  const tooShort = next.length > 0 && next.length < MIN_PASSWORD_LENGTH
  const mismatch = confirm.length > 0 && confirm !== next

  async function handleSubmit(event: SyntheticEvent) {
    event.preventDefault()
    if (current === '' || next.length < MIN_PASSWORD_LENGTH || confirm !== next) {
      setValidated(true)
      return
    }
    setState({ status: 'submitting' })
    try {
      await changePassword(current, next)
      setState({ status: 'success' })
      setCurrent('')
      setNext('')
      setConfirm('')
      setValidated(false)
    } catch (error: unknown) {
      setState({ status: 'error', messageKey: errorKeyFor(error) })
    }
  }

  const submitting = state.status === 'submitting'

  return (
    <Row className="justify-content-center">
      <Col xs={12} md={8} lg={6}>
        <h1 className="h3 mb-4">{t('account.title')}</h1>

        <Card bg="dark" text="light" border="secondary" className="mb-4">
          <Card.Body>
            <dl className="row mb-0">
              <dt className="col-sm-4">{t('account.username')}</dt>
              <dd className="col-sm-8">{user?.username}</dd>
              {user?.display_name ? (
                <>
                  <dt className="col-sm-4">{t('account.displayName')}</dt>
                  <dd className="col-sm-8">{user.display_name}</dd>
                </>
              ) : null}
              <dt className="col-sm-4">{t('account.role')}</dt>
              <dd className="col-sm-8">
                {role ? <Badge bg="secondary">{t(`roles.${role}`)}</Badge> : null}
              </dd>
            </dl>
          </Card.Body>
        </Card>

        <Card bg="dark" text="light" border="secondary">
          <Card.Body>
            <Card.Title as="h2" className="h5 mb-3">
              {t('account.changePassword')}
            </Card.Title>

            {state.status === 'success' && (
              <Alert variant="success" role="alert">
                {t('account.success')}
              </Alert>
            )}
            {state.status === 'error' && (
              <Alert variant="danger" role="alert">
                {t(state.messageKey)}
              </Alert>
            )}

            <Form
              noValidate
              validated={validated}
              onSubmit={(event) => {
                void handleSubmit(event)
              }}
            >
              <Form.Group className="mb-3" controlId="account-current">
                <Form.Label>{t('account.currentPassword')}</Form.Label>
                <Form.Control
                  type="password"
                  autoComplete="current-password"
                  required
                  value={current}
                  onChange={(event) => {
                    setCurrent(event.target.value)
                  }}
                  disabled={submitting}
                />
                <Form.Control.Feedback type="invalid">
                  {t('account.currentRequired')}
                </Form.Control.Feedback>
              </Form.Group>

              <Form.Group className="mb-3" controlId="account-new">
                <Form.Label>{t('account.newPassword')}</Form.Label>
                <Form.Control
                  type="password"
                  autoComplete="new-password"
                  required
                  minLength={MIN_PASSWORD_LENGTH}
                  isInvalid={validated && tooShort}
                  value={next}
                  onChange={(event) => {
                    setNext(event.target.value)
                  }}
                  disabled={submitting}
                />
                <Form.Text className="text-secondary">
                  {t('account.passwordHint', { min: MIN_PASSWORD_LENGTH })}
                </Form.Text>
                <Form.Control.Feedback type="invalid">
                  {t('account.tooShort', { min: MIN_PASSWORD_LENGTH })}
                </Form.Control.Feedback>
              </Form.Group>

              <Form.Group className="mb-4" controlId="account-confirm">
                <Form.Label>{t('account.confirmPassword')}</Form.Label>
                <Form.Control
                  type="password"
                  autoComplete="new-password"
                  required
                  isInvalid={mismatch || (validated && confirm === '')}
                  value={confirm}
                  onChange={(event) => {
                    setConfirm(event.target.value)
                  }}
                  disabled={submitting}
                />
                <Form.Control.Feedback type="invalid">
                  {confirm === '' ? t('account.confirmRequired') : t('account.mismatch')}
                </Form.Control.Feedback>
              </Form.Group>

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
                {t('account.submit')}
              </Button>
            </Form>
          </Card.Body>
        </Card>
      </Col>
    </Row>
  )
}
