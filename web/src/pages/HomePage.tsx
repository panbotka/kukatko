import { useEffect, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { fetchHealth, type HealthResponse } from '../services/health'

type HealthState =
  | { status: 'loading' }
  | { status: 'ok'; data: HealthResponse }
  | { status: 'error' }

/**
 * Landing page. Proves end-to-end API connectivity by calling the backend
 * `GET /healthz` endpoint and surfacing the result (and build version).
 */
export function HomePage() {
  const { t } = useTranslation()
  const [health, setHealth] = useState<HealthState>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    fetchHealth(controller.signal)
      .then((data) => {
        setHealth({ status: 'ok', data })
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setHealth({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  return (
    <Row className="justify-content-center">
      <Col md={10} lg={8}>
        <h1 className="mb-2">{t('home.title')}</h1>
        <p className="text-secondary mb-4">{t('home.subtitle')}</p>

        <Card bg="dark" text="light" border="secondary">
          <Card.Body>
            <Card.Title className="d-flex align-items-center justify-content-between">
              <span>{t('home.apiStatusTitle')}</span>
              <ApiStatusBadge state={health} />
            </Card.Title>
            {health.status === 'ok' && (
              <dl className="row mb-0 mt-3">
                <dt className="col-sm-3">{t('home.version')}</dt>
                <dd className="col-sm-9">{health.data.version.version}</dd>
                <dt className="col-sm-3">{t('home.commit')}</dt>
                <dd className="col-sm-9">
                  <code>{health.data.version.commit}</code>
                </dd>
              </dl>
            )}
          </Card.Body>
        </Card>
      </Col>
    </Row>
  )
}

/** Renders the health badge for the current API request state. */
function ApiStatusBadge({ state }: { state: HealthState }) {
  const { t } = useTranslation()

  if (state.status === 'loading') {
    return (
      <Badge bg="secondary" className="d-inline-flex align-items-center gap-2">
        <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
        {t('home.checking')}
      </Badge>
    )
  }
  if (state.status === 'ok') {
    return <Badge bg="success">{t('home.healthy')}</Badge>
  }
  return <Badge bg="danger">{t('home.unhealthy')}</Badge>
}
