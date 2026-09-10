import { type PhotoDetail, type PhotoProcessing } from '../services/photos'

/**
 * How long the viewer waits between two re-checks of a clip whose streaming
 * encode is still owed. Long enough that an open viewer costs the server one
 * cheap detail request a quarter of a minute, short enough that a rendition
 * lands on screen while the reader is still looking at the clip.
 */
export const ENCODE_POLL_INTERVAL_MS = 15000

/**
 * The streaming encode's row of a photo's processing report, or `null` when the
 * detail carries no report at all (an instance that wires no processing service,
 * an older fixture). "Cannot tell" is deliberately not "not started": everything
 * reading this treats `null` as the state before streaming existed — playback
 * behaves exactly as it always did and nothing extra is polled.
 */
export function videoEncode(photo: PhotoDetail): PhotoProcessing | null {
  return photo.processing?.find((row) => row.step === 'hls_transcode') ?? null
}

/**
 * Whether the encode is on its way — a worker holds it (`running`) or the queue
 * does (`queued`). These are the only two states a rendition can appear out of
 * on its own, and so the only two worth waiting for: `pending` means nothing has
 * been scheduled, and `done`/`failed`/`skipped` are as final as they read.
 */
export function encodeInProgress(encode: PhotoProcessing | null): boolean {
  return encode !== null && (encode.state === 'queued' || encode.state === 'running')
}

/**
 * Whether the open item is worth re-checking for a streaming rendition: it is a
 * video, it has no rendition yet, and its encode is on its way. Everything else
 * — every still photo, every clip already encoded, and every instance with
 * streaming switched off (the step reads `skipped` there) — answers `false`, so
 * the viewer generates no extra traffic whatsoever.
 */
export function shouldWatchEncode(photo: PhotoDetail): boolean {
  return photo.media_type === 'video' && photo.hls !== true && encodeInProgress(videoEncode(photo))
}

/**
 * Which explanation the player puts in place of itself once the browser has
 * refused the clip. The encode's state is what separates a dead end from a wait:
 * an encode still owed means this very clip will be playable in a few minutes,
 * which is the opposite of the "download it instead" the player said before.
 *
 * `skipped` and `done` — streaming switched off instance-wide, or a clip that
 * was encoded and still cannot be decoded — keep the original message, and so
 * does a photo whose report says nothing about the step.
 */
export type VideoFallback = 'preparing' | 'preparingNow' | 'preparationFailed' | 'unsupported'

/** The {@link VideoFallback} for an encode row; see the type for the reasoning. */
export function videoFallback(encode: PhotoProcessing | null): VideoFallback {
  switch (encode?.state) {
    case 'queued':
    case 'pending':
      return 'preparing'
    case 'running':
      return 'preparingNow'
    case 'failed':
      return 'preparationFailed'
    default:
      return 'unsupported'
  }
}
