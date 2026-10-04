import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { uploadLinkSender } from '../../lib/uploadLinks'
import type { PhotoUploadLinkRef } from '../../services/photos'

import { UploadLinkLine } from './UploadLinkLine'

/** Builds an upload-link provenance block with the given overrides. */
function link(overrides: Partial<PhotoUploadLinkRef> = {}): PhotoUploadLinkRef {
  return {
    uid: 'ul1',
    title: 'Pouť 2026',
    uploaded_at: '2026-06-20T10:00:00Z',
    ...overrides,
  }
}

/** Renders the line in Czech (the default language) and returns its text. */
async function renderLine(ref: PhotoUploadLinkRef, lang = 'cs'): Promise<string> {
  await i18n.changeLanguage(lang)
  const { container } = render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <UploadLinkLine link={ref} />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return container.textContent
}

afterAll(async () => {
  await i18n.changeLanguage('cs')
})

describe('UploadLinkLine', () => {
  it('names the typed sender', async () => {
    const text = await renderLine(link({ uploader_name: 'Jana' }))
    expect(text).toBe('Nahráno přes odkaz „Pouť 2026“ · od: Jana')
  })

  it('names the account when nothing was typed', async () => {
    const text = await renderLine(link({ account: { uid: 'us1', name: 'Jana Nováková' } }))
    expect(text).toBe('Nahráno přes odkaz „Pouť 2026“ · od: Jana Nováková')
  })

  it('says "bez jména" with neither — never an empty "od:"', async () => {
    const text = await renderLine(link())
    expect(text).toBe('Nahráno přes odkaz „Pouť 2026“ · bez jména')
    expect(text).not.toContain('od:')
  })

  it('links the title to the upload-links page', async () => {
    await renderLine(link({ uploader_name: 'Jana' }))
    expect(screen.getByRole('link', { name: '„Pouť 2026“' })).toHaveAttribute(
      'href',
      '/upload-links',
    )
  })

  it('names an untitled link as such rather than as empty quotes', async () => {
    const text = await renderLine(link({ title: '  ' }))
    expect(text).toBe('Nahráno přes odkaz bez názvu · bez jména')
    expect(screen.getByRole('link', { name: 'odkaz bez názvu' })).toBeInTheDocument()
  })

  it('reads in English', async () => {
    const text = await renderLine(link({ uploader_name: 'Jana' }), 'en')
    expect(text).toBe('Uploaded via link “Pouť 2026” · from: Jana')
    expect(await renderLine(link(), 'en')).toContain('· no name')
  })
})

describe('uploadLinkSender', () => {
  it('prefers the typed name, then the account, then nobody', () => {
    const account = { uid: 'us1', name: 'Account' }
    expect(uploadLinkSender(link({ uploader_name: 'Typed', account }))).toBe('Typed')
    expect(uploadLinkSender(link({ uploader_name: ' ', account }))).toBe('Account')
    expect(uploadLinkSender(link({ account: { uid: 'us1', name: '' } }))).toBeUndefined()
    expect(uploadLinkSender(link())).toBeUndefined()
  })
})
