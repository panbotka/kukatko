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
import { Icon, type IconName } from '../components/Icon'
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
  /** Names the action the tile performs, for its `title` tooltip. */
  actionKey: ParseKeys
  icon: IconName
  /** When true the tile is only shown to users who can upload (editor/admin). */
  writeOnly?: boolean
}

/**
 * The everyday welcome tiles. Titles, icons and action tooltips reuse the navbar
 * entries so a tile and its nav item read identically; each description lives
 * under `home.tiles.*`. Upload is a write action, gated behind `canWrite`.
 */
const TILES: HomeTile[] = [
  {
    to: '/library',
    titleKey: 'nav.library',
    descKey: 'home.tiles.library',
    actionKey: 'nav.titles.library',
    icon: 'images',
  },
  {
    to: '/search',
    titleKey: 'nav.search',
    descKey: 'home.tiles.search',
    actionKey: 'nav.titles.search',
    icon: 'search',
  },
  {
    to: '/albums',
    titleKey: 'nav.albums',
    descKey: 'home.tiles.albums',
    actionKey: 'nav.titles.albums',
    icon: 'collection',
  },
  {
    to: '/labels',
    titleKey: 'nav.labels',
    descKey: 'home.tiles.labels',
    actionKey: 'nav.titles.labels',
    icon: 'tags',
  },
  {
    to: '/people',
    titleKey: 'nav.people',
    descKey: 'home.tiles.people',
    actionKey: 'nav.titles.people',
    icon: 'people',
  },
  {
    to: '/map',
    titleKey: 'nav.map',
    descKey: 'home.tiles.map',
    actionKey: 'nav.titles.map',
    icon: 'map',
  },
  {
    to: '/upload',
    titleKey: 'nav.upload',
    descKey: 'home.tiles.upload',
    actionKey: 'nav.titles.upload',
    icon: 'cloud-arrow-up',
    writeOnly: true,
  },
]

/**
 * Landing page: a friendly welcome with large, clearly labelled cards linking to
 * the app's main destinations (library, search, albums, labels, people, map, and
 * — for editors — upload). A small, de-emphasised status line at the bottom quietly
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
      <h1 className="kk-page-title mb-1">{t('home.title')}</h1>
      <p className="text-secondary mb-4">{t('home.subtitle')}</p>

      <Row xs={1} sm={2} lg={3} className="g-3">
        {tiles.map((tile) => (
          <Col key={tile.to}>
            <Card
              as={Link}
              to={tile.to}
              text="light"
              title={t(tile.actionKey)}
              className="h-100 text-decoration-none kukatko-home-tile"
            >
              <Card.Body>
                <Card.Title
                  as="h2"
                  className="kk-section-title mb-1 d-flex align-items-center gap-2"
                >
                  <Icon name={tile.icon} />
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
