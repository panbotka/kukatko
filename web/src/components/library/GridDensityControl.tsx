import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import {
  GRID_COLUMNS_MIN,
  type GridDensityScope,
  LIBRARY_GRID_SCOPE,
  stepDensity,
} from '../../lib/gridDensity'
import { Icon } from '../Icon'

/** Props for {@link GridDensityControl}. */
export interface GridDensityControlProps {
  /**
   * Which grid this stepper drives, and therefore which stored count it moves.
   * Defaults to the photo library — pass `REVIEW_GRID_SCOPE` on the review
   * tools, which share one number of their own.
   */
  scope?: GridDensityScope
}

/**
 * Picks how many tiles sit side by side in a grid, as a compact zoom stepper:
 * `−` pins fewer, larger tiles down to one per row (where it disables), `+` pins
 * more columns (smaller tiles) up to the maximum (where it disables). The middle
 * chip is a read-only display of the current column count — there is no "auto"
 * mode any more, so nothing to reset to. The preference lives in localStorage, so
 * it is per device and survives a reload, and it is deliberately not URL state —
 * see `hooks/useGridDensity`.
 *
 * The control is shared across grids but the *number* is not: each `scope` has
 * its own key, so the library and the `/outliers` review grid never fight over
 * one density.
 *
 * The readout and the buttons follow the count **in effect**, not the stored
 * one: on a narrow viewport the grid caps its columns (`GRID_COLUMN_CAPS`), so
 * `+` disables at that cap rather than offering a step the screen would refuse.
 * The user's wider-screen choice is untouched and comes back with the window.
 */
export function GridDensityControl({ scope = LIBRARY_GRID_SCOPE }: GridDensityControlProps = {}) {
  const { t } = useTranslation()
  const { density, setDensity, maxColumns } = useGridDensity(scope)

  return (
    <ButtonGroup size="lg" className="kukatko-grid-density" aria-label={t('library.density.label')}>
      <Button
        type="button"
        variant="outline-secondary"
        disabled={density <= GRID_COLUMNS_MIN}
        aria-label={t('library.density.fewer')}
        // Only ever seen at the steps where the button still works: a natively
        // disabled button takes no pointer events, so at the floor the tooltip
        // stays away — which is the same story the greying already tells.
        title={t('library.density.fewer')}
        onClick={() => {
          setDensity(stepDensity(density, -1, scope))
        }}
      >
        <Icon name="dash-lg" />
      </Button>

      {/* A read-only readout of the current count, styled to sit in the group.
          `pointer-events: none` keeps it inert — it is not a button. */}
      <span
        className="btn btn-secondary kukatko-grid-density-value"
        style={{ pointerEvents: 'none' }}
        title={t('library.density.columns', { n: density })}
      >
        <Icon name="grid-3x3-gap-fill" />
        <span className="ms-1">{density}</span>
      </span>

      <Button
        type="button"
        variant="outline-secondary"
        disabled={density >= maxColumns}
        aria-label={t('library.density.more')}
        title={t('library.density.more')}
        onClick={() => {
          setDensity(stepDensity(density, 1, scope))
        }}
      >
        <Icon name="plus-lg" />
      </Button>
    </ButtonGroup>
  )
}
