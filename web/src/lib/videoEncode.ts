import { type Photo, type PhotoDetail, type PhotoProcessing } from '../services/photos'

import { isVideo } from './mediaKind'

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
 * Which explanation the player puts in place of itself. The encode's state is
 * what separates a dead end from a wait: an encode still owed means this very
 * clip will be playable in a few minutes, which is the opposite of the
 * "download it instead" the player said before.
 *
 * It is read twice, for the two ways a clip ends up unplayed. Before playback
 * (see {@link videoHold}) it is the reason the player was never offered; after
 * the browser has refused a clip it was offered, it is what that refusal means
 * for this clip. `skipped` and `done` — streaming switched off instance-wide, or
 * a clip that was encoded and still cannot be decoded — keep the original
 * message, and so does a photo whose report says nothing about the step.
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

/**
 * The three states a clip can be *held* in — everything {@link videoFallback}
 * says that is not the dead end. They are the only answers {@link videoHold}
 * gives, which is why they have a name of their own: a held clip is one the
 * library is still working on, never one it has given up on.
 */
export type VideoPreparation = Exclude<VideoFallback, 'unsupported'>

/**
 * Whether the clip must not be offered to the browser at all, and what the
 * picture standing in for the player then says.
 *
 * A clip without a streaming rendition used to be handed to the element anyway:
 * the browser downloaded the original, refused the codec, and only then did the
 * player admit the clip was still being prepared. The same fact is known before
 * a single byte is fetched — so it is said straight away, and the poster frame
 * with a message takes the player's place.
 *
 * Three things unhold it, and each for its own reason. A clip **with** a
 * rendition is simply played. An instance that does **not** encode
 * (`instanceStreaming`, the `video_streaming` capability — false as well while
 * the flags have not been learned yet) plays every clip from the original, as it
 * always did: holding there would leave such a library unable to play anything
 * at all. And a report that says nothing worth waiting for — `done`, `skipped`,
 * or no report whatsoever — is not evidence of work in progress, so the clip
 * keeps the behaviour it had before streaming existed.
 *
 * @param streaming Whether the photo has an encoded rendition (`photo.hls`).
 * @param encode The `hls_transcode` row of the processing report, or `null`.
 * @param instanceStreaming Whether this instance encodes videos at all.
 * @returns What the stand-in says, or `null` when the clip is to be played.
 */
export function videoHold(
  streaming: boolean,
  encode: PhotoProcessing | null,
  instanceStreaming: boolean,
): VideoPreparation | null {
  if (streaming || !instanceStreaming) {
    return null
  }
  const fallback = videoFallback(encode)
  return fallback === 'unsupported' ? null : fallback
}

/**
 * Whether a catalogue row is a clip whose streaming version is still owed: a
 * video, a payload that explicitly says it has no rendition, and an instance
 * that encodes them.
 *
 * This is {@link videoHold} for everything that holds a *list* row rather than a
 * detail — a grid tile, a slide — where there is no processing report to consult
 * and so no stage of the encode to name. The three conditions are all such a row
 * can know, and each is load-bearing: `hls` is only ever `false` when the server
 * looked and found nothing (an absent one, on an older payload or a still, says
 * nothing at all), and with the encode switched off instance-wide no clip will
 * ever gain a rendition, so "still being prepared" would be a permanent lie.
 *
 * @param photo The catalogue row.
 * @param instanceStreaming Whether this instance encodes videos at all.
 */
export function streamPending(photo: Photo, instanceStreaming: boolean): boolean {
  return instanceStreaming && isVideo(photo) && photo.hls === false
}

/**
 * Whether the question {@link streamPending} answers cannot be answered yet:
 * the row is one the flags could hold — a video whose payload says it has no
 * rendition — and the flags have not been learned from the server.
 *
 * The capability flags are fetched asynchronously and read all-off until the
 * answer lands, so "this instance does not encode" and "we have not asked yet"
 * look identical to {@link streamPending}. Wherever a wrong answer merely hides
 * a hint that is fine (see `CAPABILITIES_DEFAULT`), but where the answer decides
 * whether a ~250 MB original is handed to the browser it is not: the wrong guess
 * costs the download the hold exists to avoid. Somewhere that expensive, waiting
 * out the fetch is cheaper than guessing — as long as the wait itself is timed,
 * because flags that never arrive must not hold anything forever.
 *
 * @param photo The catalogue row.
 * @param capabilitiesKnown Whether a capabilities response was ever received.
 */
export function streamUndecided(photo: Photo, capabilitiesKnown: boolean): boolean {
  return !capabilitiesKnown && isVideo(photo) && photo.hls === false
}
