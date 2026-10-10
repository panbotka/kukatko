/**
 * The device-level key remembering that a batch was running on the public
 * upload-link page, so a reload — which loses the picked files — can say what
 * happened. Per device by design, like the uploader name: there is no account to
 * scope it to (see "Browser storage" in docs/FRONTEND.md).
 */
export const UPLOAD_BATCH_KEY = 'kukatko.uploadLink.batch'

/** A remembered batch older than this is no longer worth mentioning. */
export const UPLOAD_BATCH_MAX_AGE_MS = 3 * 24 * 60 * 60 * 1000

/** What is remembered about a running batch. */
export interface RememberedBatch {
  /** The link the batch went through; another link's batch is not mentioned. */
  code: string
  /** How many files the batch held. */
  count: number
  /** When the batch started, in ms since the epoch. */
  startedAt: number
}

/** Parses a stored value, `null` for anything that is not a remembered batch. */
function parse(raw: string | null): RememberedBatch | null {
  if (raw === null) {
    return null
  }
  try {
    const value: unknown = JSON.parse(raw)
    if (value === null || typeof value !== 'object') {
      return null
    }
    const { code, count, startedAt } = value as Partial<Record<keyof RememberedBatch, unknown>>
    if (typeof code !== 'string' || typeof count !== 'number' || typeof startedAt !== 'number') {
      return null
    }
    return { code, count, startedAt }
  } catch {
    return null
  }
}

/** Reads the stored value; storage may be unavailable (private mode). */
function read(): RememberedBatch | null {
  try {
    return parse(localStorage.getItem(UPLOAD_BATCH_KEY))
  } catch {
    return null
  }
}

/**
 * The batch a previous visit to the link `code` left unfinished, or `null` —
 * none, another link's, too old (see {@link UPLOAD_BATCH_MAX_AGE_MS}), or
 * unreadable storage.
 */
export function readInterruptedBatch(code: string, now: number): RememberedBatch | null {
  const batch = read()
  if (batch?.code !== code || batch.count <= 0) {
    return null
  }
  if (now - batch.startedAt > UPLOAD_BATCH_MAX_AGE_MS || batch.startedAt > now) {
    return null
  }
  return batch
}

/**
 * Records that a batch of `count` files is running through `code`, keeping the
 * start of a batch already recorded for the same link.
 */
export function rememberBatch(code: string, count: number, now: number): void {
  const previous = read()
  const startedAt = previous?.code === code ? previous.startedAt : now
  try {
    localStorage.setItem(UPLOAD_BATCH_KEY, JSON.stringify({ code, count, startedAt }))
  } catch {
    // Storage refused: a reload simply will not be explained.
  }
}

/** Forgets the running batch: it finished, so there is nothing to explain. */
export function forgetBatch(): void {
  try {
    localStorage.removeItem(UPLOAD_BATCH_KEY)
  } catch {
    // Storage refused: nothing was remembered either.
  }
}
