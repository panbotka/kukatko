import { type ReactNode } from 'react'

/** Props for {@link MetaField}. */
export interface MetaFieldProps {
  /** The (already translated) field label. */
  label: string
  /** The formatted value; an empty or absent value omits the row entirely. */
  value?: string
  /**
   * Native tooltip on the value — where the row shows a shortened form: the exact
   * byte count behind a rounded size, the full SHA256 behind a truncated hash.
   */
  title?: string
  /**
   * A rich value (chips, a badge, a copy button) rendered instead of `value`. A
   * row with children is always rendered, so the caller — not this component —
   * decides whether the photo has anything to say here.
   */
  children?: ReactNode
}

/**
 * One read-only labelled value of a photo's metadata, as a row of the definition
 * list its group renders: label and value sit side by side on a wide viewport and
 * stack into one column on a narrow one. Rendering nothing for an empty value
 * keeps the panels free of blank "Lens: —" noise, so a photo without EXIF simply
 * shows fewer rows.
 *
 * Values wrap rather than push the page sideways — a hash, an ICC profile name or
 * a path is long, and horizontal scrolling on the detail page is never the answer.
 */
export function MetaField({ label, value, title, children }: MetaFieldProps) {
  if (children === undefined && (value === undefined || value === '')) {
    return null
  }
  return (
    <>
      <dt className="col-sm-5 small text-secondary fw-normal">{label}</dt>
      <dd className="col-sm-7 mb-2 text-break" title={title}>
        {children ?? value}
      </dd>
    </>
  )
}
