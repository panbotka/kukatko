import { describe, expect, it } from 'vitest'

import { fileExtension, isMediaFile, MEDIA_EXTENSIONS, PICKER_ACCEPT } from './mediaFiles'

describe('fileExtension', () => {
  it.each([
    ['photo.JPG', 'jpg'],
    ['a.b.heic', 'heic'],
    ['no-extension', ''],
    ['.hidden', ''],
    ['trailing.', ''],
  ])('reads %s as %s', (name, expected) => {
    expect(fileExtension(name)).toBe(expected)
  })
})

describe('isMediaFile', () => {
  it('trusts a MIME type that says image or video', () => {
    expect(isMediaFile({ name: 'whatever', type: 'image/jpeg' })).toBe(true)
    expect(isMediaFile({ name: 'whatever', type: 'video/quicktime' })).toBe(true)
  })

  it('falls back to the extension, which is how RAW and HEIC usually arrive', () => {
    // Senders that hand a photo over as a blob of bytes are the norm, not the
    // exception: file managers, messengers and cloud drives all do it.
    expect(isMediaFile({ name: 'IMG_0042.HEIC', type: 'application/octet-stream' })).toBe(true)
    expect(isMediaFile({ name: 'DSC_1000.nef', type: '' })).toBe(true)
    expect(isMediaFile({ name: 'clip.MOV', type: '' })).toBe(true)
  })

  it('refuses what is neither a photo nor a video', () => {
    expect(isMediaFile({ name: 'smlouva.pdf', type: 'application/pdf' })).toBe(false)
    expect(isMediaFile({ name: 'notes.txt', type: 'text/plain' })).toBe(false)
    expect(isMediaFile({ name: 'archive', type: '' })).toBe(false)
  })
})

describe('PICKER_ACCEPT', () => {
  it('groups the phone gallery and still names the extensions browsers hide', () => {
    const entries = PICKER_ACCEPT.split(',')

    expect(entries.slice(0, 2)).toEqual(['image/*', 'video/*'])
    for (const extension of ['heic', 'dng', 'cr3', 'mov']) {
      expect(entries).toContain(`.${extension}`)
    }
    expect(entries).toHaveLength(MEDIA_EXTENSIONS.length + 2)
  })
})
