import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_COLUMNS_MAX, GRID_COLUMNS_MIN, stepDensity } from '../../lib/gridDensity'
import { Icon } from '../Icon'

/**
 * Picks how many photos sit side by side in the grid, as a compact zoom stepper:
 * `−` pins fewer, larger tiles down to one photo per row (where it disables), `+`
 * pins more columns (smaller tiles) up to the maximum (where it disables). The
 * middle chip is a read-only display of the current column count — there is no
 * "auto" mode any more, so nothing to reset to. The preference lives in
 * localStorage, so it is per device and survives a reload, and it is deliberately
 * not URL state — see `hooks/useGridDensity`.
 */
export function GridDensityControl() {
  const { t } = useTranslation()
  const { density, setDensity } = useGridDensity()

  return (
    <ButtonGroup size="lg" className="kukatko-grid-density" aria-label={t('library.density.label')}>
      <Button
        type="button"
        variant="outline-secondary"
        disabled={density <= GRID_COLUMNS_MIN}
        aria-label={t('library.density.fewer')}
        onClick={() => {
          setDensity(stepDensity(density, -1))
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
        disabled={density >= GRID_COLUMNS_MAX}
        aria-label={t('library.density.more')}
        onClick={() => {
          setDensity(stepDensity(density, 1))
        }}
      >
        <Icon name="plus-lg" />
      </Button>
    </ButtonGroup>
  )
}
