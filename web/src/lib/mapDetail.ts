import { DETAIL_DEFAULTS, detailQueryString } from './detailView'
import { formatBBox, type GeoBounds } from './mapArea'
import { mapViewToParams, type MapView } from './mapView'

/**
 * The query string a photo opened from the map's area panel carries: the area
 * as `bbox` plus the photo filters the map is showing. The viewer pages through
 * `GET /photos` with exactly these — the same box, the same filters, the same
 * newest-first order the feed (and so the panel) uses — so prev/next walk the
 * panel's list and nothing else, and Back returns to the map fitted to the box.
 */
export function mapDetailQuery(view: MapView, bounds: GeoBounds): string {
  const filters = mapViewToParams(view)
  return detailQueryString({
    ...DETAIL_DEFAULTS,
    bbox: formatBBox(bounds),
    taken_after: filters.taken_after ?? '',
    taken_before: filters.taken_before ?? '',
    archived: filters.archived ?? DETAIL_DEFAULTS.archived,
    album: filters.album ?? '',
    label: filters.label ?? '',
  })
}
