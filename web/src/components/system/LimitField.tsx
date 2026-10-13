import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import { LIMIT_UNITS, type LimitDraft, type LimitUnit } from '../../lib/byteLimit'

/** Props of {@link LimitField}. */
export interface LimitFieldProps {
  /** The id of the amount input, which the label points at. */
  id: string
  /** The already-translated label. */
  label: string
  value: LimitDraft
  onChange: (value: LimitDraft) => void
  /** Marks the amount invalid and shows the refusal under it. */
  invalid: boolean
}

/**
 * One project limit — an amount and a MB/GB unit, empty for no limit — as the
 * new-project dialog and the project detail edit it. Reading the draft into
 * bytes is `parseLimit`'s job; this only draws it.
 */
export function LimitField({ id, label, value, onChange, invalid }: LimitFieldProps) {
  const { t } = useTranslation()

  return (
    <Form.Group className="mb-3">
      <Form.Label htmlFor={id}>{label}</Form.Label>
      <InputGroup hasValidation>
        <Form.Control
          id={id}
          inputMode="decimal"
          value={value.amount}
          isInvalid={invalid}
          aria-describedby={`${id}-hint`}
          onChange={(event) => {
            onChange({ ...value, amount: event.currentTarget.value })
          }}
        />
        <Form.Select
          aria-label={t('systemProjects.form.unitLabel')}
          className="flex-grow-0 w-auto"
          value={value.unit}
          onChange={(event) => {
            onChange({ ...value, unit: event.currentTarget.value as LimitUnit })
          }}
        >
          {LIMIT_UNITS.map((unit) => (
            <option key={unit} value={unit}>
              {unit}
            </option>
          ))}
        </Form.Select>
        {invalid && (
          <Form.Control.Feedback type="invalid">
            {t('systemProjects.errors.limitInvalid')}
          </Form.Control.Feedback>
        )}
      </InputGroup>
      <Form.Text id={`${id}-hint`}>{t('systemProjects.form.limitHint')}</Form.Text>
    </Form.Group>
  )
}
