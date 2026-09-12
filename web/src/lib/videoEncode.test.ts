import { describe, expect, it } from 'vitest'

import {
  type Photo,
  type PhotoDetail,
  type PhotoProcessing,
  type ProcessingState,
} from '../services/photos'

import {
  encodeInProgress,
  shouldWatchEncode,
  streamPending,
  streamUndecided,
  videoEncode,
  videoFallback,
  videoHold,
} from './videoEncode'

/** A detail of the given kind, with the streaming step in the given state. */
function detail(
  mediaType: string,
  encode: ProcessingState | null,
  extra: Partial<PhotoDetail> = {},
): PhotoDetail {
  const processing: PhotoProcessing[] = [
    { step: 'metadata', state: 'done', at: '2026-09-10T10:00:00Z' },
    { step: 'thumbnail', state: 'done', at: '2026-09-10T10:00:01Z' },
  ]
  if (encode !== null) {
    processing.push({ step: 'hls_transcode', state: encode })
  }
  return { uid: 'v1', media_type: mediaType, processing, ...extra } as PhotoDetail
}

describe('videoEncode', () => {
  it('picks the streaming step out of the report', () => {
    expect(videoEncode(detail('video', 'queued'))).toEqual({
      step: 'hls_transcode',
      state: 'queued',
    })
  })

  it('answers null when the report says nothing about the step', () => {
    expect(videoEncode(detail('video', null))).toBeNull()
    expect(videoEncode({ uid: 'v1', media_type: 'video' } as PhotoDetail)).toBeNull()
  })
})

describe('encodeInProgress', () => {
  it('is true for exactly the two states a rendition can appear out of', () => {
    expect(encodeInProgress({ step: 'hls_transcode', state: 'queued' })).toBe(true)
    expect(encodeInProgress({ step: 'hls_transcode', state: 'running' })).toBe(true)
  })

  it('is false for every terminal state, and for no report at all', () => {
    for (const state of ['done', 'failed', 'skipped', 'pending'] as const) {
      expect(encodeInProgress({ step: 'hls_transcode', state })).toBe(false)
    }
    expect(encodeInProgress(null)).toBe(false)
  })
})

describe('shouldWatchEncode', () => {
  it('watches a clip whose encode is on its way', () => {
    expect(shouldWatchEncode(detail('video', 'queued'))).toBe(true)
    expect(shouldWatchEncode(detail('video', 'running'))).toBe(true)
  })

  it('never watches a still photo', () => {
    // Not that a still's report would ever say `queued` — but the guard is what
    // keeps a library of photographs from polling at all.
    expect(shouldWatchEncode(detail('image', 'queued'))).toBe(false)
    expect(shouldWatchEncode(detail('image', 'skipped'))).toBe(false)
  })

  it('never watches a clip that already has its rendition', () => {
    expect(shouldWatchEncode(detail('video', 'done', { hls: true }))).toBe(false)
    // Even mid-re-encode: there is a streaming source to play right now.
    expect(shouldWatchEncode(detail('video', 'running', { hls: true }))).toBe(false)
  })

  it('generates no traffic on an instance with streaming switched off', () => {
    expect(shouldWatchEncode(detail('video', 'skipped'))).toBe(false)
  })

  it('does not watch a state nothing will come out of on its own', () => {
    expect(shouldWatchEncode(detail('video', 'failed'))).toBe(false)
    expect(shouldWatchEncode(detail('video', 'pending'))).toBe(false)
    expect(shouldWatchEncode(detail('video', null))).toBe(false)
  })
})

describe('videoFallback', () => {
  it('reads a waiting encode as a wait, not as a dead end', () => {
    expect(videoFallback({ step: 'hls_transcode', state: 'queued' })).toBe('preparing')
    expect(videoFallback({ step: 'hls_transcode', state: 'pending' })).toBe('preparing')
    expect(videoFallback({ step: 'hls_transcode', state: 'running' })).toBe('preparingNow')
  })

  it('names a failed preparation as such', () => {
    expect(videoFallback({ step: 'hls_transcode', state: 'failed' })).toBe('preparationFailed')
  })

  it('keeps the original message where nothing more will happen', () => {
    expect(videoFallback({ step: 'hls_transcode', state: 'done' })).toBe('unsupported')
    expect(videoFallback({ step: 'hls_transcode', state: 'skipped' })).toBe('unsupported')
    expect(videoFallback(null)).toBe('unsupported')
  })
})

describe('videoHold', () => {
  const row = (state: ProcessingState): PhotoProcessing => ({ step: 'hls_transcode', state })

  it('holds a clip whose encode is owed, naming the stage it is at', () => {
    expect(videoHold(false, row('queued'), true)).toBe('preparing')
    expect(videoHold(false, row('pending'), true)).toBe('preparing')
    expect(videoHold(false, row('running'), true)).toBe('preparingNow')
    expect(videoHold(false, row('failed'), true)).toBe('preparationFailed')
  })

  it('plays a clip that already has a rendition, whatever the report says', () => {
    expect(videoHold(true, row('queued'), true)).toBeNull()
    expect(videoHold(true, row('running'), true)).toBeNull()
    expect(videoHold(true, null, true)).toBeNull()
  })

  it('plays everything on an instance that does not stream at all', () => {
    // Holding here would leave such a library unable to play a single clip.
    expect(videoHold(false, row('queued'), false)).toBeNull()
    expect(videoHold(false, row('running'), false)).toBeNull()
    expect(videoHold(false, row('failed'), false)).toBeNull()
  })

  it('plays a clip whose report says nothing worth waiting for', () => {
    expect(videoHold(false, row('skipped'), true)).toBeNull()
    expect(videoHold(false, row('done'), true)).toBeNull()
    // No report at all — an instance with no processing service, an older
    // fixture: "cannot tell" is not "being prepared".
    expect(videoHold(false, null, true)).toBeNull()
  })
})

describe('streamPending', () => {
  const row = (extra: Partial<Photo>): Photo => ({ uid: 'v1', ...extra }) as Photo

  it('marks a video the server looked at and found no rendition for', () => {
    expect(streamPending(row({ media_type: 'video', hls: false }), true)).toBe(true)
  })

  it('marks nothing once the rendition exists', () => {
    expect(streamPending(row({ media_type: 'video', hls: true }), true)).toBe(false)
  })

  it('marks nothing when the payload does not say', () => {
    expect(streamPending(row({ media_type: 'video' }), true)).toBe(false)
  })

  it('marks nothing on a still, which never streams', () => {
    expect(streamPending(row({ media_type: 'image', hls: false }), true)).toBe(false)
    expect(streamPending(row({ media_type: 'live', hls: false }), true)).toBe(false)
  })

  it('marks nothing when the instance does not stream at all', () => {
    expect(streamPending(row({ media_type: 'video', hls: false }), false)).toBe(false)
  })
})

describe('streamUndecided', () => {
  const row = (extra: Partial<Photo>): Photo => ({ uid: 'v1', ...extra }) as Photo

  it('holds the question open for a clip with no rendition until the flags land', () => {
    expect(streamUndecided(row({ media_type: 'video', hls: false }), false)).toBe(true)
  })

  it('answers nothing once the flags are known, whatever they say', () => {
    expect(streamUndecided(row({ media_type: 'video', hls: false }), true)).toBe(false)
  })

  it('answers nothing for a row the flags could not hold anyway', () => {
    expect(streamUndecided(row({ media_type: 'video', hls: true }), false)).toBe(false)
    expect(streamUndecided(row({ media_type: 'video' }), false)).toBe(false)
    expect(streamUndecided(row({ media_type: 'image', hls: false }), false)).toBe(false)
  })
})
