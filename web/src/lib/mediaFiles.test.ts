import { describe, expect, it } from 'vitest'

import {
  fileExtension,
  isMediaFile,
  MEDIA_EXTENSIONS,
  PICKER_ACCEPT,
  previewKind,
} from './mediaFiles'

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

describe('previewKind', () => {
  it('names the images a browser paints on its own', () => {
    expect(previewKind({ name: 'a.JPG', type: 'image/jpeg' })).toBe('image')
    expect(previewKind({ name: 'a.png', type: '' })).toBe('image')
    // No extension to go on: the MIME type decides.
    expect(previewKind({ name: 'clipboard', type: 'image/webp' })).toBe('image')
  })

  it('names a video, whose first frame the client cannot decode', () => {
    expect(previewKind({ name: 'clip.MOV', type: '' })).toBe('video')
    expect(previewKind({ name: 'whatever', type: 'video/mp4' })).toBe('video')
  })

  it('refuses to promise a preview of HEIC, TIFF or RAW', () => {
    // The upload queue draws a placeholder for these; an `<img>` pointed at one
    // only ever ends at the broken-image glyph.
    expect(previewKind({ name: 'IMG_0042.HEIC', type: 'image/heic' })).toBe('none')
    expect(previewKind({ name: 'scan.tif', type: 'image/tiff' })).toBe('none')
    expect(previewKind({ name: 'DSC_1000.nef', type: '' })).toBe('none')
    // A sender mislabelling a RAW file as a JPEG must not talk us into it: the
    // extension is the more reliable of the two, so it wins.
    expect(previewKind({ name: 'DSC_1000.cr3', type: 'image/jpeg' })).toBe('none')
  })

  it('says nothing can be previewed of a file that is not media at all', () => {
    expect(previewKind({ name: 'smlouva.pdf', type: 'application/pdf' })).toBe('none')
    expect(previewKind({ name: 'archive', type: '' })).toBe('none')
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
