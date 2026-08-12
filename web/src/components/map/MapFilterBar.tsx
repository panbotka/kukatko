import type { TFunction } from 'i18next'
import { useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Offcanvas from 'react-bootstrap/Offcanvas'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useIsNarrowViewport } from '../../hooks/useIsNarrowViewport'
import { formatCount } from '../../lib/format'
import { LIBRARY_PATH } from '../../lib/libraryView'
import { activeMapFilterCount, MAP_DEFAULTS, type MapView } from '../../lib/mapView'
import { type SetUrlState } from '../../lib/urlState'
import { type MapCoverage, type Mapset, MAPSETS } from '../../services/map'
import { Icon } from '../Icon'

/** DOM id of the phone's offcanvas control panel. */
const PANEL_ID = 'map-filter-panel'

/**
 * Where the "fill in the missing locations" link leads: the library scoped to the
 * photos that have no position at all. That is the page where a location gets
 * set — open a photo, place it on the picker map — so the link hands over the
 * exact working set rather than an admin button that would run an estimate over
 * the whole library behind the user's back.
 *
 * Built from {@link LIBRARY_PATH}, not the retired `/library`: an in-app link has
 * to point at the library's own route. The redirect that still catches `/library`
 * is there for bookmarks minted before the swap (and for the addresses inherited
 * from the PhotoPrism instance whose domain this one took over) — not a detour
 * for the app's own navigation.
 */
const NO_LOCATION_LIBRARY = `${LIBRARY_PATH}?has_gps=false`

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
 * On desktop everything is laid out above the map: the mapset row, the filter
 * row, and a footer saying how much of the library is actually on the map.
 *
 * **On a phone the bar is one button.** Stacked out on a 390 px screen those
 * three rows came to 382 px — 45 % of the viewport spent before the map even
 * began, the very disease the library was cured of. So the controls fold into
 * an offcanvas drawer behind a Filters button badged with the number of active
 * filters, exactly as the library's `FilterBar` does for the photo grid, rather
 * than this page inventing a second way to hide a control. The drawer closes on
 * a sticky footer carrying the live marker count
 * ({@link MapFilterDrawerFooter}), so a filter combination is read before the
 * trip back up.
 *
 * The coverage statement survives that fold, because it is the one thing on this
 * page that says the map is partial: the phone header states it in short beside
 * the button (`map.coverageShort`), and the drawer carries the full
 * sentence with the editor's "fill in locations" link. On this collection the
 * map speaks for about one photo in nine, and the old "Fotek na mapě: 2 378"
 * left the other eight unexplained — a map can look empty because a filter is
 * narrow or because nothing was ever geotagged, and those call for opposite
 * reactions.
 */
export function MapFilterBar({
  view,
  onChange,
  mapset,
  count,
  coverage,
  canWrite = false,
}: MapFilterBarProps) {
  const { t } = useTranslation()
  const narrow = useIsNarrowViewport()
  const [open, setOpen] = useState(false)
  const activeFilters = activeMapFilterCount(view)

  const clearFilters = () => {
    // Reset the photo filters but keep the chosen mapset and viewport.
    onChange({
      taken_after: MAP_DEFAULTS.taken_after,
      taken_before: MAP_DEFAULTS.taken_before,
      archived: MAP_DEFAULTS.archived,
      album: MAP_DEFAULTS.album,
      label: MAP_DEFAULTS.label,
    })
  }

  // Every control, defined once and placed in exactly one of the two layouts —
  // inline above the map on desktop, inside the drawer on a phone — so no
  // control is ever in the document twice. `stacked` is the only difference:
  // full-width, finger-sized fields in the drawer, the compact row on desktop.
  const controls = (
    <>
      <MapsetSwitch mapset={mapset} onChange={onChange} stacked={narrow} />
      <PhotoFilters view={view} onChange={onChange} stacked={narrow} />
    </>
  )

  return (
    <Form className="kukatko-map-filter-bar" aria-label={t('map.filters.label')}>
      {narrow ? (
        // The whole phone header: the button that opens the controls, and the
        // one number this page must not lose. `justify-content-end` keeps both
        // on the side the drawer comes from, and the short coverage wraps under
        // the button on a screen too narrow for the pair.
        <div className="d-flex flex-wrap align-items-center justify-content-end gap-2">
          <Button
            type="button"
            size="lg"
            variant={open || activeFilters > 0 ? 'primary' : 'outline-primary'}
            className="d-inline-flex align-items-center gap-2"
            aria-expanded={open}
            aria-controls={PANEL_ID}
            onClick={() => {
              setOpen((prev) => !prev)
            }}
          >
            <Icon name="funnel" />
            {t('map.filters.toggle')}
            {activeFilters > 0 && (
              <Badge bg="light" text="dark" pill>
                {activeFilters}
              </Badge>
            )}
          </Button>
          <CoverageStatement coverage={coverage} count={count} compact announce />
        </div>
      ) : (
        <>
          {controls}
          <div className="d-flex align-items-center justify-content-between gap-2 mt-2 flex-wrap">
            <CoverageStatement coverage={coverage} count={count} canWrite={canWrite} announce />
            {activeFilters > 0 && (
              <Button type="button" size="sm" variant="outline-secondary" onClick={clearFilters}>
                {t('map.filters.clear')}
              </Button>
            )}
          </div>
        </>
      )}

      {narrow && (
        <Offcanvas
          show={open}
          onHide={() => {
            setOpen(false)
          }}
          placement="end"
          aria-label={t('map.filters.label')}
        >
          <Offcanvas.Header closeButton>
            <Offcanvas.Title>{t('map.filters.label')}</Offcanvas.Title>
          </Offcanvas.Header>
          <Offcanvas.Body id={PANEL_ID}>
            {controls}
            <hr className="my-3" />
            {/* The full sentence, and the only place an editor can reach the
                photos it is about. Not a live region: the header states the
                same number behind the drawer, and announcing both would say it
                twice on every filter change. */}
            <CoverageStatement coverage={coverage} count={count} canWrite={canWrite} />
          </Offcanvas.Body>
          {/* A sibling of the body, not a child of it: `.offcanvas` is a flex
              column whose body grows and scrolls, so the footer is pinned to the
              bottom edge *and* subtracted from the scroll area. */}
          <MapFilterDrawerFooter
            count={count}
            clearVisible={activeFilters > 0}
            onClear={clearFilters}
            onClose={() => {
              setOpen(false)
            }}
          />
        </Offcanvas>
      )}
    </Form>
  )
}

/**
 * The base-map switch. Three mutually exclusive choices, so a button group in
 * both placements — only the sizing changes: a compact `sm` row above the map on
 * desktop, and in the drawer a full-width group under a visible caption, sized
 * for a thumb.
 */
function MapsetSwitch({
  mapset,
  onChange,
  stacked,
}: {
  mapset: Mapset
  onChange: SetUrlState<MapView>
  stacked: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className={stacked ? 'mb-3' : 'mb-2'}>
      {/* A plain caption, not a `<label>`: a button group is nothing that can be
          labelled *for*. It names itself to assistive technology through the
          group's own `aria-label`, which is why the caption is only drawn where
          there is room for it. */}
      {stacked && <div className="small mb-1">{t('map.mapset.label')}</div>}
      <ButtonGroup
        size={stacked ? undefined : 'sm'}
        className={stacked ? 'w-100' : undefined}
        aria-label={t('map.mapset.label')}
      >
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
  )
}

/**
 * The filters the GeoJSON feed honours: the capture-date range and the archive
 * selector. `stacked` is the drawer's layout — the two dates side by side, the
 * archive select on its own full-width row, all of them finger-sized
 * (`.kukatko-map-filter-panel`) — against the compact desktop row above the map.
 */
function PhotoFilters({
  view,
  onChange,
  stacked,
}: {
  view: MapView
  onChange: SetUrlState<MapView>
  stacked: boolean
}) {
  const { t } = useTranslation()
  return (
    <Row className={stacked ? 'kukatko-map-filter-panel g-3' : 'g-2 align-items-end'}>
      <Col xs={6} sm={stacked ? 6 : 3} md={stacked ? 6 : 2}>
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

      <Col xs={6} sm={stacked ? 6 : 3} md={stacked ? 6 : 2}>
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

      <Col xs={stacked ? 12 : 6} sm={stacked ? 12 : 3} md={stacked ? 12 : 2}>
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
  )
}

/**
 * How much of the library the map speaks for, in one of two lengths.
 *
 * `compact` is the phone header's version — "2 378 z 20 906 na mapě" — which
 * fits beside the Filters button and still says the map is a fraction of the
 * library; the full sentence says *why* the rest is missing and is what desktop
 * and the drawer show. Both fall back to the plain marker count for a feed that
 * reports no coverage at all.
 *
 * `announce` marks the copy that is the bar's live statement of the number.
 * Exactly one instance per layout carries it, so a filter change is announced
 * once and not once per place the number is written.
 */
function CoverageStatement({
  coverage,
  count,
  canWrite = false,
  compact = false,
  announce = false,
}: {
  coverage: MapCoverage | null | undefined
  count: number
  canWrite?: boolean
  compact?: boolean
  announce?: boolean
}) {
  const { t, i18n } = useTranslation()
  const live = announce ? 'polite' : undefined

  if (coverage === undefined || coverage === null) {
    return (
      <span className="text-secondary small" aria-live={live}>
        {t('map.count', { count })}
      </span>
    )
  }

  // Grouped by hand: five-digit counts are read wrong ungrouped, and i18next
  // runs without a number formatter.
  const located = formatCount(coverage.located, i18n.language)
  const total = formatCount(coverage.total, i18n.language)
  const missing = coverage.total - coverage.located

  return (
    <span className="text-secondary small" aria-live={live}>
      {compact ? (
        t('map.coverageShort', { located, total })
      ) : (
        <>
          {t('map.coverage', { located, total })}
          {canWrite && missing > 0 && (
            <>
              {' '}
              <Link to={NO_LOCATION_LIBRARY}>{t('map.coverageAction')}</Link>
            </>
          )}
        </>
      )}
    </span>
  )
}

/**
 * What the drawer's primary button says: the live number of photos the map is
 * plotting, or — at zero, where "Show 0 photos" would read as a broken promise —
 * the empty-set wording. Either way the button closes.
 *
 * `count` still goes in, because it is what picks the plural form; what the
 * sentence actually shows is `shown`, the same hand-grouped figure the coverage
 * line directly above the button uses, so the drawer does not print "2 378" and
 * "2378" one under the other.
 */
function applyLabel(t: TFunction, count: number, language: string): string {
  return count === 0
    ? t('map.filters.applyEmpty')
    : t('map.filters.apply', { count, shown: formatCount(count, language) })
}

/**
 * The phone drawer's sticky footer: the way out, with the answer already on it.
 *
 * The drawer covers the map, so a reader setting a date range and an archive
 * mode would otherwise have to close it to learn whether anything was left to
 * plot. The primary button carries that count instead and closes on the way
 * back; it needs no state of its own, since `count` is the same number the bar
 * already states, so it follows every filter change live with the drawer still
 * open.
 *
 * "Clear filters" repeats the bar's own action (and, like it, appears only when
 * there is something to clear) and deliberately does *not* close: clearing is
 * how a reader recovers from an empty map, and the count beside it updates in
 * place to show the recovery worked.
 */
function MapFilterDrawerFooter({
  count,
  clearVisible,
  onClear,
  onClose,
}: {
  count: number
  clearVisible: boolean
  onClear: () => void
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  return (
    <div className="offcanvas-footer kukatko-filter-footer">
      <Button type="button" size="lg" variant="primary" className="w-100" onClick={onClose}>
        {applyLabel(t, count, i18n.language)}
      </Button>
      {clearVisible && (
        <Button
          type="button"
          size="lg"
          variant="outline-secondary"
          className="w-100"
          onClick={onClear}
        >
          {t('map.filters.clear')}
        </Button>
      )}
    </div>
  )
}
