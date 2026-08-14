import type { ParseKeys } from 'i18next'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/**
 * One number on the admin dashboard: what it is, what it says, and — when there
 * is a screen that shows exactly the photos it counted — where clicking it goes.
 *
 * A tile with no `to` is deliberately not a link. Half of these numbers have a
 * matching view (the trash, the photos with no coordinates, the clusters waiting
 * for a name) and half do not (how much arrived last week, how many embeddings
 * exist); offering a dead link for the second half would be worse than leaving
 * them static, because a link that lands somewhere unrelated teaches the reader
 * to distrust the ones that work.
 */
export interface StatTileSpec {
  /** Stable key, also the `data-testid` suffix. */
  key: string
  /** i18n key of the label under the number. */
  labelKey: ParseKeys
  /** The number, already formatted for the active language. */
  value: string
  /** Where the tile leads; omitted when no view matches this number. */
  to?: string
  /**
   * i18n key of a line of context below the label. It is a key rather than a
   * translated string so an interpolated hint ("scanned 5 minutes ago") is
   * rendered where every other translation is, in the JSX.
   */
  hintKey?: ParseKeys
  /** Interpolation values for `hintKey`. */
  hintValues?: Record<string, string>
  /**
   * True when the number is a backlog worth acting on. It is highlighted only
   * while non-zero — a done backlog is not a warning, it is the goal.
   */
  gap?: boolean
}

/**
 * One tile. The whole card is the click target when it links somewhere (Bootstrap's
 * `stretched-link` over the label), because a 48-pixel-wide number is a poor one;
 * the accessible name stays the label, since a link named "16 585" tells a
 * screen-reader user nothing.
 */
function StatTile({ tile }: { tile: StatTileSpec }) {
  const { t } = useTranslation()
  const label = t(tile.labelKey)
  const highlight = tile.gap === true && tile.value !== '0'
  return (
    <Col>
      <Card className="h-100">
        <Card.Body className="position-relative py-3">
          <div
            className={`kk-display${highlight ? ' text-warning' : ''}`}
            data-testid={`tile-${tile.key}`}
          >
            {tile.value}
          </div>
          <div className="text-secondary kk-text-caption">
            {tile.to === undefined ? (
              label
            ) : (
              <Link to={tile.to} className="stretched-link text-reset">
                {label}
              </Link>
            )}
          </div>
          {tile.hintKey !== undefined && (
            <div className="text-secondary kk-text-caption mt-1">
              {t(tile.hintKey, tile.hintValues)}
            </div>
          )}
        </Card.Body>
      </Card>
    </Col>
  )
}

/**
 * A grid of tiles. Two per row on a phone (a single column of huge numbers would
 * be all scrolling), up to five on a wide screen so a whole section is one glance.
 */
export function StatTileGrid({ tiles }: { tiles: StatTileSpec[] }) {
  return (
    <Row className="g-2 g-md-3" xs={2} md={3} xl={5}>
      {tiles.map((tile) => (
        <StatTile key={tile.key} tile={tile} />
      ))}
    </Row>
  )
}
