import type { MultiSelectOption } from '../components/MultiSelect'

/**
 * An album or label picked via a {@link MultiSelect} create entry is held in the
 * selection as its name behind this prefix until the surrounding action actually
 * creates it — real UIDs are short base32 strings and never carry a colon, so a
 * pending marker and a real UID can never collide. Deferring creation to the
 * moment the batch runs means abandoning the form never leaves an empty album or
 * label behind: the consumer first creates the pending entries, swaps their fresh
 * UIDs in, and only then submits.
 *
 * Shared by the grid-selection bulk edit (`BulkEditModal`) and the upload page's
 * album/label picker (`useUploadOrganize`), so both offer identical inline-create
 * behaviour.
 */
const CREATE_PREFIX = 'create:'

/** Encodes a not-yet-existing entry name as a multi-select value. */
export function pendingValue(name: string): string {
  return CREATE_PREFIX + name
}

/** Decodes a pending-creation value back to its name; null for a real UID. */
export function pendingName(value: string): string | null {
  return value.startsWith(CREATE_PREFIX) ? value.slice(CREATE_PREFIX.length) : null
}

/**
 * Synthetic options for the pending creations in `selected`, so their chips read
 * as the typed name rather than the raw `create:` marker.
 */
export function pendingOptions(selected: string[]): MultiSelectOption[] {
  return selected.flatMap((value) => {
    const name = pendingName(value)
    return name === null ? [] : [{ value, label: name }]
  })
}
