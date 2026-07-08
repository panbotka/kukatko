import type { ParseKeys } from 'i18next'
import { useEffect, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { fetchHealth, type HealthResponse } from '../services/health'

type HealthState =
  | { status: 'loading' }
  | { status: 'ok'; data: HealthResponse }
  | { status: 'error' }

/** A welcome-tile: a whole-card link to one of the app's main destinations. */
interface HomeTile {
  to: string
  titleKey: ParseKeys
  descKey: ParseKeys
  /** When true the tile is only shown to users who can upload (editor/admin). */
  writeOnly?: boolean
}

/**
 * The everyday welcome tiles. Titles reuse the navbar labels so the wording
 * stays in one place; each description lives under `home.tiles.*`. Upload is a
 * write action, gated behind `canWrite`.
 */
const TILES: HomeTile[] = [
  { to: '/library', titleKey: 'nav.library', descKey: 'home.tiles.library' },
  { to: '/search', titleKey: 'nav.search', descKey: 'home.tiles.search' },
  { to: '/albums', titleKey: 'nav.albums', descKey: 'home.tiles.albums' },
  { to: '/people', titleKey: 'nav.people', descKey: 'home.tiles.people' },
  { to: '/map', titleKey: 'nav.map', descKey: 'home.tiles.map' },
  { to: '/upload', titleKey: 'nav.upload', descKey: 'home.tiles.upload', writeOnly: true },
]

/**
 * Landing page: a friendly welcome with large, clearly labelled cards linking to
 * the app's main destinations (library, search, albums, people, map, and — for
 * editors — upload). A small, de-emphasised status line at the bottom quietly
 * confirms the app is reachable and shows the build version; the technical
 * detail is kept out of the way of ordinary users rather than being the page's
 * centrepiece.
 */
export function HomePage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
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

  const tiles = TILES.filter((tile) => !tile.writeOnly || canWrite)

  return (
    <>
      <h1 className="h3 mb-1">{t('home.title')}</h1>
      <p className="text-secondary mb-4">{t('home.subtitle')}</p>

      <Row xs={1} sm={2} lg={3} className="g-3">
        {tiles.map((tile) => (
          <Col key={tile.to}>
            <Card
              as={Link}
              to={tile.to}
              bg="dark"
              text="light"
              border="secondary"
              className="h-100 text-decoration-none kukatko-home-tile"
            >
              <Card.Body>
                <Card.Title as="h2" className="h5 mb-1">
                  {t(tile.titleKey)}
                </Card.Title>
                <Card.Text className="text-secondary mb-0">{t(tile.descKey)}</Card.Text>
              </Card.Body>
            </Card>
          </Col>
        ))}
      </Row>

      <div className="text-secondary small mt-4 d-flex align-items-center gap-2 flex-wrap">
        <ApiStatusBadge state={health} />
        {health.status === 'ok' && (
          <span>
            {t('home.version')} {health.data.version.version}
          </span>
        )}
      </div>
    </>
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
