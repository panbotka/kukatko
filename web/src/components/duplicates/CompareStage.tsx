import { useTranslation } from 'react-i18next'

import { type SyncZoom } from '../../hooks/useSyncZoom'
import { viewTransform } from '../../lib/compareZoom'

import './compare.css'

/** One side of the stage. */
export interface ComparePane {
  uid: string
  /** The image to show, already resolved to a URL. */
  src: string
  /** Accessible name for the image — the photo's title or file name. */
  alt: string
  /** Heading above the pane ("Left" / "Right", plus the keeper marker). */
  caption: string
  /** Whether this side is the keeper the detector suggested. */
  isKeeper: boolean
}

/** Props for {@link CompareStage}. */
export interface CompareStageProps {
  left: ComparePane
  right: ComparePane
  /** The shared zoom driving both panes. */
  zoom: SyncZoom
}

/**
 * The two photos side by side, as large as the viewport allows, sharing one zoom.
 *
 * Both panes render `zoom.view`, so a wheel/drag on either moves both — the point
 * being that a JPEG re-encode only reveals itself against its original at the same
 * magnification. On a narrow screen the panes stack (the CSS does it), which keeps
 * each image usably large at the cost of them not being literally side by side; the
 * synchronised zoom is what carries the comparison there.
 */
export function CompareStage({ left, right, zoom }: CompareStageProps) {
  return (
    <div className="kk-compare-stage">
      <ComparePaneView pane={left} zoom={zoom} />
      <ComparePaneView pane={right} zoom={zoom} />
    </div>
  )
}

/**
 * The pane's cursor, advertising what a gesture will do: grab/grabbing while
 * magnified (a drag pans), zoom-in at rest (a wheel or double-click magnifies).
 */
function paneCursor(zoom: SyncZoom): string {
  if (!zoom.zoomed) {
    return 'zoom-in'
  }
  return zoom.dragging ? 'grabbing' : 'grab'
}

/** One pane: its caption and the image, transformed by the shared zoom. */
function ComparePaneView({ pane, zoom }: { pane: ComparePane; zoom: SyncZoom }) {
  const { t } = useTranslation()
  return (
    <figure className="kk-compare-pane">
      <figcaption className="kk-compare-pane__caption">
        <span>{pane.caption}</span>
        {pane.isKeeper && (
          <span className="badge bg-info text-dark ms-2">{t('duplicates.compare.suggested')}</span>
        )}
      </figcaption>
      <div
        className="kk-compare-pane__viewport"
        data-testid={`compare-pane-${pane.uid}`}
        {...zoom.handlers}
        style={{ cursor: paneCursor(zoom) }}
      >
        <img
          className="kk-compare-pane__image"
          src={pane.src}
          alt={pane.alt}
          draggable={false}
          style={{
            transform: viewTransform(zoom.view),
            // A transition would lag a drag behind the cursor; it only helps the
            // discrete wheel/button steps.
            transition: zoom.dragging ? 'none' : 'transform 0.12s ease-out',
          }}
        />
      </div>
    </figure>
  )
}
