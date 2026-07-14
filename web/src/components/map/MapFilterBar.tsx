import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { hasActiveMapFilters, MAP_DEFAULTS, type MapView } from '../../lib/mapView'
import { type SetUrlState } from '../../lib/urlState'
import { type Mapset, MAPSETS } from '../../services/map'

/** Props for {@link MapFilterBar}. */
export interface MapFilterBarProps {
  view: MapView
  onChange: SetUrlState<MapView>
  /** The active mapset (selected button). */
  mapset: Mapset
  /** Number of geotagged photos currently plotted. */
  count: number
}

/**
 * Controls for the map view: the mapset switch (basic / outdoor / aerial) plus
 * the photo filters the GeoJSON feed honours (date range, archived).
 * Every control writes through `onChange` into the URL, so Back/Forward and a
 * shared link reproduce the map. The mapset and filters push history entries;
 * the date inputs do too (they are discrete choices, not live-typed text).
 */
export function MapFilterBar({ view, onChange, mapset, count }: MapFilterBarProps) {
  const { t } = useTranslation()

  return (
    <Form className="mb-3" aria-label={t('map.filters.label')}>
      <div className="mb-2">
        <ButtonGroup size="sm" aria-label={t('map.mapset.label')}>
          {MAPSETS.map((id) => (
            <Button
              key={id}
              type="button"
              variant={id === mapset ? 'primary' : 'outline-secondary'}
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

      <div className="d-flex align-items-center justify-content-between mt-2">
        <span className="text-secondary small" aria-live="polite">
          {t('map.count', { count })}
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
