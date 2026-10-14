import { describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { canRetryUpload, isPermanentUploadError, uploadErrorMessage } from './uploadErrors'

describe('isPermanentUploadError', () => {
  it('knows the refusals a retry cannot change', () => {
    expect(isPermanentUploadError('not_media')).toBe(true)
    expect(isPermanentUploadError('unsupported_type')).toBe(true)
    expect(isPermanentUploadError('damaged')).toBe(true)
    expect(isPermanentUploadError('file_too_large')).toBe(true)
    expect(isPermanentUploadError('quota_exceeded')).toBe(true)
    expect(isPermanentUploadError(undefined)).toBe(false)
    expect(isPermanentUploadError('something_new')).toBe(false)
  })
})

describe('canRetryUpload', () => {
  it('offers a retry only for a failure that might go differently', () => {
    expect(canRetryUpload({ status: 'error' })).toBe(true)
    expect(canRetryUpload({ status: 'error', errorCode: 'not_media' })).toBe(false)
    expect(canRetryUpload({ status: 'error', errorCode: 'unsupported_type' })).toBe(false)
    expect(canRetryUpload({ status: 'created' })).toBe(false)
  })
})

describe('uploadErrorMessage', () => {
  it('translates a coded refusal instead of showing the server string', async () => {
    await i18n.changeLanguage('cs')
    const item = { errorCode: 'not_media', error: 'ingest: not a photo or video: broken.jpg' }
    expect(uploadErrorMessage(item, i18n.t)).toBe('Tohle není fotka ani video')
    await i18n.changeLanguage('en')
    expect(uploadErrorMessage(item, i18n.t)).toBe('This is not a photo or a video')
  })

  it('translates a damaged image in both languages', async () => {
    const item = { errorCode: 'damaged', error: 'ingest: damaged or incomplete image: cut.jpg' }
    await i18n.changeLanguage('cs')
    expect(uploadErrorMessage(item, i18n.t)).toBe('Soubor je poškozený nebo neúplný')
    await i18n.changeLanguage('en')
    expect(uploadErrorMessage(item, i18n.t)).toBe('This file is damaged or incomplete')
  })

  it('says a dropped connection in words instead of the transport error', async () => {
    const item = { status: 'error', error: 'network error', interrupted: true }
    await i18n.changeLanguage('cs')
    expect(uploadErrorMessage(item, i18n.t)).toBe('Spojení se přerušilo')
    await i18n.changeLanguage('en')
    expect(uploadErrorMessage(item, i18n.t)).toBe('The connection dropped')
    // Still a failure a retry can fix.
    expect(canRetryUpload(item)).toBe(true)
  })

  it('names the size cap a file is over, in both languages', async () => {
    const item = {
      errorCode: 'file_too_large',
      error: 'ingest: file exceeds maximum upload size of 1572864 bytes',
      errorLimitBytes: 1572864,
    }
    await i18n.changeLanguage('cs')
    expect(uploadErrorMessage(item, i18n.t, 'cs')).toBe('Soubor je větší než povolených 1,5 MB')
    await i18n.changeLanguage('en')
    expect(uploadErrorMessage(item, i18n.t, 'en')).toBe(
      'The file is larger than the allowed 1.5 MB',
    )
    expect(uploadErrorMessage({ errorCode: 'file_too_large' }, i18n.t)).toBe(
      'The file is larger than the project allows',
    )
  })

  it('says this upload would cross the quota, with how much of it is used', async () => {
    const MB = 1024 * 1024
    // The review's case: 9.7 of 11 MB used, so the project has not "reached"
    // anything — this file is what does not fit.
    const item = {
      errorCode: 'quota_exceeded',
      error: 'quota_exceeded',
      errorLimitBytes: 11 * MB,
      errorUsedBytes: 9.7 * MB,
    }
    await i18n.changeLanguage('cs')
    expect(uploadErrorMessage(item, i18n.t, 'cs')).toBe(
      'Tento soubor by překročil limit úložiště projektu (využito 9,7 MB z 11,0 MB)',
    )
    await i18n.changeLanguage('en')
    expect(uploadErrorMessage(item, i18n.t, 'en')).toBe(
      "This upload would exceed the project's storage limit (9.7 MB of 11.0 MB used)",
    )
  })

  it('names what the server sent of the quota, and no number it did not', async () => {
    await i18n.changeLanguage('en')
    expect(
      uploadErrorMessage(
        { errorCode: 'quota_exceeded', errorLimitBytes: 2 * 1024 ** 3 },
        i18n.t,
        'en',
      ),
    ).toBe("This upload would exceed the project's storage limit of 2.0 GB")
    expect(uploadErrorMessage({ errorCode: 'quota_exceeded' }, i18n.t)).toBe(
      "This upload would exceed the project's storage limit",
    )
  })

  it('falls back to the server message, and to nothing for an empty one', () => {
    expect(uploadErrorMessage({ error: 'network error' }, i18n.t)).toBe('network error')
    expect(uploadErrorMessage({ error: '' }, i18n.t)).toBeUndefined()
    expect(uploadErrorMessage({}, i18n.t)).toBeUndefined()
  })
})
