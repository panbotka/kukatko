import type { ParseKeys } from 'i18next'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatCount } from '../../lib/format'
import { countToneAlerts, countToneClass, type CountTone } from '../../lib/jobStateTone'
import { Icon } from '../Icon'

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
  /**
   * What the tile says. A number is formatted for the active language by the
   * tile itself and is what its colour is decided from; a string is a value that
   * is not a count at all — the duplicates tile's „—" while the background scan
   * has no answer yet — and is always shown muted, since it is not a figure the
   * eye should be drawn to.
   */
  value: number | string
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
   * What the number counts, in the page's shared vocabulary — it is what the
   * number is coloured by. A backlog is `queued`: work waiting for somebody.
   * Omitted for a plain fact about the library, which stays in the body colour.
   * Whatever the tone, a zero is never coloured — a cleared backlog is not a
   * warning, it is the goal.
   */
  tone?: CountTone
}

/**
 * One tile. The whole card is the click target when it links somewhere (Bootstrap's
 * `stretched-link` over the label), because a 48-pixel-wide number is a poor one;
 * the accessible name stays the label, since a link named "16 585" tells a
 * screen-reader user nothing.
 */
function StatTile({ tile }: { tile: StatTileSpec }) {
  const { t, i18n } = useTranslation()
  const label = t(tile.labelKey)
  // A tile with no number of its own counts as zero: muted, uncoloured.
  const count = typeof tile.value === 'number' ? tile.value : 0
  const tone = tile.tone ?? 'plain'
  const tint = countToneClass(tone, count)
  return (
    <Col>
      <Card className="h-100">
        <Card.Body className="position-relative py-3">
          <div
            className={`kk-display${tint === '' ? '' : ` ${tint}`}`}
            data-testid={`tile-${tile.key}`}
          >
            {countToneAlerts(tone, count) && <Icon name="exclamation-triangle" className="me-2" />}
            {typeof tile.value === 'number' ? formatCount(tile.value, i18n.language) : tile.value}
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
