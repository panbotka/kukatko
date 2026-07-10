/** Props for {@link MetaField}. */
export interface MetaFieldProps {
  /** The (already translated) field label. */
  label: string
  /** The formatted value; an empty or absent value omits the row entirely. */
  value: string | undefined
}

/**
 * One read-only labelled value row of a photo's metadata. Rendering nothing for
 * an empty value keeps the panels free of blank "Lens: —" noise, so a photo
 * without EXIF simply shows fewer rows.
 */
export function MetaField({ label, value }: MetaFieldProps) {
  if (value === undefined || value === '') {
    return null
  }
  return (
    <div className="mb-2">
      <div className="small text-secondary">{label}</div>
      <div>{value}</div>
    </div>
  )
}
