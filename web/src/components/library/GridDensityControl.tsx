import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_COLUMN_CHOICES, GRID_DENSITY_DEFAULT } from '../../lib/gridDensity'

/** DOM id of the density select, tying it to its (visually hidden) label. */
const SELECT_ID = 'grid-density'

/**
 * Picks how many photos sit side by side in the grid. `Auto` keeps the
 * width-driven default; any other choice pins the column count, which every
 * photo grid in the app then honours. The preference lives in localStorage, so
 * it is per device and survives a reload — see `hooks/useGridDensity`.
 */
export function GridDensityControl() {
  const { t } = useTranslation()
  const { density, setDensity } = useGridDensity()

  return (
    <InputGroup className="w-auto">
      <InputGroup.Text aria-hidden="true">
        <GridIcon />
      </InputGroup.Text>
      <Form.Label htmlFor={SELECT_ID} className="visually-hidden">
        {t('library.density.label')}
      </Form.Label>
      <Form.Select
        id={SELECT_ID}
        className="kukatko-grid-density w-auto"
        size="lg"
        value={String(density)}
        title={t('library.density.label')}
        onChange={(e) => {
          const raw = e.target.value
          setDensity(raw === 'auto' ? GRID_DENSITY_DEFAULT : Number(raw))
        }}
      >
        <option value="auto">{t('library.density.auto')}</option>
        {GRID_COLUMN_CHOICES.map((n) => (
          <option key={n} value={String(n)}>
            {t('library.density.columns', { n })}
          </option>
        ))}
      </Form.Select>
    </InputGroup>
  )
}

/** A 3×3 tile glyph (Bootstrap Icons "grid-3x3-gap-fill") marking the density picker. */
function GridIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      fill="currentColor"
      viewBox="0 0 16 16"
      aria-hidden="true"
    >
      <path d="M1 2a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1zM1 7a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1zM1 12a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1z" />
    </svg>
  )
}
