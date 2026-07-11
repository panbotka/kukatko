import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_COLUMNS_MAX, GRID_DENSITY_DEFAULT, stepDensity } from '../../lib/gridDensity'
import { Icon } from '../Icon'

/**
 * Picks how many photos sit side by side in the grid, as a compact zoom stepper:
 * `−` steps back toward the responsive `Auto` default (fewer, larger tiles), `+`
 * pins more columns (smaller tiles). The middle chip shows the current setting —
 * `A` for auto, otherwise the column count — and clicking it resets to `Auto`.
 * The preference lives in localStorage, so it is per device and survives a
 * reload, and it is deliberately not URL state — see `hooks/useGridDensity`.
 */
export function GridDensityControl() {
  const { t } = useTranslation()
  const { density, setDensity } = useGridDensity()

  const isAuto = density === 'auto'

  return (
    <ButtonGroup size="lg" className="kukatko-grid-density" aria-label={t('library.density.label')}>
      <Button
        type="button"
        variant="outline-secondary"
        disabled={isAuto}
        aria-label={t('library.density.fewer')}
        onClick={() => {
          setDensity(stepDensity(density, -1))
        }}
      >
        <Icon name="dash-lg" />
      </Button>

      <Button
        type="button"
        variant={isAuto ? 'outline-secondary' : 'secondary'}
        className="kukatko-grid-density-value"
        disabled={isAuto}
        aria-label={isAuto ? t('library.density.auto') : t('library.density.reset', { n: density })}
        title={isAuto ? t('library.density.label') : t('library.density.reset', { n: density })}
        onClick={() => {
          setDensity(GRID_DENSITY_DEFAULT)
        }}
      >
        <Icon name="grid-3x3-gap-fill" />
        <span aria-hidden="true" className="ms-1">
          {isAuto ? t('library.density.autoShort') : density}
        </span>
      </Button>

      <Button
        type="button"
        variant="outline-secondary"
        disabled={density === GRID_COLUMNS_MAX}
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
