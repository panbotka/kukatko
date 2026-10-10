import { describe, expect, it } from 'vitest'

import {
  awaitingVerdicts,
  batchCompletion,
  fileCompletion,
  progressPercent,
  SEND_SHARE,
} from './uploadProgress'

/** A queued file of `size` bytes in `status`. */
function item(size: number, status: string, progress = 0, interrupted?: boolean) {
  return { file: { size }, status, progress, interrupted }
}

describe('fileCompletion', () => {
  it('makes a file whole only with a verdict; sending fills nine tenths', () => {
    expect(fileCompletion(item(10, 'created', 1))).toBe(1)
    expect(fileCompletion(item(10, 'duplicate', 1))).toBe(1)
    expect(fileCompletion(item(10, 'error', 0.3))).toBe(1)
    expect(fileCompletion(item(10, 'uploading', 0.5))).toBeCloseTo(SEND_SHARE * 0.5)
    expect(fileCompletion(item(10, 'uploading', 1))).toBe(SEND_SHARE)
    expect(fileCompletion(item(10, 'queued'))).toBe(0)
  })

  it('puts an interrupted file back at zero, since it goes again from its first byte', () => {
    expect(fileCompletion(item(10, 'error', 0.7, true))).toBe(0)
  })
})

describe('batchCompletion', () => {
  it('weighs each file by its bytes, holding back the verdict share', () => {
    const items = [item(100, 'created', 1), item(300, 'uploading', 0.5), item(600, 'queued')]
    expect(batchCompletion(items, 'bytes')).toBeCloseTo((100 + 300 * SEND_SHARE * 0.5) / 1000)
  })

  it('weighs every file the same with `files`', () => {
    const items = [item(100, 'created', 1), item(300, 'uploading', 0.5)]
    expect(batchCompletion(items, 'files')).toBeCloseTo((1 + SEND_SHARE * 0.5) / 2)
  })

  it('stays below 1 with every byte sent but a verdict pending', () => {
    // The prerelease case: four photos answered, two fully sent and still processing.
    const items = [
      ...Array.from({ length: 4 }, () => item(13_000_000, 'created', 1)),
      item(13_000_000, 'uploading', 1),
      item(13_000_000, 'uploading', 1),
    ]
    expect(batchCompletion(items, 'bytes')).toBeLessThan(1)
    expect(batchCompletion(items, 'files')).toBeLessThan(1)
    expect(progressPercent(batchCompletion(items, 'bytes'))).toBeLessThan(100)
  })

  it('is 1 exactly once every file has a verdict', () => {
    const items = [item(100, 'created', 1), item(7, 'duplicate', 1), item(3, 'error', 0)]
    expect(batchCompletion(items, 'bytes')).toBe(1)
    expect(batchCompletion(items, 'files')).toBe(1)
  })

  it('is zero for an empty batch and counts files when nothing has bytes', () => {
    expect(batchCompletion([], 'bytes')).toBe(0)
    expect(batchCompletion([], 'files')).toBe(0)
    expect(batchCompletion([item(0, 'created', 1), item(0, 'queued')], 'bytes')).toBeCloseTo(0.5)
  })
})

describe('awaitingVerdicts', () => {
  it('is true once every running file has sent all its bytes and nothing waits', () => {
    expect(awaitingVerdicts([item(1, 'created', 1), item(1, 'uploading', 1)])).toBe(true)
  })

  it('is false while bytes are still going, a file waits or is due to go again', () => {
    expect(awaitingVerdicts([item(1, 'uploading', 0.99)])).toBe(false)
    expect(awaitingVerdicts([item(1, 'uploading', 1), item(1, 'queued')])).toBe(false)
    expect(awaitingVerdicts([item(1, 'uploading', 1), item(1, 'error', 0.4, true)])).toBe(false)
  })

  it('is false with nothing running — a settled batch is done, not processing', () => {
    expect(awaitingVerdicts([])).toBe(false)
    expect(awaitingVerdicts([item(1, 'created', 1), item(1, 'error', 0)])).toBe(false)
  })
})

describe('progressPercent', () => {
  it('rounds down, so one verdict short of done never reads 100 %', () => {
    expect(progressPercent(0.999)).toBe(99)
    expect(progressPercent(0.25)).toBe(25)
    expect(progressPercent(1)).toBe(100)
  })

  it('clamps to the 0–100 range', () => {
    expect(progressPercent(-0.2)).toBe(0)
    expect(progressPercent(1.4)).toBe(100)
  })
})
