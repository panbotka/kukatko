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
 * Shared by the batch action bar's album/label pickers (`BatchActionBar`), the
 * grid-selection bulk edit (`BulkEditModal`) and the upload page's album/label
 * picker (`useUploadOrganize`), so all three offer identical inline-create
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

/** Whether `values` still holds a name that has to be created before it is usable. */
export function hasPending(values: string[]): boolean {
  return values.some((value) => pendingName(value) !== null)
}

/**
 * Outcome of {@link resolvePending}. Both branches carry `values`: on a failure
 * it is the list with the entries created *so far* already swapped in, so the
 * caller can write it back and a retry never creates the same album twice.
 */
export type PendingResolution =
  | { status: 'resolved'; values: string[] }
  | {
      status: 'failed'
      values: string[]
      /** The name whose creation failed. */
      name: string
      /** Whatever `create` rejected with — typically an `ApiError`. */
      error: unknown
    }

/**
 * Turns every `create:` marker in `values` into a real entry: each pending name
 * is handed to `create`, which is expected to create it and answer with its
 * fresh UID, and that UID replaces the marker. Real UIDs pass through untouched,
 * so a list with nothing pending resolves without a single request.
 *
 * Creation runs one at a time and stops at the first failure — the server names
 * the problem (a duplicate title, permission denied) and repeating it for every
 * remaining name would only bury it.
 */
export async function resolvePending(
  values: string[],
  create: (name: string) => Promise<string>,
): Promise<PendingResolution> {
  let resolved = values
  for (const value of values) {
    const name = pendingName(value)
    if (name === null) {
      continue
    }
    try {
      const uid = await create(name)
      resolved = resolved.map((current) => (current === value ? uid : current))
    } catch (error: unknown) {
      return { status: 'failed', values: resolved, name, error }
    }
  }
  return { status: 'resolved', values: resolved }
}
