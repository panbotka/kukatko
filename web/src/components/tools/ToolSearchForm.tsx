import { type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import Col, { type ColProps } from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'

/** The breakpoint widths a tool field may take from `md` up (phones always stack). */
export type ToolFieldWidth = Pick<ColProps, 'md' | 'lg'>

/** Props for {@link ToolSearchForm}. */
export interface ToolSearchFormProps {
  /** Runs the tool's search; the native submit is already prevented. */
  onSubmit: () => void
  /** The fields, in reading order, ending with a {@link ToolSubmit}. */
  children: ReactNode
}

/**
 * ToolSearchForm is the one layout behind the config panels of the person,
 * expand and recognition tools, so the three cannot drift apart. Every field
 * is a label row, a control exactly one tap target tall, then its helper text;
 * the `kk-tool-form` rules give each label row the same height, so labels line
 * up at the top and controls at the bottom on desktop. Below `md` every column
 * is full-width, so the fields stack in DOM order with the submit button last.
 */
export function ToolSearchForm({ onSubmit, children }: ToolSearchFormProps) {
  return (
    <Form
      className="mb-4"
      onSubmit={(event) => {
        event.preventDefault()
        onSubmit()
      }}
    >
      <Row className="g-3 kk-tool-form">{children}</Row>
    </Form>
  )
}

/** Props for {@link ToolField}. */
export interface ToolFieldProps extends ToolFieldWidth {
  /** The field: a label row, the control, then its helper text. */
  children: ReactNode
}

/**
 * ToolField is one column of a {@link ToolSearchForm} for a control the shared
 * fields do not cover (a picker that brings its own label). Full-width on
 * phones; `md`/`lg` size it from there up.
 */
export function ToolField({ md, lg, children }: ToolFieldProps) {
  return (
    <Col xs={12} md={md} lg={lg}>
      {children}
    </Col>
  )
}

/** Props for {@link ToolRangeField}. */
export interface ToolRangeFieldProps extends ToolFieldWidth {
  /** Ties the label to the slider. */
  id: string
  /** The field label, left in the label row. */
  label: string
  /** The current value as read out, right in the label row (e.g. "75 %"). */
  valueText: string
  min: number
  max: number
  step: number
  value: number
  disabled?: boolean
  /** Called with the slider's new numeric value. */
  onChange: (value: number) => void
  /** The low end's trade-off ("more results"), under the slider's left end. */
  lowHint: string
  /** The high end's trade-off ("better matches"), under its right end. */
  highHint: string
}

/**
 * ToolRangeField is the percent slider every tool carries: the label and the
 * live value share the label row, the slider is centred in a control-tall slot
 * (so its track sits level with the neighbouring inputs, not above them), and
 * the trade-off bookends are its helper text.
 */
export function ToolRangeField({
  id,
  label,
  valueText,
  min,
  max,
  step,
  value,
  disabled,
  onChange,
  lowHint,
  highHint,
  md,
  lg,
}: ToolRangeFieldProps) {
  return (
    <Col xs={12} md={md} lg={lg}>
      <Form.Label htmlFor={id} className="d-flex justify-content-between">
        <span>{label}</span>
        <span className="fw-semibold">{valueText}</span>
      </Form.Label>
      <div className="kk-tool-range">
        <Form.Range
          id={id}
          min={min}
          max={max}
          step={step}
          value={value}
          disabled={disabled}
          onChange={(event) => {
            onChange(Number(event.target.value))
          }}
        />
      </div>
      <div className="d-flex justify-content-between small text-secondary">
        <span>{lowHint}</span>
        <span>{highHint}</span>
      </div>
    </Col>
  )
}

/** Props for {@link ToolLimitField}. */
export interface ToolLimitFieldProps extends ToolFieldWidth {
  /** Ties the label to the input. */
  id: string
  label: string
  /** What the number means ("0 = no limit"), under the input. */
  hint: string
  value: number
  min: number
  max?: number
  disabled?: boolean
  /** Called with the raw numeric value; the caller clamps it. */
  onChange: (value: number) => void
}

/** ToolLimitField is the result-cap input every tool carries, one tap target tall. */
export function ToolLimitField({
  id,
  label,
  hint,
  value,
  min,
  max,
  disabled,
  onChange,
  md,
  lg,
}: ToolLimitFieldProps) {
  return (
    <Col xs={12} md={md} lg={lg}>
      <Form.Label htmlFor={id}>{label}</Form.Label>
      <Form.Control
        id={id}
        type="number"
        className="kukatko-tap-target"
        min={min}
        max={max}
        value={value}
        disabled={disabled}
        onChange={(event) => {
          onChange(Number(event.target.value))
        }}
      />
      <Form.Text>{hint}</Form.Text>
    </Col>
  )
}

/** Props for {@link ToolSubmit}. */
export interface ToolSubmitProps {
  /** `submit` runs the form; `button` (with `onClick`) does something else, e.g. cancel. */
  type?: 'submit' | 'button'
  variant?: string
  disabled?: boolean
  onClick?: () => void
  /** Icon and label. */
  children: ReactNode
}

/**
 * ToolSubmit is the form's last column. From `md` up it sizes to its button and
 * an empty label row above the button puts it level with the controls; on a
 * phone it is the full-width last row, under every helper text.
 */
export function ToolSubmit({
  type = 'submit',
  variant = 'primary',
  disabled,
  onClick,
  children,
}: ToolSubmitProps) {
  return (
    <Col xs={12} md="auto">
      <span className="form-label d-none d-md-block" aria-hidden="true">
        {' '}
      </span>
      <Button
        type={type}
        variant={variant}
        disabled={disabled}
        onClick={onClick}
        className="w-100 kukatko-tap-target d-flex align-items-center justify-content-center gap-2"
      >
        {children}
      </Button>
    </Col>
  )
}
