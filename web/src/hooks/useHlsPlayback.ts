import { useEffect, useState, type RefObject } from 'react'

import { nativeHlsSupported, videoDelivery, type VideoDelivery } from '../lib/hlsPlayback'
import { hlsMasterUrl, videoUrl } from '../services/photos'

/** What {@link useHlsPlayback} needs to drive one clip. */
export interface HlsPlaybackOptions {
  /** The `<video>` element hls.js attaches to. */
  videoRef: RefObject<HTMLVideoElement | null>
  /** UID of the photo being played. */
  uid: string
  /** Whether the photo reports an encoded HLS rendition (`photo.hls`). */
  streaming: boolean
  /** Download token appended to every URL, for cookie-less contexts. */
  token?: string | null
  /** Called once playback has fatally failed and cannot be resumed. */
  onFatalError: () => void
}

/** What {@link useHlsPlayback} tells the player. */
export interface HlsPlayback {
  /**
   * What belongs in the element's `src` — the master playlist, the progressive
   * endpoint, or `undefined` while hls.js owns the element and feeds it through
   * Media Source Extensions.
   */
  src: string | undefined
  /** How the clip is currently delivered, for tests and for the record. */
  delivery: VideoDelivery
}

/**
 * Plays a clip the best way the pair (photo, browser) allows.
 *
 * A photo with an encoded rendition is streamed: on a browser that plays HLS
 * itself the master playlist simply goes into `src`, and on every other one
 * hls.js is loaded — **lazily**, as its own chunk, so a library of stills never
 * downloads a video demuxer — and attached to the very same element. That is the
 * whole point of attaching rather than replacing: the control bar, the storyboard
 * scrubber, the shortcuts, fullscreen and the remembered rate all keep talking to
 * one plain `<video>` and know nothing about how its bytes arrive.
 *
 * Anything that goes wrong *before* playback — no rendition, the chunk fails to
 * load, a browser without Media Source Extensions — falls back to the progressive
 * `/video` endpoint, which is what the player did before streaming existed. A
 * fatal error *during* playback is reported through `onFatalError` instead: at
 * that point the element has already rejected the media, and the honest answer is
 * the player's "cannot play, download instead" state rather than a silent retry.
 *
 * @param options The element, the photo and the fatal-error callback.
 * @returns The `src` for the element and the delivery in force.
 */
export function useHlsPlayback(options: HlsPlaybackOptions): HlsPlayback {
  const { videoRef, uid, streaming, token, onFatalError } = options
  const master = hlsMasterUrl(uid, token)
  const progressive = videoUrl(uid, token)

  const [delivery, setDelivery] = useState<VideoDelivery>(() =>
    videoDelivery(streaming, nativeHlsSupported()),
  )
  // Another clip is another decision, and it has to be taken while rendering:
  // an effect would let one frame through with the previous clip's delivery,
  // which for a still-after-a-video means a request for a playlist that 404s.
  const [decidedFor, setDecidedFor] = useState({ uid, streaming })
  if (decidedFor.uid !== uid || decidedFor.streaming !== streaming) {
    setDecidedFor({ uid, streaming })
    setDelivery(videoDelivery(streaming, nativeHlsSupported()))
  }

  useEffect(() => {
    if (delivery !== 'library') {
      return undefined
    }
    const video = videoRef.current
    if (video === null) {
      return undefined
    }
    let cancelled = false
    let player: { destroy: () => void } | null = null

    const attach = async (): Promise<void> => {
      const { default: Hls } = await import('hls.js/light')
      if (cancelled) {
        return
      }
      if (!Hls.isSupported()) {
        setDelivery('progressive')
        return
      }
      const hls = new Hls()
      player = hls
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          onFatalError()
        }
      })
      hls.attachMedia(video)
      hls.loadSource(master)
    }

    attach().catch(() => {
      // The chunk did not load, or the library threw on the way up. Nothing has
      // been played yet, so the original file is still a perfectly good answer.
      if (!cancelled) {
        setDelivery('progressive')
      }
    })

    return () => {
      cancelled = true
      player?.destroy()
    }
  }, [delivery, master, videoRef, onFatalError])

  if (delivery === 'library') {
    return { src: undefined, delivery }
  }
  return { src: delivery === 'native' ? master : progressive, delivery }
}
