/**
 * The time a batch has left, estimated from the throughput measured so far.
 *
 * Pure on purpose: the page samples the queue once a second and hands the bytes
 * here, so every rule about what an estimate may say — when it is too early to
 * say anything, how it is smoothed so it never jumps around, how it is rounded
 * for a person rather than a stopwatch — is unit-tested without timers or React.
 */

/** The part of a queued file the byte count reads (structural, like `lib/uploadErrors`). */
interface SizedUpload {
  file: { size: number }
  status: string
  progress: number
  interrupted?: boolean
}

/** How far a batch has got, in bytes. */
export interface BatchBytes {
  /** Bytes already sent (a settled file counts whole, a running one by its progress). */
  done: number
  /** Bytes the whole batch holds. */
  total: number
}

/**
 * The batch's bytes sent and bytes overall. A created, duplicate or failed file
 * counts as sent in full (it needs nothing more), a running one by its upload
 * progress, a waiting one not at all — and neither does a file whose request was
 * interrupted, because it is going to be sent again from its first byte.
 */
export function batchBytes(items: readonly SizedUpload[]): BatchBytes {
  let done = 0
  let total = 0
  for (const item of items) {
    const size = item.file.size
    total += size
    if (item.status === 'uploading') {
      done += size * item.progress
    } else if (
      item.status === 'created' ||
      item.status === 'duplicate' ||
      (item.status === 'error' && item.interrupted !== true)
    ) {
      done += size
    }
  }
  return { done, total }
}

/** The fraction of the batch's bytes sent, in `[0, 1]`; zero for an empty batch. */
export function bytesFraction({ done, total }: BatchBytes): number {
  return total > 0 ? Math.min(1, done / total) : 0
}

/** How long a stretch of measurements must be before a rate is believed. */
export const ETA_MIN_DATA_MS = 4_000
/** The sliding window the raw rate is measured over. */
export const ETA_WINDOW_MS = 20_000
/**
 * Weight of a fresh window rate in the smoothed one (an exponential moving
 * average): low enough that a single slow or fast second barely moves the
 * estimate, high enough that a real change in speed shows within half a minute.
 */
export const ETA_SMOOTHING = 0.2

/** One measurement: the bytes sent at a moment. */
interface EtaSample {
  at: number
  bytes: number
}

/** The estimator's running state; start from {@link INITIAL_THROUGHPUT}. */
export interface ThroughputState {
  /** The measurements inside the window, oldest first. */
  samples: readonly EtaSample[]
  /** The smoothed rate in bytes per second, `null` until there is enough data. */
  rate: number | null
}

/** The state before the first measurement. */
export const INITIAL_THROUGHPUT: ThroughputState = { samples: [], rate: null }

/**
 * Folds one measurement (`bytes` sent by the moment `at`, in ms) into the state.
 *
 * The raw rate is the slope across the sliding window; it is believed only once
 * the window spans {@link ETA_MIN_DATA_MS}, and then blended into the previous
 * rate rather than replacing it. A count that went *down* (a file removed, or one
 * re-sent from zero) restarts the window but keeps the smoothed rate, so an
 * estimate already on screen does not vanish over it.
 */
export function sampleThroughput(
  state: ThroughputState,
  at: number,
  bytes: number,
): ThroughputState {
  const last = state.samples.at(-1)
  if (last !== undefined && (bytes < last.bytes || at < last.at)) {
    return { samples: [{ at, bytes }], rate: state.rate }
  }
  const samples = [...state.samples, { at, bytes }].filter(
    (sample) => sample.at >= at - ETA_WINDOW_MS,
  )
  const first = samples[0]
  const span = at - first.at
  if (span < ETA_MIN_DATA_MS) {
    return { samples, rate: state.rate }
  }
  const windowRate = ((bytes - first.bytes) / span) * 1000
  const rate =
    state.rate === null ? windowRate : state.rate + ETA_SMOOTHING * (windowRate - state.rate)
  return { samples, rate }
}

/**
 * The seconds left for `remainingBytes` at the smoothed rate, or `null` when
 * there is no honest answer yet (no rate, or nothing moving).
 */
export function remainingSeconds(state: ThroughputState, remainingBytes: number): number | null {
  if (state.rate === null || state.rate <= 0) {
    return null
  }
  return Math.max(0, remainingBytes) / state.rate
}

/** A remaining time as a person would say it. */
export type EtaPhrase =
  | { unit: 'lessThanMinute' }
  | { unit: 'minutes'; count: number }
  | { unit: 'hours'; count: number }

/**
 * Rounds `seconds` to what is worth saying: "under a minute", whole minutes up
 * to ten, then steps of five minutes up to an hour and a half, then whole hours.
 * The coarser the figure, the coarser the step, so a slowly drifting estimate
 * changes its wording rarely instead of every second.
 */
export function etaPhrase(seconds: number): EtaPhrase {
  if (seconds < 60) {
    return { unit: 'lessThanMinute' }
  }
  const minutes = seconds / 60
  if (minutes < 10) {
    return { unit: 'minutes', count: Math.max(1, Math.round(minutes)) }
  }
  if (minutes < 90) {
    return { unit: 'minutes', count: Math.max(10, Math.round(minutes / 5) * 5) }
  }
  return { unit: 'hours', count: Math.max(2, Math.round(minutes / 60)) }
}
