import { afterEach, describe, expect, it, vi } from 'vitest'

import { type PhotoDetail } from '../services/photos'

import {
  bumpRenditionVersion,
  renditionVersion,
  renditionVersions,
  resetRenditionVersions,
  subscribeRenditionVersions,
  thumbnailRebuildPending,
  thumbnailRebuiltSince,
  versionedUrl,
} from './renditionRebuild'

/** A detail carrying only what the rebuild check reads. */
function detail(processing?: PhotoDetail['processing']): PhotoDetail {
  return { uid: 'b', processing } as PhotoDetail
}

const SAVED_AT = '2026-09-06T10:00:00.250Z'

describe('thumbnailRebuiltSince', () => {
  it('is true once the thumbnail step ran at or after the save', () => {
    expect(
      thumbnailRebuiltSince(
        detail([{ step: 'thumbnail', state: 'done', at: '2026-09-06T10:00:04Z' }]),
        SAVED_AT,
      ),
    ).toBe(true)
    expect(
      thumbnailRebuiltSince(detail([{ step: 'thumbnail', state: 'done', at: SAVED_AT }]), SAVED_AT),
    ).toBe(true)
  })

  it('is false while the report still shows the run before the save', () => {
    // `done` says nothing on its own: the step reads done from the first build
    // on, whatever the queue holds. Only a later stamp means the rebuild landed.
    expect(
      thumbnailRebuiltSince(
        detail([{ step: 'thumbnail', state: 'done', at: '2026-09-06T09:59:59Z' }]),
        SAVED_AT,
      ),
    ).toBe(false)
    expect(thumbnailRebuiltSince(detail([{ step: 'thumbnail', state: 'queued' }]), SAVED_AT)).toBe(
      false,
    )
  })

  it('never says yes without a report to read', () => {
    expect(thumbnailRebuiltSince(detail(undefined), SAVED_AT)).toBe(false)
    expect(thumbnailRebuiltSince(detail([]), SAVED_AT)).toBe(false)
    expect(
      thumbnailRebuiltSince(
        detail([{ step: 'thumbnail', state: 'done', at: 'not a date' }]),
        SAVED_AT,
      ),
    ).toBe(false)
  })
})

describe('thumbnailRebuildPending', () => {
  it('is true while the report positively owes a rebuild', () => {
    // A build recorded before the save, or a step the queue/a worker holds.
    expect(
      thumbnailRebuildPending(
        detail([{ step: 'thumbnail', state: 'done', at: '2026-09-06T09:59:59Z' }]),
        SAVED_AT,
      ),
    ).toBe(true)
    expect(
      thumbnailRebuildPending(detail([{ step: 'thumbnail', state: 'queued' }]), SAVED_AT),
    ).toBe(true)
    expect(
      thumbnailRebuildPending(detail([{ step: 'thumbnail', state: 'running' }]), SAVED_AT),
    ).toBe(true)
  })

  it('is false once the rebuild has landed', () => {
    expect(
      thumbnailRebuildPending(
        detail([{ step: 'thumbnail', state: 'done', at: '2026-09-06T10:00:04Z' }]),
        SAVED_AT,
      ),
    ).toBe(false)
    expect(
      thumbnailRebuildPending(
        detail([{ step: 'thumbnail', state: 'done', at: SAVED_AT }]),
        SAVED_AT,
      ),
    ).toBe(false)
  })

  it('is false whenever the report cannot tell, so no watch is started in vain', () => {
    // An instance that wires no processing service, and the states no rebuild
    // ever comes out of. This is the half `thumbnailRebuiltSince` cannot express:
    // there, all of these are `false` too, but meaning "not built", not "unknown".
    expect(thumbnailRebuildPending(detail(undefined), SAVED_AT)).toBe(false)
    expect(thumbnailRebuildPending(detail([]), SAVED_AT)).toBe(false)
    expect(
      thumbnailRebuildPending(detail([{ step: 'thumbnail', state: 'failed' }]), SAVED_AT),
    ).toBe(false)
    expect(
      thumbnailRebuildPending(detail([{ step: 'thumbnail', state: 'skipped' }]), SAVED_AT),
    ).toBe(false)
    expect(
      thumbnailRebuildPending(detail([{ step: 'thumbnail', state: 'pending' }]), SAVED_AT),
    ).toBe(false)
    expect(
      thumbnailRebuildPending(
        detail([{ step: 'thumbnail', state: 'done', at: 'not a date' }]),
        SAVED_AT,
      ),
    ).toBe(false)
  })
})

describe('versionedUrl', () => {
  it('appends a version with the right separator, and nothing for version 0', () => {
    expect(versionedUrl('/api/v1/photos/b/thumb/fit_1280', 0)).toBe(
      '/api/v1/photos/b/thumb/fit_1280',
    )
    expect(versionedUrl('/api/v1/photos/b/thumb/fit_1280', 2)).toBe(
      '/api/v1/photos/b/thumb/fit_1280?v=2',
    )
    expect(versionedUrl('/api/v1/photos/b/thumb/fit_1280?t=tok', 1)).toBe(
      '/api/v1/photos/b/thumb/fit_1280?t=tok&v=1',
    )
  })
})

describe('rendition versions', () => {
  afterEach(() => {
    resetRenditionVersions()
  })

  it('counts each photo on its own and tells subscribers', () => {
    const listener = vi.fn()
    const unsubscribe = subscribeRenditionVersions(listener)
    const before = renditionVersions()

    expect(renditionVersion('a')).toBe(0)
    expect(bumpRenditionVersion('a')).toBe(1)
    expect(bumpRenditionVersion('a')).toBe(2)
    expect(renditionVersion('a')).toBe(2)
    expect(renditionVersion('b')).toBe(0)
    expect(listener).toHaveBeenCalledTimes(2)
    // A new snapshot per bump, so a React subscriber sees a changed value; the
    // old one is left as it was.
    expect(renditionVersions()).not.toBe(before)
    expect(renditionVersion('a', before)).toBe(0)

    unsubscribe()
    bumpRenditionVersion('b')
    expect(listener).toHaveBeenCalledTimes(2)
  })
})
