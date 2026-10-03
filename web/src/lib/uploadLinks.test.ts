import { describe, expect, it } from 'vitest'

import { type UploadSummary } from '../hooks/useUploadQueue'
import i18n from '../i18n'

import { newLinkForAlbum, uploadLinkSummary } from './uploadLinks'

/** A finished batch with the given counts. */
function summary(created: number, duplicate: number, error: number): UploadSummary {
  return { total: created + duplicate + error, queued: 0, uploading: 0, created, duplicate, error }
}

describe('uploadLinkSummary', () => {
  it('names only the parts that happened, the uploaded count always', async () => {
    await i18n.changeLanguage('en')
    expect(uploadLinkSummary(summary(12, 1, 1), i18n.t)).toBe('12 uploaded, 1 duplicate, 1 error')
    expect(uploadLinkSummary(summary(3, 0, 0), i18n.t)).toBe('3 uploaded')
    expect(uploadLinkSummary(summary(0, 2, 0), i18n.t)).toBe('0 uploaded, 2 duplicates')
  })

  it('declines Czech counts', async () => {
    await i18n.changeLanguage('cs')
    expect(uploadLinkSummary(summary(12, 1, 1), i18n.t)).toBe('12 nahráno, 1 duplicita, 1 chyba')
    expect(uploadLinkSummary(summary(2, 3, 4), i18n.t)).toBe('2 nahrány, 3 duplicity, 4 chyby')
    await i18n.changeLanguage('en')
  })
})

describe('newLinkForAlbum', () => {
  it('opens the create form with the album chosen', () => {
    expect(newLinkForAlbum('al 1')).toBe('/upload-links?album=al%201')
  })
})
