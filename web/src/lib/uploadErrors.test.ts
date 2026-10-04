import { describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { canRetryUpload, isPermanentUploadError, uploadErrorMessage } from './uploadErrors'

describe('isPermanentUploadError', () => {
  it('knows the refusals a retry cannot change', () => {
    expect(isPermanentUploadError('not_media')).toBe(true)
    expect(isPermanentUploadError('unsupported_type')).toBe(true)
    expect(isPermanentUploadError('damaged')).toBe(true)
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

  it('falls back to the server message, and to nothing for an empty one', () => {
    expect(uploadErrorMessage({ error: 'network error' }, i18n.t)).toBe('network error')
    expect(uploadErrorMessage({ error: '' }, i18n.t)).toBeUndefined()
    expect(uploadErrorMessage({}, i18n.t)).toBeUndefined()
  })
})
