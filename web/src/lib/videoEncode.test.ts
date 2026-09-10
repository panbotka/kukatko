import { describe, expect, it } from 'vitest'

import { type PhotoDetail, type PhotoProcessing, type ProcessingState } from '../services/photos'

import { encodeInProgress, shouldWatchEncode, videoEncode, videoFallback } from './videoEncode'

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
