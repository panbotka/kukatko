import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatCount } from '../../lib/format'
import { hasActiveMapFilters, MAP_DEFAULTS, type MapView } from '../../lib/mapView'
import { type SetUrlState } from '../../lib/urlState'
import { type MapCoverage, type Mapset, MAPSETS } from '../../services/map'

/**
 * Where the "fill in the missing locations" link leads: the library scoped to the
 * photos that have no position at all. That is the page where a location gets
 * set — open a photo, place it on the picker map — so the link hands over the
 * exact working set rather than an admin button that would run an estimate over
 * the whole library behind the user's back.
 */
const NO_LOCATION_LIBRARY = '/library?has_gps=false'

/** Props for {@link MapFilterBar}. */
export interface MapFilterBarProps {
  view: MapView
  onChange: SetUrlState<MapView>
  /** The active mapset (selected button). */
  mapset: Mapset
  /** Number of geotagged photos currently plotted. */
  count: number
  /**
   * How many of the filtered photos the map could place, out of how many match
   * at all. Omitted while the feed is still loading, and for a backend that does
   * not report it; the bar then falls back to the plain marker count.
   */
  coverage?: MapCoverage | null
  /**
   * Whether the viewer may edit photo metadata, and so act on the missing
   * locations. A viewer is told the same number — the map being 11 % of the
   * library is a fact about what they are looking at — but is not sent to a page
   * where every photo is read-only.
   */
  canWrite?: boolean
}

/**
 * Controls for the map view: the mapset switch (basic / outdoor / aerial) plus
 * the photo filters the GeoJSON feed honours (date range, archived).
 * Every control writes through `onChange` into the URL, so Back/Forward and a
 * shared link reproduce the map. The mapset and filters push history entries;
 * the date inputs do too (they are discrete choices, not live-typed text).
 *
 * The footer says how much of the library is actually on the map. On this
 * collection that is about one photo in nine, and the old "Fotek na mapě: 2 378"
 * left the other eight unexplained — a map can look empty because a filter is
 * narrow or because nothing was ever geotagged, and those call for opposite
 * reactions. Editors get the missing photos as a link.
 */
export function MapFilterBar({
  view,
  onChange,
  mapset,
  count,
  coverage,
  canWrite = false,
}: MapFilterBarProps) {
  const { t, i18n } = useTranslation()
  const missing =
    coverage === undefined || coverage === null ? 0 : coverage.total - coverage.located

  return (
    <Form className="mb-3" aria-label={t('map.filters.label')}>
      <div className="mb-2">
        <ButtonGroup size="sm" aria-label={t('map.mapset.label')}>
          {MAPSETS.map((id) => (
            <Button
              key={id}
              type="button"
              // The inactive style is `outline-light`, not `outline-secondary`:
              // Superhero's secondary is a desaturated navy that on this page's
              // near-black background reads as an unlit button, and the two
              // unchosen mapsets then look disabled rather than available.
              variant={id === mapset ? 'primary' : 'outline-light'}
              active={id === mapset}
              aria-pressed={id === mapset}
              onClick={() => {
                onChange({ mapset: id })
              }}
            >
              {t(`map.mapset.${id}`)}
            </Button>
          ))}
        </ButtonGroup>
      </div>

      <Row className="g-2 align-items-end">
        <Col xs={6} sm={3} md={2}>
          <Form.Group controlId="map-taken-after">
            <Form.Label className="small mb-1">{t('map.filters.takenAfter')}</Form.Label>
            <Form.Control
              type="date"
              value={view.taken_after}
              onChange={(e) => {
                onChange({ taken_after: e.target.value })
              }}
            />
          </Form.Group>
        </Col>

        <Col xs={6} sm={3} md={2}>
          <Form.Group controlId="map-taken-before">
            <Form.Label className="small mb-1">{t('map.filters.takenBefore')}</Form.Label>
            <Form.Control
              type="date"
              value={view.taken_before}
              onChange={(e) => {
                onChange({ taken_before: e.target.value })
              }}
            />
          </Form.Group>
        </Col>

        <Col xs={6} sm={3} md={2}>
          <Form.Group controlId="map-archived">
            <Form.Label className="small mb-1">{t('map.filters.archived')}</Form.Label>
            <Form.Select
              value={view.archived}
              onChange={(e) => {
                onChange({ archived: e.target.value })
              }}
            >
              <option value="false">{t('map.archived.hide')}</option>
              <option value="true">{t('map.archived.show')}</option>
              <option value="only">{t('map.archived.only')}</option>
            </Form.Select>
          </Form.Group>
        </Col>
      </Row>

      <div className="d-flex align-items-center justify-content-between gap-2 mt-2 flex-wrap">
        <span className="text-secondary small" aria-live="polite">
          {coverage === undefined || coverage === null ? (
            t('map.count', { count })
          ) : (
            <>
              {/* Grouped by hand: five-digit counts are read wrong ungrouped,
                  and i18next runs without a number formatter. */}
              {t('map.coverage', {
                located: formatCount(coverage.located, i18n.language),
                total: formatCount(coverage.total, i18n.language),
              })}
              {canWrite && missing > 0 && (
                <>
                  {' '}
                  <Link to={NO_LOCATION_LIBRARY}>{t('map.coverageAction')}</Link>
                </>
              )}
            </>
          )}
        </span>
        {hasActiveMapFilters(view) && (
          <Button
            type="button"
            size="sm"
            variant="outline-secondary"
            onClick={() => {
              // Reset the photo filters but keep the chosen mapset and viewport.
              onChange({
                taken_after: MAP_DEFAULTS.taken_after,
                taken_before: MAP_DEFAULTS.taken_before,
                archived: MAP_DEFAULTS.archived,
                album: MAP_DEFAULTS.album,
                label: MAP_DEFAULTS.label,
              })
            }}
          >
            {t('map.filters.clear')}
          </Button>
        )}
      </div>
    </Form>
  )
}
