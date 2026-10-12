import { forwardRef, type CSSProperties, type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { type GridComponents, VirtuosoGrid } from 'react-virtuoso'

import { FadeInImage } from '../FadeInImage'
import { Icon } from '../Icon'
import { formatCount, formatDate } from '../../lib/format'
import { type SheetState } from '../../lib/mapView'
import { squareRenditionName } from '../../lib/rendition'
import { ProjectLink } from '../../project/ProjectLink'
import { type MapFeature } from '../../services/map'
import { thumbUrl } from '../../services/photos'

import { BottomSheet } from './BottomSheet'

/** CSS side of a grid cell: three across the desktop panel, three or four on a phone. */
const GRID_CELL_PX = 112

/** CSS side of a thumbnail in the peeking sheet's strip. */
const STRIP_CELL_PX = 72

/**
 * How many photos the peek strip renders. It is a teaser, not the list —
 * raising the sheet shows the virtualized grid — so it stays a plain,
 * unvirtualized row.
 */
const STRIP_LIMIT = 24

/** What the panel is listing. */
export type AreaPanelMode = 'area' | 'spot'

/** Props shared by both panel layouts. */
interface AreaContentProps {
  /** The photos to list, newest first (the feed's order). */
  photos: readonly MapFeature[]
  /** `area`: the viewport's photos; `spot`: one cluster opened as a grid. */
  mode: AreaPanelMode
  /** The `?…` query a photo link carries (the area box + map filters). */
  detailQuery: string
  /** Returns from a cluster's grid to the whole area. */
  onBackToArea: () => void
  /** Reports the photo under the pointer (or focus), so the map can mark its tile. */
  onHoverPhoto: (uid: string | null) => void
  /** The photo marked on the map, marked here too. */
  highlightUid: string | null
}

/** Props for {@link MapAreaPanel}. */
export interface MapAreaPanelProps extends AreaContentProps {
  /** `side`: the desktop column beside the map; `sheet`: the phone bottom sheet. */
  layout: 'side' | 'sheet'
  /** Desktop: whether the column is open. */
  open: boolean
  /** Desktop: open/close the column. */
  onOpenChange: (open: boolean) => void
  /** Phone: the sheet's stop. */
  sheetState: SheetState
  /** Phone: move the sheet to a stop. */
  onSheetStateChange: (state: SheetState) => void
}

/** Geometry handed to the grid's list element through virtuoso's `context`. */
interface GridContext {
  cell: number
}

/** The grid's list element: a CSS grid filling the panel's width. */
const GridList = forwardRef<HTMLDivElement, { style?: CSSProperties; children?: ReactNode }>(
  function GridList({ style, children }, ref) {
    return (
      <div ref={ref} className="kk-map-area-grid" style={style}>
        {children}
      </div>
    )
  },
)

/** The grid's item wrapper: one square cell. */
function GridItem({ children, ...rest }: { children?: ReactNode }) {
  return (
    <div {...rest} className="kk-map-area-cell">
      {children}
    </div>
  )
}

/** The components the panel's VirtuosoGrid renders with. */
const gridComponents: GridComponents<GridContext> = {
  List: GridList,
  Item: GridItem,
}

/** The header line: how many photos, and the way back from a cluster's grid. */
function AreaHeading({
  count,
  mode,
  onBackToArea,
}: {
  count: number
  mode: AreaPanelMode
  onBackToArea: () => void
}) {
  const { t, i18n } = useTranslation()
  const shown = formatCount(count, i18n.language)
  return (
    <div className="kk-map-area-heading">
      <h2 className="kk-map-area-title" aria-live="polite">
        {mode === 'spot'
          ? t('map.area.spotTitle', { count, shown })
          : t('map.area.title', { count, shown })}
      </h2>
      {mode === 'spot' && (
        <Button
          type="button"
          size="sm"
          variant="link"
          className="kk-map-area-back"
          onClick={onBackToArea}
        >
          <Icon name="arrow-left" /> {t('map.area.backToArea')}
        </Button>
      )}
    </div>
  )
}

/**
 * One photo in the panel: a square thumbnail over its blurred placeholder,
 * linking into the viewer with the area's query, so prev/next walk the panel's
 * list. Pointer and focus report it to the map, which marks its tile.
 */
function AreaThumb({
  photo,
  size,
  detailQuery,
  highlighted,
  onHoverPhoto,
}: {
  photo: MapFeature
  size: number
  detailQuery: string
  highlighted: boolean
  onHoverPhoto: (uid: string | null) => void
}) {
  const { t, i18n } = useTranslation()
  const { uid, taken_at: takenAt, blurhash, media_type: mediaType } = photo.properties
  const alt =
    takenAt === undefined || takenAt === ''
      ? t('map.area.photoAltUndated')
      : t('map.area.photoAlt', { date: formatDate(takenAt, i18n.language) })
  const enter = () => {
    onHoverPhoto(uid)
  }
  const leave = () => {
    onHoverPhoto(null)
  }
  return (
    <ProjectLink
      to={`/photos/${encodeURIComponent(uid)}${detailQuery === '' ? '' : `?${detailQuery}`}`}
      className={highlighted ? 'kk-map-area-thumb is-highlighted' : 'kk-map-area-thumb'}
      onMouseEnter={enter}
      onMouseLeave={leave}
      onFocus={enter}
      onBlur={leave}
    >
      <FadeInImage
        src={thumbUrl(uid, squareRenditionName(size, window.devicePixelRatio))}
        alt={alt}
        blurhash={blurhash}
        draggable={false}
      />
      {mediaType === 'video' && (
        <span className="kk-map-area-video">
          <Icon name="play-fill" />
          <span className="visually-hidden">{t('map.area.video')}</span>
        </span>
      )}
    </ProjectLink>
  )
}

/** The empty area: what to do, not a mood. */
function AreaEmpty() {
  const { t } = useTranslation()
  return (
    <div className="kk-map-area-empty">
      <p className="mb-1">{t('map.area.emptyTitle')}</p>
      <p className="small text-secondary mb-0">{t('map.area.emptyHint')}</p>
    </div>
  )
}

/** The whole list as a virtualized grid scrolling inside the panel. */
function AreaGrid({ photos, detailQuery, highlightUid, onHoverPhoto }: AreaContentProps) {
  if (photos.length === 0) {
    return <AreaEmpty />
  }
  return (
    <VirtuosoGrid
      className="kk-map-area-scroller"
      data={photos}
      context={{ cell: GRID_CELL_PX }}
      components={gridComponents}
      increaseViewportBy={{ top: 200, bottom: 400 }}
      computeItemKey={(_index, photo) => photo.properties.uid}
      itemContent={(_index, photo) => (
        <AreaThumb
          photo={photo}
          size={GRID_CELL_PX}
          detailQuery={detailQuery}
          highlighted={photo.properties.uid === highlightUid}
          onHoverPhoto={onHoverPhoto}
        />
      )}
    />
  )
}

/** The peeking sheet's teaser: the newest photos in one sideways row. */
function AreaStrip({ photos, detailQuery, highlightUid, onHoverPhoto }: AreaContentProps) {
  if (photos.length === 0) {
    return <AreaEmpty />
  }
  return (
    <div className="kk-map-area-strip">
      {photos.slice(0, STRIP_LIMIT).map((photo) => (
        <AreaThumb
          key={photo.properties.uid}
          photo={photo}
          size={STRIP_CELL_PX}
          detailQuery={detailQuery}
          highlighted={photo.properties.uid === highlightUid}
          onHoverPhoto={onHoverPhoto}
        />
      ))}
    </div>
  )
}

/**
 * "Photos in this area": the photos inside the map's viewport (or inside one
 * cluster opened as a grid), newest first, as a library grid — tap a photo and
 * the viewer pages through exactly this list.
 *
 * On a desktop it is a collapsible column beside the map; collapsed, a small
 * floating button over the map's edge brings it back and says how many photos
 * are waiting. On a phone it is a {@link BottomSheet} that peeks with the count
 * and a strip of the newest photos and rises to half or the whole map to show
 * the grid.
 */
export function MapAreaPanel(props: MapAreaPanelProps) {
  const { t, i18n } = useTranslation()
  const { layout, open, onOpenChange, sheetState, onSheetStateChange, ...content } = props
  const heading = (
    <AreaHeading
      count={content.photos.length}
      mode={content.mode}
      onBackToArea={content.onBackToArea}
    />
  )

  if (layout === 'sheet') {
    return (
      <BottomSheet
        state={sheetState}
        onStateChange={onSheetStateChange}
        label={t('map.area.label')}
        growLabel={t('map.area.grow')}
        shrinkLabel={t('map.area.shrink')}
        header={heading}
        peek={<AreaStrip {...content} />}
      >
        <AreaGrid {...content} />
      </BottomSheet>
    )
  }

  if (!open) {
    return (
      <Button
        type="button"
        variant="dark"
        className="kk-map-area-reopen"
        aria-expanded={false}
        onClick={() => {
          onOpenChange(true)
        }}
      >
        <Icon name="images" /> {t('map.area.expand')}
        <span className="kk-map-area-reopen-count">
          {formatCount(content.photos.length, i18n.language)}
        </span>
      </Button>
    )
  }

  return (
    <aside className="kk-map-area" aria-label={t('map.area.label')}>
      <div className="kk-map-area-top">
        {heading}
        <Button
          type="button"
          variant="link"
          className="kk-map-area-collapse"
          aria-label={t('map.area.collapse')}
          aria-expanded
          onClick={() => {
            onOpenChange(false)
          }}
        >
          <Icon name="chevron-right" />
        </Button>
      </div>
      <AreaGrid {...content} />
    </aside>
  )
}
