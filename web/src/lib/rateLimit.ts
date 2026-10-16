import { ApiError } from '../services/auth'

/**
 * How a page waits out a `429` from the server's rate limiters — the heavy
 * vector throttle in particular, which guards the five routes that scan a whole
 * project (duplicates, the review queue, the recognition sweep, the candidate
 * search, the collection expansion). Pure, so the schedule is unit-testable;
 * {@link import('../hooks/useRateLimitRetry').useRateLimitRetry} runs it.
 *
 * The server says `Retry-After: 1`, but that is only "not this second": the
 * heavy bucket refills one token every ten seconds, so retrying every second
 * would just collect nine more refusals. The wait therefore starts at
 * {@link RATE_LIMIT_FIRST_DELAY_MS}, doubles with every refusal in a row, never
 * drops below what the server asked and never exceeds
 * {@link RATE_LIMIT_MAX_DELAY_MS}; after {@link RATE_LIMIT_MAX_RETRIES}
 * refusals the page gives up and shows its ordinary failure state.
 */

/** The first wait after a 429, in milliseconds. */
export const RATE_LIMIT_FIRST_DELAY_MS = 2000

/** The longest single wait, in milliseconds, however many refusals came before. */
export const RATE_LIMIT_MAX_DELAY_MS = 30_000

/**
 * How many automatic retries a page makes before giving up. With the delays
 * above that is 2 + 4 + 8 + 16 + 30 + 30 s ≈ 1.5 min of waiting — several
 * refills of the heavy bucket, so only a real overload exhausts it.
 */
export const RATE_LIMIT_MAX_RETRIES = 6

/** Whether `err` is the server's "rate limit exceeded" (HTTP 429). */
export function isRateLimited(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 429
}

/**
 * The wait before automatic retry number `attempt` (0 for the first) after the
 * 429 `err`: exponential from {@link RATE_LIMIT_FIRST_DELAY_MS}, at least the
 * server's `Retry-After`, at most {@link RATE_LIMIT_MAX_DELAY_MS}.
 */
export function rateLimitDelayMs(err: ApiError, attempt: number): number {
  const backoff = RATE_LIMIT_FIRST_DELAY_MS * 2 ** Math.max(0, attempt)
  const asked = (err.retryAfterSeconds ?? 0) * 1000
  return Math.min(RATE_LIMIT_MAX_DELAY_MS, Math.max(backoff, asked))
}
